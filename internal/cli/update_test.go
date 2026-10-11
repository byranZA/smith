package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/config"
	"github.com/byranZA/smith/internal/release"
	"github.com/byranZA/smith/internal/selfupdate"
)

// releaseServer stands in for GitHub, serving canned bodies by URL.
type releaseServer struct {
	bodies map[string][]byte
	asked  []string
}

func (r *releaseServer) Get(_ context.Context, url string) ([]byte, error) {
	r.asked = append(r.asked, url)
	body, ok := r.bodies[url]
	if !ok {
		return nil, fmt.Errorf("GET %s: 404 Not Found", url)
	}
	return body, nil
}

// newReleaseServer publishes darwin/arm64 versions whose binaries read "smith <version>".
func newReleaseServer(t *testing.T, latest string, versions ...string) *releaseServer {
	t.Helper()
	r := &releaseServer{bodies: map[string][]byte{
		release.LatestURL: []byte(`{"tag_name":"v` + latest + `"}`),
	}}
	for _, v := range versions {
		asset, err := release.ForPlatform(v, "darwin", "arm64")
		if err != nil {
			t.Fatalf("ForPlatform(%s): %v", v, err)
		}
		archive := smithArchive(t, "smith "+v)
		sum := sha256.Sum256(archive)
		r.bodies[asset.URL] = archive
		r.bodies[asset.ChecksumsURL] = []byte(hex.EncodeToString(sum[:]) + "  " + asset.Name + "\n")
	}
	return r
}

func (r *releaseServer) downloaded() bool {
	for _, url := range r.asked {
		if strings.HasSuffix(url, ".tar.gz") {
			return true
		}
	}
	return false
}

func smithArchive(t *testing.T, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "smith", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatalf("write tar entry: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// switchRecorder stands in for the process switch, recording what it would exec.
type switchRecorder struct {
	called bool
	path   string
	args   []string
}

func (s *switchRecorder) exec(path string, args []string) error {
	s.called, s.path, s.args = true, path, args
	return nil
}

// fleetSSH routes ssh by target to an upgradeSSH box, or fails it as unreachable.
type fleetSSH struct {
	boxes       map[string]*upgradeSSH
	unreachable map[string]bool
}

func (f *fleetSSH) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if name != "ssh" || len(args) < 2 {
		return nil
	}
	target := args[len(args)-2]
	if f.unreachable[target] {
		if _, err := io.WriteString(stderr, "ssh: connect to host: Connection timed out\n"); err != nil {
			return fmt.Errorf("write canned stderr: %w", err)
		}
		return relayExit(255)
	}
	box, ok := f.boxes[target]
	if !ok {
		return fmt.Errorf("no fake box at %s", target)
	}
	return box.Run(ctx, name, args, stdin, stdout, stderr)
}

func (f *fleetSSH) installed(target string) bool {
	box, ok := f.boxes[target]
	return ok && box.installCommand() != ""
}

// updateRig fakes everything `smith update` reaches.
type updateRig struct {
	version    string
	goos       string
	markerPath string
	server     *releaseServer
	binary     string
	switched   *switchRecorder
	term       *scriptedTerminal
	home       string
	ssh        *fleetSSH
	// predates makes the installed release one with no box step to switch into.
	predates bool
	// probeErr is what checking the installed release for the box step fails with.
	probeErr error
}

// newUpdateRig runs darwin/arm64 smith 0.2.0, unattended, with 0.3.0 the latest and no boxes.
func newUpdateRig(t *testing.T) *updateRig {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	binary := filepath.Join(dir, "smith")
	if err := os.WriteFile(binary, []byte("smith 0.2.0"), 0o755); err != nil {
		t.Fatalf("write installed binary: %v", err)
	}
	return &updateRig{
		version:    "0.2.0",
		goos:       "darwin",
		markerPath: filepath.Join(t.TempDir(), "bootstrap.json"),
		server:     newReleaseServer(t, "0.3.0", "0.1.0", "0.3.0"),
		binary:     binary,
		switched:   &switchRecorder{},
		term:       &scriptedTerminal{},
		home:       t.TempDir(),
		ssh:        &fleetSSH{boxes: map[string]*upgradeSSH{}},
	}
}

func (r *updateRig) run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	resolve := func() (config.Home, error) { return config.NewHome(r.home), nil }
	cmd := newUpdateCmd(updateWiring{
		updater: selfupdate.Updater{
			Version:    r.version,
			OS:         r.goos,
			Arch:       "arm64",
			MarkerPath: r.markerPath,
			Get:        r.server,
			Executable: func() (string, error) { return r.binary, nil },
			Continues:  func(context.Context, string) (bool, error) { return !r.predates, r.probeErr },
		},
		exec:     r.switched.exec,
		term:     r.term,
		home:     resolve,
		converge: convergeNamedBox(resolve, r.ssh),
	})
	cmd.SetArgs(args)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	code = codeFromError(cmd.Execute())
	return out.String(), errBuf.String(), code
}

