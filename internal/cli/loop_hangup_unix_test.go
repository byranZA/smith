//go:build unix

package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/byranZA/smith/internal/agent"
	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/loop"
)

// hangupDirEnv names the directory a re-executed test binary, run as a hung-up loop, writes its records to.
const hangupDirEnv = "SMITH_HANGUP_DIR"

// hangupSessionEnv names the worktree of the loop-driven session a re-executed test binary runs as the root process of, when set.
const hangupSessionEnv = "SMITH_HANGUP_SESSION"

// sleepingAgent runs a stand-in agent in place of cmd's, one that writes its own pid and its sleeping child's to pidfile and waits.
type sleepingAgent struct {
	pidfile string
}

func (s sleepingAgent) Launch(ctx context.Context, cmd agent.Command) error {
	cmd.Name, cmd.Dir = "sh", ""
	cmd.Args = []string{"-c", `sleep 30 & echo $$ $! > "$0"; wait`, s.pidfile}
	return loop.Process{Stdout: os.Stdout, Stderr: os.Stderr}.Launch(ctx, cmd)
}

// hungUpLoop is what the parent sees of a child loop run it hung up.
type hungUpLoop struct {
	code   int
	signal syscall.Signal
	agents []int
	ran    []string
}

// hangUpLoop starts a child `smith loop run` whose agent sleeps, hangs it up once the agent is running, and waits for it. The child runs with env added to its environment.
func hangUpLoop(t *testing.T, env ...string) hungUpLoop {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHungUpLoop$") // #nosec G204 -- re-executes this test binary.
	cmd.Env = append(append(os.Environ(), hangupDirEnv+"="+dir), env...)
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatalf("create the child's output: %v", err)
	}
	t.Cleanup(func() {
		if err := output.Close(); err != nil {
			t.Errorf("close the child's output: %v", err)
		}
	})
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child loop: %v", err)
	}
	agents := waitForPids(t, filepath.Join(dir, "agent.pid"))
	t.Cleanup(func() {
		for _, pid := range agents {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("hang up child loop: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("hung-up child loop did not exit")
	}

	run := hungUpLoop{}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("child wait status is %T, want syscall.WaitStatus", cmd.ProcessState.Sys())
	}
	if status.Signaled() {
		run.signal = status.Signal()
	} else {
		run.code = status.ExitStatus()
	}
	run.agents = survivors(agents)
	ran, err := os.ReadFile(filepath.Join(dir, "gh"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read the child's gh record: %v", err)
	}
	run.ran = strings.Split(strings.TrimSpace(string(ran)), "\n")
	return run
}

// waitForPids returns the pids written to pidfile, once it is written.
func waitForPids(t *testing.T, pidfile string) []int {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		b, err := os.ReadFile(pidfile)
		if err != nil || !strings.HasSuffix(string(b), "\n") {
			continue
		}
		var pids []int
		for _, field := range strings.Fields(string(b)) {
			pid, err := strconv.Atoi(field)
			if err != nil {
				t.Fatalf("parse agent pid %q: %v", field, err)
			}
			pids = append(pids, pid)
		}
		return pids
	}
	t.Fatal("the child loop's agent never wrote its pids")
	return nil
}

// survivors returns those of pids still running after a grace period, which gives an orphaned agent killed with its loop time to be reaped by init.
func survivors(pids []int) []int {
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		alive := slices.DeleteFunc(slices.Clone(pids), func(pid int) bool { return syscall.Kill(pid, 0) != nil })
		if len(alive) == 0 || time.Now().After(deadline) {
			return alive
		}
	}
}

func TestHelperHungUpLoop(t *testing.T) {
	dir := os.Getenv(hangupDirEnv)
	if dir == "" {
		t.Skip("runs only as the child process of hangUpLoop")
	}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, sleepingAgent{pidfile: filepath.Join(dir, "agent.pid")}, "presets:\n  claude: {agent: claude}\nroles:\n  work: claude\n")
	args := []string{"run", "42"}
	if worktree := os.Getenv(hangupSessionEnv); worktree != "" {
		w.git, w.workdir = connection.System(), func() (string, error) { return worktree, nil }
		args = append(args, "--branch=smith/42-the-loop", "--in-session")
	}
	root := newLoopCmd(inRepo(t, w))
	root.SetArgs(args)
	root.SilenceUsage, root.SilenceErrors = true, true

	code := execute(root, time.Minute)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(strings.Join(gh.ran, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("record gh commands: %v", err)
	}
	os.Exit(code)
}

func TestHangupEndsALoopRunWithItsAgentStopped(t *testing.T) {
	run := hangUpLoop(t)

	if run.signal != 0 || run.code != 129 || len(run.agents) != 0 {
		t.Errorf("hung-up loop ended with code %d signal %v, agent pids %v still running; want code 129 and no agent left", run.code, run.signal, run.agents)
	}
}

func TestHangupRemovesTheWorkingLabelFromTheTask(t *testing.T) {
	run := hangUpLoop(t)

	if want := "gh issue edit 43 --remove-label smith:working"; !slices.Contains(run.ran, want) {
		t.Errorf("hung-up loop ran gh %q; want it to include %q", run.ran, want)
	}
}

func TestHangupOfALoopDrivenSessionShowsItStopped(t *testing.T) {
	tmux, resolve, dir := loopSession(t)
	hangUpLoop(t, hangupSessionEnv+"="+dir)
	stopSession(t, resolve, tmux, "smith-smith-42-the-loop")

	row := listedRow(t, resolve, tmux, "smith-smith-42-the-loop")

	if got := strings.Join(row[1:], " "); got != "stopped - 0 loop stopped" {
		t.Errorf("session list row = %q, want %q", got, "stopped - 0 loop stopped")
	}
}
