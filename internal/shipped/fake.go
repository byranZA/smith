package shipped

import (
	"context"
	"fmt"
	"io"
)

// Call is one subcommand a Fake was asked to run: its arguments, what it was
// fed on stdin, the connection it went over, and whether it was run as the
// final subcommand.
type Call struct {
	Sub   string
	Args  []string
	Input string
	Over  Conn
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
// is asked to run and the connection it ran over, answers each with its
// scripted Reply, and notes whether and over which connection it was closed, so
// a caller's tests assert on subcommands and outcomes rather than on how a
// script reaches the box. Conn is the connection it starts over; Move replaces
// it.
type Fake struct {
	Conn       Conn
	Replies    map[string]Reply
	Calls      []Call
	Closed     bool
	ClosedOver Conn
}

// Run records sub and args and answers with the Reply scripted for sub, or
// with empty output and no error when none is.
func (f *Fake) Run(ctx context.Context, stdout, stderr io.Writer, sub string, args ...string) error {
	return f.RunWithInput(ctx, nil, stdout, stderr, sub, args...)
}

// RunWithInput is Run that also records what stdin fed the subcommand.
func (f *Fake) RunWithInput(_ context.Context, stdin io.Reader, stdout, stderr io.Writer, sub string, args ...string) error {
	var input []byte
	if stdin != nil {
		var err error
		if input, err = io.ReadAll(stdin); err != nil {
			return fmt.Errorf("read fake stdin: %w", err)
		}
	}
	f.Calls = append(f.Calls, Call{Sub: sub, Args: args, Input: string(input), Over: f.Conn})
	r := f.Replies[sub]
	if _, err := io.WriteString(stdout, r.Stdout); err != nil {
		return fmt.Errorf("write fake stdout: %w", err)
	}
	if _, err := io.WriteString(stderr, r.Stderr); err != nil {
		return fmt.Errorf("write fake stderr: %w", err)
	}
	return r.Err
}

// Move records conn as the connection every later call and Close goes over.
func (f *Fake) Move(conn Conn) {
	f.Conn = conn
}

// Close records that the handle was closed and the connection it was closed
// over.
func (f *Fake) Close(context.Context) {
	f.Closed = true
	f.ClosedOver = f.Conn
}
