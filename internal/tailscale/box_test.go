package tailscale

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/bootstrap"
)

// stubScript stands in for the shipped bootstrap.sh: it answers each
// subcommand the box driver runs and leaves a trace beside itself, so a test
// can tell the copied file is the one that ran.
const stubScript = `case "$1" in
tailscale-status) echo tailscale-ip=100.64.0.1 ;;
enroll) read -r key; [ "$key" = tskey-test ] && echo tailscale-ip=100.64.0.2 ;;
close-public-ssh) touch "$(dirname "$0")/closed" ;;
esac
`

// localBox is one box reachable at several addresses. Each address it is
// dialled at runs commands in a real local bash, as the box's login shell
// would, and records which address every call travelled to. Shipped
// directories are made under a TMPDIR holding whitespace, an apostrophe and
// shell metacharacters, as a box's may. A copy of bootstrap.sh lands as
// stubScript, so the box answers without root.
type localBox struct {
	bash   string
	tmpdir string
	// calls is every call made, as "<host> <command>"; a copy is "<host> scp".
	calls []string
}

func newLocalBox(t *testing.T) *localBox {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	tmpdir := filepath.Join(t.TempDir(), "smith's dir; $HOME")
	if err := os.Mkdir(tmpdir, 0o700); err != nil {
		t.Fatalf("make the box's TMPDIR: %v", err)
	}
	return &localBox{bash: bash, tmpdir: tmpdir}
}

// dial reaches the box at host.
func (b *localBox) dial(host string) Remote { return boxAt{box: b, host: host} }

// ranAt reports whether a command containing want ran at host.
func (b *localBox) ranAt(host, want string) bool {
	for _, c := range b.calls {
		if strings.HasPrefix(c, host+" ") && strings.Contains(c, want) {
			return true
		}
	}
	return false
}

// shippedDirs lists what the box's TMPDIR holds.
func (b *localBox) shippedDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(b.tmpdir)
	if err != nil {
		t.Fatalf("read the box's TMPDIR: %v", err)
	}
	var dirs []string
	for _, e := range entries {
		dirs = append(dirs, e.Name())
	}
	return dirs
}

// boxAt is the box reached at one address.
type boxAt struct {
	box  *localBox
	host string
}

func (a boxAt) Copy(_ context.Context, localPath, remotePath string) error {
	a.box.calls = append(a.box.calls, a.host+" scp")
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read the copied file: %w", err)
	}
	if string(data) != bootstrap.Script {
		return errors.New("copied something other than bootstrap.sh")
	}
	if err := os.WriteFile(remotePath, []byte(stubScript), 0o600); err != nil {
		return fmt.Errorf("write the shipped script: %w", err)
	}
	return nil
}

func (a boxAt) Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error {
	return a.RunWithInput(ctx, remoteCmd, nil, stdout, stderr)
}

func (a boxAt) RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	a.box.calls = append(a.box.calls, a.host+" "+remoteCmd)
	if strings.HasPrefix(remoteCmd, "mktemp -d") {
		// The box's mktemp is GNU's, which the host's may not be: make the
		// private directory under the box's TMPDIR as it would.
		dir, err := os.MkdirTemp(a.box.tmpdir, "smith.")
		if err != nil {
			return fmt.Errorf("make the shipped directory: %w", err)
		}
		_, err = fmt.Fprintln(stdout, dir)
		return err
	}
	cmd := exec.CommandContext(ctx, a.box.bash, "-c", remoteCmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bash -c %q: %w", remoteCmd, err)
	}
	return nil
}

func TestBoxDriverReadsStatusFromAScriptItShippedOverThePublicHost(t *testing.T) {
	box := newLocalBox(t)
	driver := NewBox(box.dial, "203.0.113.10")
	defer driver.Close(context.Background())

	ip, err := driver.CurrentIP(context.Background())
	if err != nil {
		t.Fatalf("CurrentIP() error = %v", err)
	}
	if ip != "100.64.0.1" {
		t.Errorf("CurrentIP() = %q, want the shipped script's tailnet IP", ip)
	}
	if !box.ranAt("203.0.113.10", "scp") || !box.ranAt("203.0.113.10", "tailscale-status") {
		t.Errorf("calls = %q, want the script shipped and run over the public host", box.calls)
	}
}

func TestBoxDriverEnrollsThroughTheScriptItShipped(t *testing.T) {
	box := newLocalBox(t)
	driver := NewBox(box.dial, "203.0.113.10")
	defer driver.Close(context.Background())

	ip, err := driver.Enroll(context.Background(), EnrollOptions{Host: "dev", AuthKey: "tskey-test"})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if ip != "100.64.0.2" {
		t.Errorf("Enroll() = %q, want the shipped script's tailnet IP", ip)
	}
}

func TestBoxDriverMovedToTheTailnetClosesPublicSSHAndCleansUpThere(t *testing.T) {
	box := newLocalBox(t)
	driver := NewBox(box.dial, "203.0.113.10")
	if _, err := driver.CurrentIP(context.Background()); err != nil {
		t.Fatalf("CurrentIP() error = %v", err)
	}
	dirs := box.shippedDirs(t)
	if len(dirs) != 1 {
		t.Fatalf("TMPDIR holds %q, want the one shipped directory", dirs)
	}
	shipped := filepath.Join(box.tmpdir, dirs[0])

	driver.MoveToTailnet("100.64.0.2")
	if err := driver.ClosePublicSSH(context.Background()); err != nil {
		t.Fatalf("ClosePublicSSH() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(shipped, "closed")); err != nil {
		t.Errorf("the shipped script did not run close-public-ssh: %v", err)
	}
	driver.Close(context.Background())

	if !box.ranAt("100.64.0.2", "close-public-ssh") {
		t.Errorf("calls = %q, want close-public-ssh run over the tailnet", box.calls)
	}
	if !box.ranAt("100.64.0.2", "rm -rf") {
		t.Errorf("calls = %q, want the shipped directory removed over the tailnet", box.calls)
	}
	if left := box.shippedDirs(t); len(left) > 0 {
		t.Errorf("TMPDIR still holds %q, want the shipped directory gone", left)
	}
}

func TestBoxDriverCleansUpOverThePublicHostBeforeAMove(t *testing.T) {
	box := newLocalBox(t)
	driver := NewBox(box.dial, "203.0.113.10")
	if _, err := driver.CurrentIP(context.Background()); err != nil {
		t.Fatalf("CurrentIP() error = %v", err)
	}

	driver.Close(context.Background())

	if !box.ranAt("203.0.113.10", "rm -rf") {
		t.Errorf("calls = %q, want the shipped directory removed over the public host", box.calls)
	}
	if left := box.shippedDirs(t); len(left) > 0 {
		t.Errorf("TMPDIR still holds %q, want the shipped directory gone", left)
	}
}

func TestBoxDriverThatShippedNothingReachesNothingOnClose(t *testing.T) {
	box := newLocalBox(t)

	NewBox(box.dial, "203.0.113.10").Close(context.Background())

	if len(box.calls) > 0 {
		t.Errorf("calls = %q, want nothing run for a driver that shipped nothing", box.calls)
	}
}
