package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
	"github.com/byranZA/smith/internal/onbox"
	"github.com/byranZA/smith/internal/provider"
)

// setupSSH stands in for every local binary a `machine setup` run launches: the
// ssh and scp that reach the box, and the tailscale CLI that reads this
// machine's own tailnet membership. It answers as a box that lets a run all the
// way through, so a test can drive the real command and assert on what the
// operator ends up with rather than on how setup got there.
type setupSSH struct {
	// marker is what the box's marker file holds. Empty is a box smith has
	// never provisioned, which is what a fresh box answers with.
	marker string
	// tailnetIP is the address the box is already enrolled and Running at, so a
	// tailscale run establishes reach over it without burning an auth key.
	tailnetIP string
	// enrollIP is the address an enroll brings the box up at, for a box not
	// yet on the tailnet.
	enrollIP string
	// deafAt is an ssh destination whose reachability probe the box does not
	// answer — the shape of a box smith cannot prove it can reach by the
	// address it is about to write down.
	deafAt string
	// machine is the machine hardware name `uname -m` prints on the box, which
	// the install stage reads to pick the release asset to fetch.
	machine string
	// installed is the smith version already on the box, empty when it carries
	// none — which is what a box the install stage has never reached answers.
	installed string
	// phaseErr is what the box answers the mutating phases with, standing in
	// for a run that failed partway through the base layer.
	phaseErr error
	// probeDenied has the tailnet deny the live ssh probe, the shape of a
	// tailnet missing its ssh ACL rule for tag:smith.
	probeDenied bool

	// public is the host the run first reached the box at: its public host.
	public string
	// hardened is set once the setup subcommand has run, after which sshd
	// refuses root logins as ssh-hardening leaves it.
	hardened bool
	// closed is set once close-public-ssh has run, after which the firewall
	// drops every connection to the public host.
	closed bool
	// refused is every launch the box turned away, as "<target> <command>".
	// None of them are in targets or commands, which hold what actually ran.
	refused []string

	targets  []string
	commands []string
	// dirs counts the private directories mktemp has made on the box.
	dirs int
	// copies is every scp of bootstrap.sh, as "<target>:<remote path>".
	copies []string
	// installCopies is every scp of the install stage's install.sh, likewise.
	installCopies []string
}

// Run answers whichever local binary the run launched, recording every ssh
// destination it was pointed at.
func (s *setupSSH) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	if name == "tailscale" {
		_, err := io.WriteString(stdout, adminOnTailnet)
		return err
	}
	if name == "scp" && len(args) > 1 {
		dest := args[len(args)-1]
		target, _, _ := strings.Cut(dest, ":")
		if s.refuses(target) {
			s.refused = append(s.refused, "scp "+dest)
			return refusedExit{}
		}
		return s.copy(args[len(args)-2], dest)
	}
	if name != "ssh" || len(args) < 2 {
		return nil
	}
	machine := s.machine
	if machine == "" {
		machine = "x86_64"
	}
	target, remoteCmd := args[len(args)-2], args[len(args)-1]
	if s.refuses(target) {
		s.refused = append(s.refused, target+" "+remoteCmd)
		return refusedExit{}
	}
	s.targets = append(s.targets, target)
	s.commands = append(s.commands, remoteCmd)
	if target == s.deafAt && remoteCmd == "true" {
		return refusedExit{}
	}
	if answered, err := answerVersionCheck(remoteCmd, stdout); answered {
		return err
	}
	switch {
	case strings.HasPrefix(remoteCmd, "mktemp -d"):
		s.dirs++
		_, err := fmt.Fprintf(stdout, "/tmp/smith.%08d\n", s.dirs)
		return err
	case strings.HasPrefix(remoteCmd, "bash ") && strings.Contains(remoteCmd, " setup --access "):
		s.hardened = true
		return s.phaseErr
	case strings.HasSuffix(remoteCmd, " 'close-public-ssh'"):
		s.closed = true
		return nil
	case remoteCmd == "true" && s.probeDenied && hostOf(target) != s.public:
		return errors.New("tailscale: ssh access denied")
	case strings.Contains(remoteCmd, " 'enroll' '--hostname' "):
		if s.enrollIP == "" {
			return nil
		}
		_, err := fmt.Fprintf(stdout, "tailscale-ip=%s\n", s.enrollIP)
		return err
	case strings.HasSuffix(remoteCmd, " 'probe'"):
		out := "arch=" + machine + "\n"
		if s.installed != "" {
			out += "smith-version=" + s.installed + "\n"
		}
		_, err := io.WriteString(stdout, out)
		return err
	case strings.HasSuffix(remoteCmd, "preflight"):
		_, err := io.WriteString(stdout, supportedRelease)
		return err
	case strings.Contains(remoteCmd, marker.Path):
		_, err := io.WriteString(stdout, s.marker)
		return err
	case strings.HasSuffix(remoteCmd, " 'tailscale-status'"):
		if s.tailnetIP == "" {
			return nil
		}
		_, err := fmt.Fprintf(stdout, "tailscale-ip=%s\n", s.tailnetIP)
		return err
	}
	return nil
}

