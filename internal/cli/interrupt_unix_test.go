//go:build unix

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/byranZA/smith/internal/bootstrap"
)

// The environment a re-executed test binary reads to become an interrupted
// smith. interruptRecordEnv's presence is what makes the helper test run.
const (
	interruptRecordEnv  = "SMITH_INTERRUPT_RECORD"
	interruptModeEnv    = "SMITH_INTERRUPT_MODE"
	interruptGraceEnv   = "SMITH_INTERRUPT_GRACE"
	interruptShippedDir = "/tmp/smith.interrupt"
)

// The ways the helper's command behaves once interrupted.
const (
	// modeUnwind returns when its context is cancelled, cleaning up on the way.
	modeUnwind = "unwind"
	// modeFailedCleanup unwinds, but the box refuses the remove.
	modeFailedCleanup = "failed-cleanup"
	// modeStuck never returns, as a command blocked on a terminal read would.
	modeStuck = "stuck"
)

// recordingBox is a box at the bootstrap.Conn seam that appends every command
// run against it, and whether its context was still live, to a file the parent
// test process reads after the child has exited.
type recordingBox struct {
	record       string
	refuseRemove bool
}

func (b recordingBox) Copy(context.Context, string, string) error { return nil }

func (b recordingBox) Run(ctx context.Context, remoteCmd string, stdout, _ io.Writer) error {
	if strings.HasPrefix(remoteCmd, "mktemp") {
		_, err := fmt.Fprintln(stdout, interruptShippedDir)
		return err
	}
	f, err := os.OpenFile(b.record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s live=%t\n", remoteCmd, ctx.Err() == nil); err != nil {
		return err
	}
	if b.refuseRemove {
		return errors.New("rm: permission denied")
	}
	return nil
}

// interruptedRun is what the parent sees of a child smith it interrupted.
type interruptedRun struct {
	code   int
	signal syscall.Signal
	stderr string
	box    string
}

// runInterrupted starts a child smith whose command ships a script, sends it
// interrupts once the script is shipped, and reports how the child ended.
func runInterrupted(t *testing.T, mode string, grace time.Duration, interrupts int) interruptedRun {
	t.Helper()
	record := filepath.Join(t.TempDir(), "box")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperInterruptedSmith$") // #nosec G204 -- re-executes this test binary.
	cmd.Env = append(os.Environ(),
		interruptRecordEnv+"="+record,
		interruptModeEnv+"="+mode,
		interruptGraceEnv+"="+grace.String(),
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("pipe child stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child smith: %v", err)
	}
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "shipped\n" {
		t.Fatalf("child smith did not ship: line %q, err %v\n%s", line, err, stderr.String())
	}
	for range interrupts {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Fatalf("interrupt child smith: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(20 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("interrupted child smith did not exit")
	}

	run := interruptedRun{stderr: stderr.String()}
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok {
		t.Fatalf("child wait status is %T, want syscall.WaitStatus", cmd.ProcessState.Sys())
	}
	if status.Signaled() {
		run.signal = status.Signal()
	} else {
		run.code = status.ExitStatus()
	}
	box, err := os.ReadFile(record)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read box record: %v", err)
	}
	run.box = string(box)
	return run
}

// TestHelperInterruptedSmith is the child process body of runInterrupted and
// does nothing when run directly: smith running a command that ships a script
// and then waits to be interrupted.
func TestHelperInterruptedSmith(t *testing.T) {
	record := os.Getenv(interruptRecordEnv)
	if record == "" {
		t.Skip("runs only as the child process of runInterrupted")
	}
	mode := os.Getenv(interruptModeEnv)
	grace, err := time.ParseDuration(os.Getenv(interruptGraceEnv))
	if err != nil {
		t.Fatalf("parse grace: %v", err)
	}
	box := recordingBox{record: record, refuseRemove: mode == modeFailedCleanup}
	root := &cobra.Command{
		Use:           "smith",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			shipped, err := bootstrap.Ship(ctx, box, "echo hi")
			if err != nil {
				return err
			}
			defer shipped.Remove(ctx)
			fmt.Println("shipped")
			if mode == modeStuck {
				select {}
			}
			<-ctx.Done()
			return fmt.Errorf("run script: %w", ctx.Err())
		},
	}
	root.SetArgs(nil)
	os.Exit(execute(root, grace))
}

func TestInterruptRemovesTheShippedDirectory(t *testing.T) {
	run := runInterrupted(t, modeUnwind, time.Minute, 1)

	want := "rm -rf -- '" + interruptShippedDir + "' live=true\n"
	if run.box != want {
		t.Errorf("box saw %q after the interrupt, want %q", run.box, want)
	}
}

func TestInterruptExitsWithTheInterruptCode(t *testing.T) {
	run := runInterrupted(t, modeUnwind, time.Minute, 1)

	if run.signal != 0 || run.code != 130 {
		t.Errorf("interrupted smith ended with code %d signal %v, want code 130", run.code, run.signal)
	}
}

func TestInterruptWithAFailedCleanupPrintsNothing(t *testing.T) {
	run := runInterrupted(t, modeFailedCleanup, time.Minute, 1)

	if run.code != 130 || run.stderr != "" {
		t.Errorf("failed cleanup ended with code %d, stderr %q; want code 130 and no output", run.code, run.stderr)
	}
}

func TestInterruptedCommandThatCannotUnwindExitsAfterTheGrace(t *testing.T) {
	run := runInterrupted(t, modeStuck, 200*time.Millisecond, 1)

	if run.signal != 0 || run.code != 130 {
		t.Errorf("stuck smith ended with code %d signal %v, want code 130", run.code, run.signal)
	}
}

func TestSecondInterruptEndsSmithAtOnce(t *testing.T) {
	run := runInterrupted(t, modeStuck, time.Minute, 2)

	if run.signal != syscall.SIGINT {
		t.Errorf("twice-interrupted smith ended with code %d signal %v, want killed by SIGINT", run.code, run.signal)
	}
}