func TestUpdateRefusesOnWindowsBeforeAnyDownload(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.goos = "windows"

	_, stderr, code := rig.run(t)

	if code == 0 {
		t.Fatalf("exit code = 0, want a refusal")
	}
	if len(rig.server.asked) != 0 {
		t.Errorf("GitHub was asked for %v, want nothing fetched", rig.server.asked)
	}
	if !strings.Contains(stderr, "docs/install.md") {
		t.Errorf("stderr = %q, want it to point at the manual install in docs/install.md", stderr)
	}
}

func TestUpdateOnABoxRefusesAndPointsAtTheOperatorsMachine(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	if err := os.WriteFile(rig.markerPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	_, stderr, code := rig.run(t)

	if code == 0 {
		t.Fatalf("exit code = 0, want a refusal")
	}
	if len(rig.server.asked) != 0 {
		t.Errorf("GitHub was asked for %v, want nothing fetched", rig.server.asked)
	}
	for _, want := range []string{"operator's machine", "smith machine upgrade <box>"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, does not contain %q", stderr, want)
		}
	}
}

func TestUpdateBehindTheLatestReleaseReplacesTheBinaryAndSwitchesIntoItForTheBoxStep(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)

	stdout, stderr, code := rig.run(t, "--boxes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := readBinary(t, rig.binary); got != "smith 0.3.0" {
		t.Errorf("installed binary = %q, want the 0.3.0 release", got)
	}
	if !strings.Contains(stdout, "0.2.0 → 0.3.0") {
		t.Errorf("stdout = %q, want it to report 0.2.0 → 0.3.0", stdout)
	}
	want := []string{rig.binary, "update", "--" + selfupdate.ContinueFlag, "--boxes"}
	if !rig.switched.called || rig.switched.path != rig.binary || strings.Join(rig.switched.args, " ") != strings.Join(want, " ") {
		t.Errorf("switched to %q with %q, want %q with %q", rig.switched.path, rig.switched.args, rig.binary, want)
	}
}

func readBinary(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestUpdateToANamedOlderVersionInstallsIt(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"0.1.0", "v0.1.0"} {
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			rig := newUpdateRig(t)

			_, stderr, code := rig.run(t, "--version", spelling)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			if got := readBinary(t, rig.binary); got != "smith 0.1.0" {
				t.Errorf("installed binary = %q, want the 0.1.0 release", got)
			}
		})
	}
}

func TestUpdateToAVersionWithNoReleaseRefusesBeforeAnyDownload(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)

	_, stderr, code := rig.run(t, "--version", "dev")

	if code == 0 {
		t.Fatalf("exit code = 0, want a refusal")
	}
	if len(rig.server.asked) != 0 {
		t.Errorf("GitHub was asked for %v, want nothing fetched", rig.server.asked)
	}
	if !strings.Contains(stderr, `"dev"`) {
		t.Errorf("stderr = %q, want it to name the version it refused", stderr)
	}
}

