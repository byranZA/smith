package shipped_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/byranZA/smith/internal/connection"
)

// box is an in-memory box at the connection seam. It reads each remote command
// as a shell would, models a temp filesystem that mktemp, scp and rm -rf act
// on, and runs a shipped script only from a copy that is actually on it.
type box struct {
	mu sync.Mutex

	tmpdir       string
	mktempStderr string
	copyErr      error
	rmErr        error
	unreachable  bool
	unresponsive bool
	scriptStdout string
	scriptStderr string
	scriptErr    error

	made    int
	dirs    map[string]map[string]string
	runs    []run
	removed []removal
	sent    int
}

// run is one execution of a shipped script on the box.
type run struct {
	over   string
	dir    string
	script string
	args   []string
	stdin  string
}

// removal is one rm -rf the box received.
type removal struct {
	dir    string
	ctxErr error
}

func newBox() *box {
	return &box{tmpdir: "/tmp", dirs: map[string]map[string]string{}}
}

// login returns a connection to the box named name.
func (b *box) login(name string) *boxConn { return &boxConn{box: b, name: name} }

type boxConn struct {
	box  *box
	name string
	gone bool
}

func (c *boxConn) Copy(_ context.Context, localPath, remotePath string) error {
	b := c.box
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent++
	if b.unreachable || c.gone {
		return fmt.Errorf("scp: %w", connection.ErrConnect)
	}
	if b.copyErr != nil {
		return b.copyErr
	}
	files, ok := b.dirs[path.Dir(remotePath)]
	if !ok {
		return fmt.Errorf("scp: %s: No such file or directory", path.Dir(remotePath))
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file: %w", err)
	}
	files[path.Base(remotePath)] = string(data)
	return nil
}

func (c *boxConn) Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error {
	return c.RunWithInput(ctx, cmd, nil, stdout, stderr)
}

func (c *boxConn) RunWithInput(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	b := c.box
	b.mu.Lock()
	b.sent++
	if b.unreachable {
		b.mu.Unlock()
		return fmt.Errorf("ssh: %w", connection.ErrConnect)
	}
	words, err := shellWords(cmd)
	if err != nil {
		b.mu.Unlock()
		return err
	}
	if c.gone {
		b.mu.Unlock()
		return fmt.Errorf("ssh %s: %w", c.name, connection.ErrConnect)
	}
	if words[0] == "rm" && b.unresponsive {
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("ssh: %w", ctx.Err())
		case <-time.After(30 * time.Second):
			return errors.New("ssh: box never answered")
		}
	}
	defer b.mu.Unlock()
	switch words[0] {
	case "mktemp":
		return b.mktemp(stdout, stderr)
	case "rm":
		return b.rm(ctx, words)
	case "bash":
		return b.bash(c.name, words[1:], stdin, stdout, stderr)
	}
	return fmt.Errorf("sh: %s: command not found", words[0])
}

func (b *box) mktemp(stdout, stderr io.Writer) error {
	if b.mktempStderr != "" {
		if _, err := io.WriteString(stderr, b.mktempStderr); err != nil {
			return err
		}
		return errors.New("exit status 1")
	}
	b.made++
	dir := fmt.Sprintf("%s/smith.%08d", b.tmpdir, b.made)
	b.dirs[dir] = map[string]string{}
	_, err := fmt.Fprintln(stdout, dir)
	return err
}

func (b *box) rm(ctx context.Context, words []string) error {
	if len(words) != 4 || words[1] != "-rf" || words[2] != "--" {
		return fmt.Errorf("unexpected rm %q", words)
	}
	b.removed = append(b.removed, removal{dir: words[3], ctxErr: ctx.Err()})
	if b.rmErr != nil {
		return b.rmErr
	}
	delete(b.dirs, words[3])
	return nil
}

func (b *box) bash(over string, words []string, stdin io.Reader, stdout, stderr io.Writer) error {
	dir, file := path.Split(words[0])
	dir = strings.TrimSuffix(dir, "/")
	script, ok := b.dirs[dir][file]
	if !ok {
		return fmt.Errorf("bash: %s: No such file or directory", words[0])
	}
	var input []byte
	if stdin != nil {
		var err error
		if input, err = io.ReadAll(stdin); err != nil {
			return err
		}
	}
	b.runs = append(b.runs, run{over: over, dir: dir, script: script, args: words[1:], stdin: string(input)})
	if _, err := io.WriteString(stdout, b.scriptStdout); err != nil {
		return err
	}
	if _, err := io.WriteString(stderr, b.scriptStderr); err != nil {
		return err
	}
	return b.scriptErr
}

// leftovers lists the private directories still on the box.
func (b *box) leftovers() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var dirs []string
	for d := range b.dirs {
		dirs = append(dirs, d)
	}
	return dirs
}

// shellWords splits cmd into words as a POSIX shell would for the subset smith
// sends: single quotes, backslash escapes and plain words. An unquoted shell
// metacharacter is an error, since a real shell would act on it.
func shellWords(cmd string) ([]string, error) {
	var words []string
	var w strings.Builder
	inWord, quoted := false, false
	for i := 0; i < len(cmd); i++ {
		ch := cmd[i]
		switch {
		case quoted && ch == '\'':
			quoted = false
		case quoted:
			w.WriteByte(ch)
		case ch == '\'':
			quoted, inWord = true, true
		case ch == '\\' && i+1 < len(cmd):
			i++
			w.WriteByte(cmd[i])
			inWord = true
		case ch == ' ' || ch == '\t':
			if inWord {
				words = append(words, w.String())
				w.Reset()
				inWord = false
			}
		case strings.IndexByte("$`;|&()<>\"*?\n", ch) >= 0:
			return nil, fmt.Errorf("sh: unquoted %q in %q", ch, cmd)
		default:
			w.WriteByte(ch)
			inWord = true
		}
	}
	if quoted {
		return nil, fmt.Errorf("sh: unterminated quote in %q", cmd)
	}
	if inWord {
		words = append(words, w.String())
	}
	if len(words) == 0 {
		return nil, errors.New("sh: empty command")
	}
	return words, nil
}
