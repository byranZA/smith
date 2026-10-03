package shipped

import (
	"context"
	"fmt"
	"io"
)

// Call is one subcommand a Fake was asked to run, with its arguments and
// whether it was run as the final subcommand.
type Call struct {
	Sub   string
	Args  []string
	Final bool
}

// Reply scripts what a Fake does for one subcommand: the output it streams and
// the error it returns, such as a run failure, connection.ErrConnect or
// ErrNotShipped.
type Reply struct {
	Stdout string
	Stderr string
	Err    error
}

// Fake is a recording test double for a Script. It records every subcommand it
// is asked to run, answers each with its scripted Reply, and notes whether it
// was closed, so a caller's tests assert on subcommands and outcomes rather
// than on how a script reaches the box.
type Fake struct {
	Replies map[string]Reply
	Calls   []Call
	Closed  bool
}

// Run records sub and args and answers with the Reply scripted for sub, or
// with empty output and no error when none is.
func (f *Fake) Run(_ context.Context, stdout, stderr io.Writer, sub string, args ...string) error {
	f.Calls = append(f.Calls, Call{Sub: sub, Args: args})
	r := f.Replies[sub]
	if _, err := io.WriteString(stdout, r.Stdout); err != nil {
		return fmt.Errorf("write fake stdout: %w", err)
	}
	if _, err := io.WriteString(stderr, r.Stderr); err != nil {
		return fmt.Errorf("write fake stderr: %w", err)
	}
	return r.Err
}

// Close records that the handle was closed.
func (f *Fake) Close(context.Context) {
	f.Closed = true
}
