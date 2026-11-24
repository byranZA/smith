package loop

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/byranZA/smith/internal/tailbuf"
	"github.com/byranZA/smith/internal/tracker"
)

// stackUp brings the stack of the prepared worktree at dir up with the Stack,
// when there is one, as prepare holds its output back. A failure is an error
// naming task and dir, since another agent attempt cannot fix it.
func (l Loop) stackUp(ctx context.Context, task tracker.Task, dir string) error {
	if l.Stack == nil {
		return nil
	}
	if err := l.report("smith: bringing up the stack of #%d\n", task.Number); err != nil {
		return err
	}
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	if err := l.Stack.Ensure(ctx, dir, &captured); err != nil {
		if rerr := l.reportTail(&captured); rerr != nil {
			return rerr
		}
		wrapped := fmt.Errorf("bring up the stack of #%d in %s: %w (system packages and groups come from the blueprint, not the repo file)", task.Number, dir, err)
		return upFailure{error: wrapped, output: reportOutput(&captured)}
	}
	return nil
}

// ReportTailLimit bounds how much of a failed up's output a stop report
// carries, well under GitHub's comment limit.
const ReportTailLimit = 4 << 10

// upFailure is a failed stack up that keeps the last of up's output for the
// stop report, while its message stays the wrapped error alone.
type upFailure struct {
	error
	output string
}

func (f upFailure) Unwrap() error { return f.error }

// reportOutput renders the last ReportTailLimit bytes of b from a full line
// (or a partial one when no full line fits), with a line saying so when earlier output was cut, or "" when b is empty.
func reportOutput(b *tailbuf.Buffer) string {
	cut := b.Dropped()
	out := b.Bytes()
	if len(out) > ReportTailLimit {
		out = out[len(out)-ReportTailLimit:]
		cut = true
	}
	if cut {
		// Start at the next full line, unless none is left to show: a partial
		// line beats no output.
		if i := bytes.IndexByte(out, '\n'); i >= 0 && i+1 < len(out) {
			out = out[i+1:]
		}
	}
	if len(out) == 0 {
		return ""
	}
	text := string(out)
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if cut {
		text = fmt.Sprintf("(earlier output was cut; showing the last %d KiB)\n", ReportTailLimit>>10) + text
	}
	return text
}

// recoverStack takes down a stack a dead run left behind, saying so. A failed
// recovery only warns, since the run's own up fails loudly if something still
// holds what it needs; only a failed progress write is an error.
func (l Loop) recoverStack(ctx context.Context) error {
	if l.Recoverer == nil {
		return nil
	}
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	rec, err := l.Recoverer.Recover(ctx, &captured)
	if rec == nil {
		return l.warnIf(err, "recover the stack a dead run left")
	}
	if werr := l.report("smith: recovering the stack of work branch %s left in %s\n", rec.Branch, rec.Dir); werr != nil {
		return werr
	}
	if err == nil {
		return nil
	}
	if werr := l.warnIf(err, "the stack left behind was not taken down"); werr != nil {
		return werr
	}
	return l.reportTail(&captured)
}

// stackDown takes down the stack the run brought up, when there is one,
// warning with the last of down's output when it fails, since the run's
// outcome stands whether or not the stack stopped. It returns why the stack
// did not stop, and separately any failed progress write.
func (l Loop) stackDown(ctx context.Context) (failure, err error) {
	if l.Stack == nil {
		return nil, nil
	}
	captured := tailbuf.Buffer{Limit: PrepareTailLimit}
	failure = l.Stack.Release(ctx, &captured)
	if failure == nil {
		return nil, nil
	}
	if werr := l.warnIf(failure, "the stack did not stop"); werr != nil {
		return failure, werr //nolint:wrapcheck // the stack's own failure is reported as it came, beside the write error.
	}
	return failure, l.reportTail(&captured) //nolint:wrapcheck // the stack's own failure is reported as it came, beside the write error.
}

// reportTail writes the output captured in b to Progress. When b dropped
// earlier output, it starts at the next full line, after a note saying so.
func (l Loop) reportTail(b *tailbuf.Buffer) error {
	out := b.Lines()
	if b.Dropped() {
		if err := l.report("smith: earlier preparation output was cut; showing the last %d KiB\n", PrepareTailLimit>>10); err != nil {
			return err
		}
	}
	if _, err := l.Progress.Write(out); err != nil {
		return fmt.Errorf("write progress: %w", err)
	}
	return nil
}