// refuses reports whether the box turns away a launch at target: a root login
// once hardening has run, and anything at the public host once public SSH is
// closed. The first target the run reaches is taken as the public host.
func (s *setupSSH) refuses(target string) bool {
	if s.public == "" {
		s.public = hostOf(target)
	}
	if s.hardened && strings.HasPrefix(target, "root@") {
		return true
	}
	return s.closed && hostOf(target) == s.public
}

// copy records an scp of the local file to dest, telling the install stage's
// install.sh from bootstrap.sh by what was copied: both land in private
// directories that look alike.
func (s *setupSSH) copy(local, dest string) error {
	data, err := os.ReadFile(local)
	if err != nil {
		return fmt.Errorf("read the copied file: %w", err)
	}
	if string(data) == onbox.Script {
		s.installCopies = append(s.installCopies, dest)
		return nil
	}
	s.copies = append(s.copies, dest)
	return nil
}

// shippedBootstrap is every scp of bootstrap.sh into a private directory, as
// "<target>:<remote path>". The install stage's install.sh is not among them.
func (s *setupSSH) shippedBootstrap() []string {
	var shipped []string
	for _, c := range s.copies {
		if strings.Contains(c, ":/tmp/smith.") {
			shipped = append(shipped, c)
		}
	}
	return shipped
}

// ranAs reports whether remoteCmd ran over ssh as target.
func (s *setupSSH) ranAs(target, remoteCmd string) bool {
	for i, cmd := range s.commands {
		if s.targets[i] == target && cmd == remoteCmd {
			return true
		}
	}
	return false
}

// ranAgainstAs reports whether subcommand of the script at scriptPath ran over
// ssh as target.
func (s *setupSSH) ranAgainstAs(target, scriptPath, subcommand string) bool {
	for i, cmd := range s.commands {
		if s.targets[i] == target && strings.HasPrefix(cmd, "bash "+connection.ShellArg(scriptPath)+" "+subcommand) {
			return true
		}
	}
	return false
}

// reached reports whether any ssh launch was pointed at the given destination.
func (s *setupSSH) reached(target string) bool { return slices.Contains(s.targets, target) }

// supportedRelease is what bootstrap.sh's preflight prints on a box that clears
// the gate: a root login on an Ubuntu LTS above the floor.
const supportedRelease = `privilege=root
os-release-begin
ID=ubuntu
VERSION="24.04.1 LTS (Noble Numbat)"
VERSION_ID="24.04"
os-release-end
`

// adminOnTailnet is what `tailscale status --json` prints on an operator's
// machine that is itself a Running tailnet member.
const adminOnTailnet = `{"BackendState":"Running","Self":{"UserID":1},"User":{"1":{"LoginName":"operator@example.com"}}}`

// markerNamedDev is the marker a box smith already set up as "dev" carries.
const markerNamedDev = `{"schema_version":2,"access_mode":"public","name":"dev"}`

// runSetup runs `machine setup` as an operator holding a released smith: the
// version the install stage puts on the box is named unless the test names one
// itself, because a test binary reports no released tag and every run would
// otherwise stop at that stage. A test about the install stage itself drives
// runSetupAsBuilt instead.
func runSetup(t *testing.T, dir string, ssh *setupSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	if !slices.Contains(args, "--smith-version") {
		args = append([]string{"--smith-version", "0.2.0"}, args...)
	}
	return runSetupAsBuilt(t, dir, ssh, args...)
}

