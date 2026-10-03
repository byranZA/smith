package shipped_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/shipped"
)

// localBox is a box at the connection seam backed by the host's real
// filesystem: Run runs each command under sh with the box's TMPDIR, and Copy
// writes the file to the remote path as scp would. It tests what a shipped
// directory is, not how the command that made it is spelled.
type localBox struct {
	env []string
}

// newLocalBox returns a localBox whose TMPDIR is tmpdir. The box runs Ubuntu,
// so its mktemp is GNU coreutils; the host's mktemp is used only when it is
// GNU too (gmktemp on macOS), and the test skips, saying why, when neither is.
func newLocalBox(t *testing.T, tmpdir string) localBox {
	t.Helper()
	mktemp := gnuMktemp()
	if mktemp == "" {
		t.Skip("no GNU coreutils mktemp on this host to stand in for the box's (on macOS: brew install coreutils)")
	}
	bin := t.TempDir()
	if err := os.Symlink(mktemp, filepath.Join(bin, "mktemp")); err != nil {
		t.Fatalf("link GNU mktemp: %v", err)
	}
	return localBox{env: []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + tmpdir,
	}}
}

// gnuMktemp is the path of a GNU coreutils mktemp on the host, or "" if there
// is none.
func gnuMktemp() string {
	for _, name := range []string{"mktemp", "gmktemp"} {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		out, err := exec.Command(p, "--version").Output()
		if err == nil && strings.Contains(string(out), "GNU coreutils") {
			return p
		}
	}
	return ""
}

func (b localBox) Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	return b.RunWithInput(ctx, cmd, nil, stdout, stderr)
}

func (b localBox) RunWithInput(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	c.Env = b.env
	c.Stdin = stdin
	c.Stdout = stdout
	c.Stderr = stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("run %q: %w", cmd, err)
	}
	return nil
}

func (b localBox) Copy(_ context.Context, localPath, remotePath string) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read shipped file: %w", err)
	}
	if err := os.WriteFile(remotePath, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", remotePath, err)
	}
	return nil
}

// whereScript prints the directory its shipped copy lives in.
const whereScript = `printf '%s' "$(dirname "$0")"`

func TestEachCopyLivesInAPrivateDirectoryUnderTheBoxTMPDIR(t *testing.T) {
	tmp := t.TempDir()
	box := newLocalBox(t, tmp)
	dirs := map[string]bool{}
	for range 2 {
		var out bytes.Buffer
		s := shipped.New(box, "where.sh", whereScript)
		if err := s.Run(context.Background(), &out, io.Discard, "where"); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		dir := out.String()
		if filepath.Dir(dir) != tmp {
			t.Errorf("shipped into %q, want a directory of its own under TMPDIR %q", dir, tmp)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat shipped directory: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("shipped directory mode = %v, want %v", got, os.FileMode(0o700))
		}
		dirs[dir] = true
	}
	if len(dirs) != 2 {
		t.Errorf("two handles used directories %v, want two distinct ones", dirs)
	}
}

func TestCloseRemovesTheCopyFromARealFilesystem(t *testing.T) {
	box := newLocalBox(t, t.TempDir())
	var out bytes.Buffer
	s := shipped.New(box, "where.sh", whereScript)
	if err := s.Run(context.Background(), &out, io.Discard, "where"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	s.Close(context.Background())

	if _, err := os.Stat(out.String()); !os.IsNotExist(err) {
		t.Errorf("Stat(shipped directory) err = %v, want it removed", err)
	}
}
