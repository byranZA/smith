package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
)

// gitIn runs git with args in dir, failing the test on error, and returns its trimmed output.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=smith", "-c", "user.email=smith@example.com"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// committingAgent is a fakeAgent that first commits a file named for its task in the directory it runs in.
type committingAgent struct {
	fakeAgent
	t *testing.T
}

func (c *committingAgent) Launch(ctx context.Context, cmd agent.Command) error {
	task := strings.Fields(strings.SplitN(cmd.Args[len(cmd.Args)-1], "issue **#", 2)[1])[0]
	if err := os.WriteFile(filepath.Join(cmd.Dir, task+".txt"), []byte(task+"\n"), 0o644); err != nil {
		return err
	}
	gitIn(c.t, cmd.Dir, "add", task+".txt")
	gitIn(c.t, cmd.Dir, "commit", "-m", "task "+task)
	return c.fakeAgent.Launch(ctx, cmd)
}

func TestLoopRunLandsEachTaskOnANewWorkBranchAndPushesItLeavingTheCheckoutAlone(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	remote, clone := filepath.Join(root, "remote.git"), filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", "-b", "main", remote)
	gitIn(t, root, "clone", remote, clone)
	gitIn(t, clone, "commit", "--allow-empty", "-m", "seed")
	gitIn(t, clone, "push", "origin", "main")
	if err := os.WriteFile(filepath.Join(clone, "wip.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkout := func() []string {
		return []string{gitIn(t, clone, "symbolic-ref", "HEAD"), gitIn(t, clone, "rev-parse", "HEAD"), gitIn(t, clone, "status", "--porcelain")}
	}
	before := checkout()
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43, 44), "43": taskJSON(43, "OPEN", "ready-for-agent"), "44": taskJSON(44, "OPEN", "ready-for-agent")}}
	w := loopWiring{
		gh:       gh,
		launcher: &committingAgent{fakeAgent: fakeAgent{gh: gh}, t: t},
		bench:    systemBench,
		pusher:   func(dir string) loop.Pusher { return loop.Origin{Git: connection.System(), Dir: dir} },
		lookPath: onPath,
		git:      connection.System(),
		workdir:  func() (string, error) { return clone, nil },
	}

	stdout, stderr, code := runLoopRun(t, w, "42")

	if code != 0 || !strings.HasSuffix(stdout, workBranch) {
		t.Fatalf("exit %d, stdout %q, stderr %q; want 0 and the work branch named", code, stdout, stderr)
	}
	landed := gitIn(t, remote, "log", "--format=%s", "smith/42-the-loop")
	if want := "task 44\ntask 43\nseed"; landed != want {
		t.Errorf("origin's smith/42-the-loop holds %q, want %q", landed, want)
	}
	if after := checkout(); !slices.Equal(after, before) {
		t.Errorf("operator's checkout branch, HEAD and status = %q, want them unchanged as %q", after, before)
	}
}