// runSetupAsBuilt runs `machine setup` against a config home rooted at dir,
// with every local binary the run launches answered by the given fake, and
// returns what landed on each stream plus the exit code. It passes the flags it
// is given and nothing else, so a test can drive the build it is actually
// running.
func runSetupAsBuilt(t *testing.T, dir string, ssh *setupSSH, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := newMachineCmd(
		func() (config.Home, error) { return config.NewHome(dir), nil },
		ssh, &fakeDialer{}, provider.SystemClock(),
	)
	cmd.SetArgs(append([]string{"setup"}, args...))
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

// tailscaleRun is the flag pair a tailscale-mode run is driven with: the access
// mode, and a key reference so the run never reaches for a terminal prompt.
func tailscaleRun(t *testing.T) []string {
	t.Helper()
	t.Setenv("SMITH_TEST_TAILSCALE_KEY", "tskey-auth-test")
	return []string{"--access", "tailscale", "--tailscale-auth-key", "env:SMITH_TEST_TAILSCALE_KEY"}
}

func TestSetupProvesTheSmithUserFromTheOperatorsMachineBeforeRegistering(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{}

	stdout, stderr, code := runSetup(t, dir, ssh, "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a box that answered as the smith user (stderr: %s)", code, stderr)
	}
	if !ssh.reached("smith@203.0.113.10") {
		t.Errorf("ssh targets = %v, want a connection opened as smith@203.0.113.10", ssh.targets)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"203.0.113.10"`) || !strings.Contains(got, `"smith@203.0.113.10"`) {
		t.Errorf("inventory = %q, want the host mapped to smith@203.0.113.10", got)
	}
	if !strings.Contains(stdout, "registered") {
		t.Errorf("stdout = %q, want the box reported as registered", stdout)
	}
}

func TestSetupRegistersTheTailnetAddressAndNotThePublicOne(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{tailnetIP: "100.92.14.7"}

	args := append(tailscaleRun(t), "--name", "dev", "root@203.0.113.10")
	_, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a box reachable over the tailnet (stderr: %s)", code, stderr)
	}
	got := inventoryContent(t, dir)
	if !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want dev mapped to the tailnet address", got)
	}
	if strings.Contains(got, "203.0.113.10") {
		t.Errorf("inventory = %q, want the public address never stored in tailscale mode", got)
	}
}

func TestSetupNamesTheBoxInTheTailscaleReRunLine(t *testing.T) {
	dir := t.TempDir()

	args := append(tailscaleRun(t), "--name", "dev", "root@203.0.113.10")
	stdout, stderr, code := runSetup(t, dir, &setupSSH{tailnetIP: "100.92.14.7"}, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "smith machine setup dev") {
		t.Errorf("stdout = %q, want the re-run line to name the box", stdout)
	}
	if strings.Contains(stdout, "smith machine setup smith-") {
		t.Errorf("stdout = %q, want no derived tailnet name in the re-run line", stdout)
	}
}

func TestSetupRegistersUnderTheNameTheOperatorChose(t *testing.T) {
	dir := t.TempDir()

	_, stderr, code := runSetup(t, dir, &setupSSH{}, "--name", "dev", "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"dev"`) {
		t.Errorf("inventory = %q, want the box registered as dev", got)
	}
}

func TestSetupReportsAProvisionedButUnregisteredBoxWhenItCannotProveReach(t *testing.T) {
	dir := t.TempDir()

	stdout, stderr, code := runSetup(t, dir, &setupSSH{deafAt: "smith@203.0.113.10"},
		"--name", "dev", "root@203.0.113.10")

	if code != 1 {
		t.Fatalf("exit code = %d, want 1: the phases completed, so a probe failure is partial", code)
	}
	out := stdout + stderr
	for _, want := range []string{"provisioned", "smith@203.0.113.10", "smith machine add"} {
		if !strings.Contains(out, want) {
			t.Errorf("output = %q, want it to name %q", out, want)
		}
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a box smith could not reach left unregistered", err)
	}
}

func TestSetupCreatesTheConfigHomeWhenItRegisters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "smith")

	_, stderr, code := runSetup(t, dir, &setupSSH{}, "--name", "dev", "root@203.0.113.10")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("ReadFile(gitignore): %v", err)
	}
	if !strings.Contains(string(data), "cache/") {
		t.Errorf("gitignore = %q, want the cache kept out of version control", data)
	}
}

