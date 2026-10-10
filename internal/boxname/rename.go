// Package boxname renames a registered box on both sides that record its name:
// the name field of the box's marker and its inventory entry.
//
// A rename runs no setup phase or stage and never reaches the provider, so a
// provider-side marker keeps the box's old name.
package boxname

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
)

// Conn reaches the box being renamed as the smith user.
type Conn interface {
	// Run executes cmd on the box, with an error wrapping connection.ErrConnect
	// when the box could not be reached.
	Run(ctx context.Context, cmd string, stdout, stderr io.Writer) error
	// RunWithInput executes cmd on the box with stdin delivered from the reader.
	RunWithInput(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error
}

// Change is one rename: the box registered as From at Target, renamed To.
type Change struct {
	// From is the name the box is registered under now.
	From string
	// To is the name it is renamed to.
	To string
	// Target is the registered address the box is reached over.
	Target string
}

// Register moves the box's inventory entry from c.From to c.To.
type Register func(c Change) error

// ErrNoMarker reports a box carrying no marker, so no name on it to rewrite.
var ErrNoMarker = fmt.Errorf("the box carries no marker at %s, so smith has never set it up: "+
	"provision it first with smith machine setup", marker.Path)

// UnreachableError reports a box that could not be reached, before anything
// was written. It reads as the connect failure it wraps.
type UnreachableError struct {
	// Err is the connect failure, wrapping connection.ErrConnect.
	Err error
}

// Error implements error.
func (e *UnreachableError) Error() string { return e.Err.Error() }

// Unwrap returns the connect failure.
func (e *UnreachableError) Unwrap() error { return e.Err }

// PartialError reports a rename whose marker was rewritten but whose inventory
// entry did not move, so the two disagree about the box's name.
type PartialError struct {
	Change
	// Err is why the inventory entry did not move.
	Err error
}

// Error implements error.
func (e *PartialError) Error() string {
	return fmt.Sprintf("the box's marker records the name %q, but the inventory still registers it as %q: %v",
		e.To, e.From, e.Err)
}

// Unwrap returns why the inventory entry did not move.
func (e *PartialError) Unwrap() error { return e.Err }

// Rename rewrites the name the box's marker records to c.To, then has register
// move its inventory entry. Nothing is written to a box that is unreachable
// (*UnreachableError) or carries no marker (ErrNoMarker), and a failed
// registration after the marker write is a *PartialError.
func Rename(ctx context.Context, conn Conn, c Change, register Register) error {
	if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return &UnreachableError{Err: err}
		}
		return fmt.Errorf("probe box: %w", err)
	}
	if err := renameMarker(ctx, conn, c.To); err != nil {
		return err
	}
	if err := register(c); err != nil {
		return &PartialError{Change: c, Err: err}
	}
	return nil
}

// renameMarker rewrites the name the box's marker records to name, leaving
// every other byte of the marker as it was.
func renameMarker(ctx context.Context, conn Conn, name string) error {
	var raw bytes.Buffer
	if err := conn.Run(ctx, fmt.Sprintf("cat %s 2>/dev/null || true", marker.Path), &raw, io.Discard); err != nil {
		return fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	if len(bytes.TrimSpace(raw.Bytes())) == 0 {
		return ErrNoMarker
	}
	renamed, err := marker.Rename(raw.Bytes(), name)
	if err != nil {
		return fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	if err := conn.RunWithInput(ctx, writeCommand(), bytes.NewReader(renamed), io.Discard, io.Discard); err != nil {
		return fmt.Errorf("write the box's marker at %s: %w", marker.Path, err)
	}
	return nil
}

// writeCommand builds the remote write of the marker from stdin: into a
// temporary file beside it, given the root ownership and mode bootstrap.sh
// leaves it with, then moved into place through the smith user's sudo.
func writeCommand() string {
	tmp := connection.ShellArg(marker.Path + ".rename")
	dest := connection.ShellArg(marker.Path)
	return strings.Join([]string{
		fmt.Sprintf("sudo rm -f %s", tmp),
		fmt.Sprintf("sudo tee %s >/dev/null", tmp),
		fmt.Sprintf("sudo chmod 0644 %s", tmp),
		fmt.Sprintf("sudo chown root:root %s", tmp),
		fmt.Sprintf("sudo mv -f %s %s", tmp, dest),
	}, " && ")
}
