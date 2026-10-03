//go:build unix

package cli

import (
	"bufio"
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
)

// The environment a re-executed test binary reads to become an interrupted
// smith. interruptRecordEnv's presence is what makes the helper test run.
const (
	interruptRecordEnv = "SMITH_INTERRUPT_RECORD"
	interruptModeEnv   = "SMITH_INTERRUPT_MODE"
	interruptGraceEnv  = "SMITH_INTERRUPT_GRACE"
)

// The ways the helper's command behaves once interrupted.
const (
	// modeUnwind returns when its context is cancelled, cleaning up on the way.
	modeUnwind = "unwind"
	// modeStuck never returns, as a command blocked on a terminal read would.
	modeStuck = "stuck"
)

// recordCleanup appends to record that the command's deferred cleanup ran.
func recordCleanup(record string) error {
	f, err := os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open cleanup record: %w", err)
	}
	if _, err := io.WriteString(f, "cleaned up\n"); err != nil {
		return errors.Join(fmt.Errorf("write cleanup record: %w", err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close cleanup record: %w", err)
	}
	return nil
}

// interruptedRun is what the parent sees of a child smith it interrupted.
type interruptedRun struct {
	code   int
	signal syscall.Signal
	stderr string
	record string
}

// runInterrupted interrupts a child smith whose command defers a cleanup.
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
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "started\n" {
		t.Fatalf("child smith did not start: line %q, err %v\n%s", line, err, stderr.String())
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
	cleanup, err := os.ReadFile(record)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read cleanup record: %v", err)
	}
	run.record = string(cleanup)
	return run
}

// TestHelperInterruptedSmith is runInterrupted's child process body, a no-op when run directly.
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
	root := &cobra.Command{
		Use:           "smith",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			//nolint:errcheck // a failed record shows up as a missing cleanup in the parent
			defer recordCleanup(record)
			fmt.Println("started")
			if mode == modeStuck {
				select {}
			}
			<-ctx.Done()
			return fmt.Errorf("run command: %w", ctx.Err())
		},
	}
	root.SetArgs(nil)
	os.Exit(execute(root, grace))
}

func TestInterruptLetsTheCommandCleanUp(t *testing.T) {
	run := runInterrupted(t, modeUnwind, time.Minute, 1)

	if want := "cleaned up\n"; run.record != want {
		t.Errorf("cleanup record after the interrupt = %q, want %q", run.record, want)
	}
}

func TestInterruptExitsWithTheInterruptCode(t *testing.T) {
	run := runInterrupted(t, modeUnwind, time.Minute, 1)

	if run.signal != 0 || run.code != 130 || run.stderr != "" {
		t.Errorf("interrupted smith ended with code %d signal %v, stderr %q; want code 130 and no output", run.code, run.signal, run.stderr)
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