func TestSetupShipsBootstrapOnceAndRemovesIt(t *testing.T) {
	tests := []struct {
		name string
		ssh  *setupSSH
	}{
		{name: "setup succeeds", ssh: &setupSSH{}},
		{name: "a phase fails", ssh: &setupSSH{phaseErr: errors.New("phase packages failed")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSetup(t, t.TempDir(), tt.ssh, "root@203.0.113.10")

			shipped := tt.ssh.shippedBootstrap()
			if len(shipped) != 1 {
				t.Fatalf("shipped bootstrap.sh to %q, want exactly once", shipped)
			}
			target, script, _ := strings.Cut(shipped[0], ":")
			if target != "root@203.0.113.10" {
				t.Errorf("shipped over %q, want the bootstrap login", target)
			}
			for _, sub := range []string{"preflight", "setup"} {
				if !tt.ssh.ranAgainstAs(target, script, sub) {
					t.Errorf("%s did not run against the shipped %q; ran %q", sub, script, tt.ssh.commands)
				}
			}
			// Hardening has closed the root login by the time setup returns, so
			// the script removes its own directory as it exits.
			removeDir := "--remove-dir '" + path.Dir(script) + "'"
			if i := commandIndex(tt.ssh.commands, removeDir); i < 0 {
				t.Errorf("setup was not told to remove its shipped directory; ran %q", tt.ssh.commands)
			}
		})
	}
}

// accessCopy is the bootstrap.sh the access stage shipped for itself, as the
// target it was shipped over and its remote path; ok is false when the stage
// shipped none.
func (s *setupSSH) accessCopy() (target, script string, ok bool) {
	for _, c := range s.shippedBootstrap() {
		if strings.HasPrefix(c, smithLogin+"@") {
			target, script, _ = strings.Cut(c, ":")
			return target, script, true
		}
	}
	return "", "", false
}

// refusedAs is every launch the box turned away whose target was login's.
func (s *setupSSH) refusedAs(login string) []string {
	var refused []string
	for _, r := range s.refused {
		if strings.HasPrefix(r, login+"@") || strings.HasPrefix(r, "scp "+login+"@") {
			refused = append(refused, r)
		}
	}
	return refused
}

// rootTailscaleRun drives a tailscale setup of a fresh box over a root
// bootstrap login, the run hardening used to cut off halfway through.
func rootTailscaleRun(t *testing.T, ssh *setupSSH) (stdout, stderr string, code int) {
	t.Helper()
	args := append(tailscaleRun(t), "--name", "dev", "root@203.0.113.10")
	return runSetup(t, t.TempDir(), ssh, args...)
}

func TestTailscaleSetupOverARootLoginCompletesAfterHardening(t *testing.T) {
	dir := t.TempDir()
	ssh := &setupSSH{enrollIP: "100.92.14.7"}

	args := append(tailscaleRun(t), "--name", "dev", "root@203.0.113.10")
	stdout, stderr, code := runSetup(t, dir, ssh, args...)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stdout, "public SSH closed") {
		t.Errorf("stdout = %q, want public SSH reported closed", stdout)
	}
	if got := inventoryContent(t, dir); !strings.Contains(got, `"smith@100.92.14.7"`) {
		t.Errorf("inventory = %q, want dev registered under its tailnet address", got)
	}
}

func TestTailscaleAccessStageReachesTheBoxAsTheSmithUser(t *testing.T) {
	ssh := &setupSSH{enrollIP: "100.92.14.7"}

	rootTailscaleRun(t, ssh)

	target, script, ok := ssh.accessCopy()
	if !ok {
		t.Fatalf("shipped bootstrap.sh to %q, want a copy shipped as the smith user", ssh.shippedBootstrap())
	}
	if target != "smith@203.0.113.10" {
		t.Errorf("access stage shipped over %q, want the smith user over the public host", target)
	}
	for _, sub := range []string{"'tailscale-status'", "'enroll'"} {
		if !ssh.ranAgainstAs("smith@203.0.113.10", script, sub) {
			t.Errorf("%s did not run as smith@203.0.113.10 against %q; ran %q", sub, script, ssh.commands)
		}
	}
	// Hardening closes the root login, so a stage that tried it was turned
	// away. Only the runner's own best-effort removal may try, and its script
	// has already removed that directory itself.
	for _, r := range ssh.refusedAs("root") {
		if !strings.Contains(r, " rm -rf -- ") {
			t.Errorf("the box turned away %q, want no stage to reach it as root", r)
		}
	}
}

