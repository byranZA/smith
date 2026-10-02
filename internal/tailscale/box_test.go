package tailscale

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// localShell is a Remote that runs each command in a real local bash, as the
// box's login shell would, so a test sees exactly which file a command runs.
type localShell struct{ bash string }

func (l localShell) Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error {
	return l.RunWithInput(ctx, remoteCmd, nil, stdout, stderr)
}

func (l localShell) RunWithInput(ctx context.Context, remoteCmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, l.bash, "-c", remoteCmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("bash -c %q: %w", remoteCmd, err)
	}
	return nil
}

// stubScript stands in for the shipped bootstrap.sh: it answers each
// subcommand the box driver runs and leaves a trace beside itself, so a test
// can tell the copied file is the one that ran.
const stubScript = `case "$1" in
tailscale-status) echo tailscale-ip=100.64.0.1 ;;
enroll) read -r key; [ "$key" = tskey-test ] && echo tailscale-ip=100.64.0.2 ;;
close-public-ssh) touch "$(dirname "$0")/closed" ;;
esac
`

// shipStub copies stubScript into a directory whose name holds whitespace, an
// apostrophe and shell metacharacters, as a box's TMPDIR may, and returns its
// path.
func shipStub(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "smith's dir; $HOME")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("make shipped directory: %v", err)
	}
	script := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(script, []byte(stubScript), 0o600); err != nil {
		t.Fatalf("write shipped script: %v", err)
	}
	return script
}

func newLocalShell(t *testing.T) localShell {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	return localShell{bash: bash}
}

func TestBoxDriverReadsStatusFromTheShippedScript(t *testing.T) {
	box := NewBox(newLocalShell(t), shipStub(t))
	ip, err := box.CurrentIP(context.Background())
	if err != nil {
		t.Fatalf("CurrentIP() error = %v", err)
	}
	if ip != "100.64.0.1" {
		t.Errorf("CurrentIP() = %q, want the shipped script's tailnet IP", ip)
	}
}

func TestBoxDriverEnrollsThroughTheShippedScript(t *testing.T) {
	box := NewBox(newLocalShell(t), shipStub(t))
	ip, err := box.Enroll(context.Background(), EnrollOptions{Host: "dev", AuthKey: "tskey-test"})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if ip != "100.64.0.2" {
		t.Errorf("Enroll() = %q, want the shipped script's tailnet IP", ip)
	}
}

func TestBoxDriverClosesPublicSSHThroughTheShippedScript(t *testing.T) {
	script := shipStub(t)
	box := NewBox(newLocalShell(t), script)
	if err := box.ClosePublicSSH(context.Background()); err != nil {
		t.Fatalf("ClosePublicSSH() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(script), "closed")); err != nil {
		t.Errorf("shipped script did not run close-public-ssh: %v", err)
	}
}