func TestUpdateWhenAlreadyCurrentSaysSoAndGoesOnToTheBoxStep(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.version = "0.3.0"

	stdout, stderr, code := rig.run(t)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if rig.server.downloaded() || rig.switched.called {
		t.Errorf("downloaded = %v, switched = %v; want neither for a current smith", rig.server.downloaded(), rig.switched.called)
	}
	for _, want := range []string{"local smith is already 0.3.0", "no boxes in the inventory"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
}

// withBoxes registers each name at smith@<name>, running smith 0.2.0 on x86_64.
func (r *updateRig) withBoxes(t *testing.T, names ...string) {
	t.Helper()
	entries := make([]string, 0, len(names))
	for _, name := range names {
		entries = append(entries, fmt.Sprintf(`%q:{"target":"smith@%s"}`, name, name))
		r.ssh.boxes["smith@"+name] = &upgradeSSH{machine: "x86_64", installed: "0.2.0"}
	}
	writeInventory(t, r.home, `{"schema_version":1,"boxes":{`+strings.Join(entries, ",")+`}}`)
}

// afterSwitch makes the rig the switched-into 0.3.0 binary, attended and answering answer.
func (r *updateRig) afterSwitch(answer string) {
	r.version = "0.3.0"
	r.term.attended, r.term.answer = true, answer
}

func TestUpdateWithBoxesUpgradesEveryBoxWithoutAsking(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha", "beta")
	rig.afterSwitch("")

	stdout, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag, "--boxes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if rig.term.asked {
		t.Errorf("asked %q, want no question under --boxes", rig.term.question)
	}
	for _, box := range []string{"alpha", "beta"} {
		if !rig.ssh.installed("smith@" + box) {
			t.Errorf("box %s was not upgraded", box)
		}
		if want := "box " + box + ": upgraded 0.2.0 → 0.3.0"; !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
}

func TestUpdateContinuingAfterTheSwitchDoesNotRepeatTheLocalUpdate(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha")
	rig.afterSwitch("")

	_, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag, "--boxes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(rig.server.asked) != 0 || rig.switched.called {
		t.Errorf("GitHub was asked for %v and switched = %v, want the local update left alone", rig.server.asked, rig.switched.called)
	}
}

func TestUpdateAsksOneQuestionForEveryBox(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha", "beta", "gamma")
	rig.afterSwitch("y")

	_, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "Upgrade 3 boxes (alpha, beta, gamma) to 0.3.0? [y/N] "; rig.term.asks != 1 || rig.term.question != want {
		t.Errorf("asked %d question(s), the last %q; want exactly %q", rig.term.asks, rig.term.question, want)
	}
	for _, box := range []string{"alpha", "beta", "gamma"} {
		if !rig.ssh.installed("smith@" + box) {
			t.Errorf("box %s was not upgraded after a yes", box)
		}
	}
}

func TestUpdateDeclinedUpgradesNoBoxAndPrintsTheUpgradeCommands(t *testing.T) {
	t.Parallel()
	for _, answer := range []string{"n", ""} {
		t.Run(fmt.Sprintf("answer %q", answer), func(t *testing.T) {
			t.Parallel()
			rig := newUpdateRig(t)
			rig.withBoxes(t, "alpha", "beta")
			rig.afterSwitch(answer)

			stdout, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			for _, box := range []string{"alpha", "beta"} {
				if rig.ssh.installed("smith@" + box) {
					t.Errorf("box %s was upgraded after a no", box)
				}
				if want := "smith machine upgrade " + box + "\n"; !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, does not contain %q", stdout, want)
				}
				if want := "box " + box + ": skipped"; !strings.Contains(stdout, want) {
					t.Errorf("stdout = %q, does not contain %q", stdout, want)
				}
			}
		})
	}
}