func TestTailscaleSetupClosesPublicSSHFromTheTailnet(t *testing.T) {
	ssh := &setupSSH{enrollIP: "100.92.14.7"}

	rootTailscaleRun(t, ssh)

	_, script, ok := ssh.accessCopy()
	if !ok {
		t.Fatalf("shipped bootstrap.sh to %q, want a copy shipped as the smith user", ssh.shippedBootstrap())
	}
	if !ssh.ranAgainstAs("smith@100.92.14.7", script, "'close-public-ssh'") {
		t.Errorf("close-public-ssh did not run as smith@100.92.14.7; ran %q", ssh.commands)
	}
	if refused := ssh.refusedAs(smithLogin); len(refused) > 0 {
		t.Errorf("the box turned away %q, want nothing to reach the public host after public SSH closed", refused)
	}
}

func TestTailscaleAccessStageRemovesItsScriptOverTheTailnet(t *testing.T) {
	ssh := &setupSSH{enrollIP: "100.92.14.7"}

	rootTailscaleRun(t, ssh)

	_, script, ok := ssh.accessCopy()
	if !ok {
		t.Fatalf("shipped bootstrap.sh to %q, want a copy shipped as the smith user", ssh.shippedBootstrap())
	}
	rm := "rm -rf -- '" + path.Dir(script) + "'"
	if !ssh.ranAs("smith@100.92.14.7", rm) {
		t.Errorf("the access stage's copy was not removed over the tailnet; ran %q", ssh.commands)
	}
}

func TestTailscaleAccessStageThatStopsBeforeTheProbePassesCleansUpOverThePublicHost(t *testing.T) {
	tests := []struct {
		name  string
		ssh   *setupSSH
		names string
	}{
		{name: "a denied probe", ssh: &setupSSH{enrollIP: "100.92.14.7", probeDenied: true}, names: "ssh ACL rule"},
		{name: "an enroll that never reaches Running", ssh: &setupSSH{}, names: "tagOwners"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := rootTailscaleRun(t, tt.ssh)

			if code != 1 {
				t.Errorf("exit code = %d, want 1 for a partial setup", code)
			}
			if !strings.Contains(stderr, tt.names) {
				t.Errorf("stderr = %q, want it to name the missing %s", stderr, tt.names)
			}
			if tt.ssh.closed {
				t.Error("public SSH closed, want it left open")
			}
			_, script, ok := tt.ssh.accessCopy()
			if !ok {
				t.Fatalf("shipped bootstrap.sh to %q, want a copy shipped as the smith user", tt.ssh.shippedBootstrap())
			}
			rm := "rm -rf -- '" + path.Dir(script) + "'"
			if !tt.ssh.ranAs("smith@203.0.113.10", rm) {
				t.Errorf("the access stage's copy was not removed over the public host; ran %q", tt.ssh.commands)
			}
		})
	}
}

func TestMachineSetupRegistersNothingWhenThePhasesNeverRan(t *testing.T) {
	dir := t.TempDir()
	ssh := &refusingSSH{}

	_, _, code := runMachine(t, dir, ssh, "setup", "root@203.0.113.10")

	if code == 0 {
		t.Fatal("exit code = 0, want non-zero for a setup that never got in")
	}
	if _, err := os.Stat(config.NewHome(dir).InventoryPath()); !os.IsNotExist(err) {
		t.Errorf("Stat(inventory) err = %v, want a failed setup to register nothing", err)
	}
}

// answerVersionCheck answers the install stage's relayed confirmation the way a
// box whose binary agrees with the smith that installed it does: the version
// the relay declared, printed as `smith version` prints it. It reports whether
// the command was that confirmation, so a fake can leave everything else to its
// own cases.
func answerVersionCheck(remoteCmd string, stdout io.Writer) (bool, error) {
	_, rest, ok := strings.Cut(remoteCmd, "--relayed-from '")
	if !ok || !strings.HasSuffix(remoteCmd, "'version'") {
		return false, nil
	}
	version, _, _ := strings.Cut(rest, "'")
	if _, err := fmt.Fprintf(stdout, "smith %s\n", version); err != nil {
		return true, fmt.Errorf("write the box's version: %w", err)
	}
	return true, nil
}
