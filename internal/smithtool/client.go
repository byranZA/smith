package smithtool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
)

// dial connects to the socket at path, or refuses: there is no path, or
// nothing answers on it.
func dial(ctx context.Context, path string) (net.Conn, error) {
	if path == "" {
		return nil, &RefusedError{Reason: errNoSocket.Error()}
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, &RefusedError{Reason: fmt.Sprintf("smith is not running a loop for this shell: nothing answers on SMITH_TOOLS (%s): %v", path, err)}
	}
	return conn, nil
}

// exchange sends req on the socket at path and reads frames until the final
// one, copying output to out when it is set, and returns the final frame.
func exchange(ctx context.Context, path string, req request, out io.Writer) (_ frame, err error) {
	conn, err := dial(ctx, path)
	if err != nil {
		return frame{}, err
	}
	defer func() {
		if cerr := closeConn(conn); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()
	// A cancelled context closes conn to end the read; the failure that
	// follows says so, and closeConn above tolerates the second close.
	stop := context.AfterFunc(ctx, func() { conn.Close() }) //nolint:errcheck,gosec // see above
	defer stop()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return frame{}, &RefusedError{Reason: fmt.Sprintf("smith is not running a loop for this shell: send the call: %v", err)}
	}
	dec := json.NewDecoder(bufio.NewReader(conn))
	for {
		var f frame
		if err := dec.Decode(&f); err != nil {
			return frame{}, &FailedError{Reason: fmt.Sprintf("the loop ended the call before it finished: %v", err)}
		}
		if f.Done {
			return f, nil
		}
		if out != nil {
			if _, err := out.Write(f.Out); err != nil {
				return frame{}, fmt.Errorf("write tool output: %w", err)
			}
		}
	}
}

// closeConn closes conn, treating one already closed as done: a cancelled
// call closes it from the context hook first.
func closeConn(conn net.Conn) error {
	if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close the tool socket: %w", err)
	}
	return nil
}

// final turns a final frame into the error it stands for, nil for success.
func final(f frame) error {
	switch f.Status {
	case statusOK:
		return nil
	case statusRefused:
		return &RefusedError{Reason: f.Error}
	default:
		return &FailedError{Reason: f.Error}
	}
}

// Call asks the loop serving the socket at path to run the tool name with
// args, streaming its output to out. It returns a *RefusedError when the
// call was refused before anything changed, a *FailedError when the tool ran
// and failed, and nil when it succeeded.
func Call(ctx context.Context, path, name string, args []string, out io.Writer) error {
	f, err := exchange(ctx, path, request{Op: opCall, Name: name, Args: args}, out)
	if err != nil {
		return err
	}
	return final(f)
}

// List asks the loop serving the socket at path which tools it serves. It
// returns a *RefusedError when there is no loop to ask.
func List(ctx context.Context, path string) ([]Info, error) {
	f, err := exchange(ctx, path, request{Op: opList}, nil)
	if err != nil {
		return nil, err
	}
	if err := final(f); err != nil {
		return nil, err
	}
	infos := []Info{}
	if err := json.Unmarshal(f.Tools, &infos); err != nil {
		return nil, errors.Join(&FailedError{Reason: "read the tool listing"}, err)
	}
	return infos, nil
}