func TestUpdateUnattendedWithoutBoxesTouchesNoBoxAndPrintsOneCommandPerBox(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha", "beta")
	rig.version = "0.3.0"

	stdout, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if rig.term.asked {
		t.Errorf("asked %q, want no question with no terminal", rig.term.question)
	}
	for _, box := range []string{"alpha", "beta"} {
		if rig.ssh.installed("smith@" + box) {
			t.Errorf("box %s was upgraded unattended", box)
		}
		if got := strings.Count(stdout, "smith machine upgrade "+box+"\n"); got != 1 {
			t.Errorf("stdout = %q carries %d upgrade lines for %s, want 1", stdout, got, box)
		}
	}
}

func TestUpdateCarriesOnPastAnUnreachableBoxAndFailsNamingIt(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha", "beta")
	rig.ssh.unreachable = map[string]bool{"smith@alpha": true}
	rig.afterSwitch("")

	stdout, _, code := rig.run(t, "--"+selfupdate.ContinueFlag, "--boxes")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero for a failed box")
	}
	if !rig.ssh.installed("smith@beta") {
		t.Errorf("box beta was not upgraded after alpha failed")
	}
	for _, want := range []string{"box alpha: failed:", "box beta: upgraded 0.2.0 → 0.3.0", "local smith 0.3.0: the local update succeeded"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
}

func TestUpdateReportsABoxAlreadyOnTheVersionAsCurrent(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha")
	rig.ssh.boxes["smith@alpha"].installed = "0.3.0"
	rig.afterSwitch("")

	stdout, stderr, code := rig.run(t, "--"+selfupdate.ContinueFlag, "--boxes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if want := "box alpha: already current at 0.3.0"; !strings.Contains(stdout, want) {
		t.Errorf("stdout = %q, does not contain %q", stdout, want)
	}
}

func TestUpdateWithAMismatchedChecksumKeepsTheOldBinaryAndDoesNotSwitch(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	asset, err := release.ForPlatform("0.3.0", "darwin", "arm64")
	if err != nil {
		t.Fatalf("ForPlatform: %v", err)
	}
	rig.server.bodies[asset.URL] = smithArchive(t, "tampered smith")

	_, _, code := rig.run(t, "--boxes")

	if code == 0 {
		t.Fatalf("exit code = 0, want a failed update")
	}
	if got := readBinary(t, rig.binary); got != "smith 0.2.0" {
		t.Errorf("installed binary = %q, want the old 0.2.0 one", got)
	}
	if rig.switched.called {
		t.Errorf("switched to %q, want no switch after a failed install", rig.switched.path)
	}
}

func TestUpdateToAReleaseThatPredatesTheBoxStepPrintsTheUpgradeCommandsInsteadOfSwitching(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha", "beta")
	rig.predates = true

	stdout, stderr, code := rig.run(t, "--version", "0.1.0", "--boxes")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	if rig.switched.called {
		t.Errorf("switched to %q, want no switch into a smith with no box step", rig.switched.path)
	}
	for _, box := range []string{"alpha", "beta"} {
		if rig.ssh.installed("smith@" + box) {
			t.Errorf("box %s was upgraded by the old smith", box)
		}
		if want := "smith machine upgrade " + box + "\n"; !strings.Contains(stdout, want) {
			t.Errorf("stdout = %q, does not contain %q", stdout, want)
		}
	}
}

func TestUpdateWhoseProbeFailsExitsNonZeroWithoutSwitchingOrTouchingABox(t *testing.T) {
	t.Parallel()
	rig := newUpdateRig(t)
	rig.withBoxes(t, "alpha")
	rig.probeErr = errors.New("signal: killed")

	_, _, code := rig.run(t, "--boxes")

	if code == 0 {
		t.Fatalf("exit code = 0, want non-zero after a failed probe")
	}
	if rig.switched.called {
		t.Errorf("switched to %q, want no switch after a failed probe", rig.switched.path)
	}
	if rig.ssh.installed("smith@alpha") {
		t.Errorf("box alpha was upgraded after a failed probe")
	}
}
