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
	"github.com/byranZA/smith/internal/loop"
)

// hangupDirEnv names the directory a re-executed test binary, run as a hung-up loop, writes its records to.
const hangupDirEnv = "SMITH_HANGUP_DIR"

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

// hangUpLoop starts a child `smith loop run` whose agent sleeps, hangs it up once the agent is running, and waits for it.
func hangUpLoop(t *testing.T) hungUpLoop {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHungUpLoop$") // #nosec G204 -- re-executes this test binary.
	cmd.Env = append(os.Environ(), hangupDirEnv+"="+dir)
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
	for _, pid := range agents {
		if syscall.Kill(pid, 0) == nil {
			run.agents = append(run.agents, pid)
		}
	}
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

// TestHelperHungUpLoop is hangUpLoop's child process body, a no-op when run directly.
func TestHelperHungUpLoop(t *testing.T) {
	dir := os.Getenv(hangupDirEnv)
	if dir == "" {
		t.Skip("runs only as the child process of hangUpLoop")
	}
	gh := &fakeTracker{issues: map[string]string{"42": specJSON(43), "43": taskJSON(43, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, sleepingAgent{pidfile: filepath.Join(dir, "agent.pid")}, "agent: claude\n")
	root := newLoopCmd(inRepo(t, w))
	root.SetArgs([]string{"run", "42"})
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
