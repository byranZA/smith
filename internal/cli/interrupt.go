package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// interruptGrace bounds how long an interrupted command has to unwind. It
// outlasts the few seconds a shipped script's removal may wait on the box, so
// cleanup gets its full chance, while a command blocked where its context
// cannot reach, such as a terminal prompt, still ends soon after the interrupt.
const interruptGrace = 7 * time.Second

// execute runs root under a context the operator's first interrupt (SIGINT or
// SIGTERM) cancels, and returns the process exit code. Cancelling, rather than
// dying on the signal, lets the running command return through its deferred
// cleanup, so a shipped script is still removed from the box.
//
// An interrupted run ends with the shell's 128+signal code and prints nothing
// more: the operator asked it to stop, and a cleanup that failed on the way out
// is not theirs to act on. Once interrupted, the signals revert to their
// default, so a second interrupt ends smith at once, and a command still
// running after grace is ended for it.
func execute(root *cobra.Command, grace time.Duration) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigs := make(chan os.Signal, 1)
	// Notify with no signals would catch every signal, not none.
	if caught := caughtSignals(); len(caught) > 0 {
		signal.Notify(sigs, caught...)
	}

	finished := make(chan struct{})
	interrupted := make(chan int, 1)
	go func() {
		defer close(interrupted)
		select {
		case <-finished:
		case sig := <-sigs:
			signal.Stop(sigs)
			code := signalExitCode(sig)
			interrupted <- code
			cancel()
			select {
			case <-finished:
			case <-time.After(grace):
				os.Exit(code)
			}
		}
	}()

	err := root.ExecuteContext(ctx)
	signal.Stop(sigs)
	close(finished)
	if code, ok := <-interrupted; ok {
		return code
	}
	return codeFromError(err)
}

// caughtSignals is the interrupts execute turns into a cancelled command. A
// signal smith was started ignoring, as a background job's SIGINT is, stays
// ignored: catching it would let an interrupt meant for someone else stop smith.
func caughtSignals() []os.Signal {
	var caught []os.Signal
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		if !signal.Ignored(sig) {
			caught = append(caught, sig)
		}
	}
	return caught
}

// signalExitCode is the exit code a shell reports for a process sig ended:
// 128 plus the signal number.
func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 128 + int(syscall.SIGINT)
}
