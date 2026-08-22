package status

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

// Conn is the narrow slice of a connection the read-only box probes need: run
// a remote command and collect what it printed. Nothing here ships a file or
// mutates a box, so nothing here asks for more.
type Conn interface {
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
}

// Reachable reports whether smith can open a connection to the box: it runs
// the cheapest command there is and looks only at whether it got in.
//
// A refused connection is (false, nil), not an error — an unreachable box is a
// fact about the box, and a caller deciding what to do about it should not
// have to unwrap an error to learn it. Anything else — a missing ssh binary, a
// command that ran and failed — is a Go error, because it says nothing about
// whether the box answers.
func Reachable(ctx context.Context, conn Conn) (bool, error) {
	if err := conn.Run(ctx, "true", io.Discard, io.Discard); err != nil {
		if errors.Is(err, connection.ErrConnect) {
			return false, nil
		}
		return false, fmt.Errorf("reachability probe: %w", err)
	}
	return true, nil
}

// ReadMarker reads the box's marker and reports whether it carried one. It
// reads and never writes: a box smith has never provisioned carries no marker,
// and saying otherwise would assert provisioned state that does not exist.
//
// An absent marker is (zero marker, false, nil) rather than an error, because
// "this box has never been set up" is an answer, not a failure. A marker that
// is there but will not decode is an error naming the file, so the operator is
// told which one to look at. The schema skew is not this read's business: a
// marker a newer smith wrote still decodes to the facts this build recognises.
func ReadMarker(ctx context.Context, conn Conn) (marker.Marker, bool, error) {
	var out bytes.Buffer
	cmd := fmt.Sprintf("cat %s 2>/dev/null || true", marker.Path)
	if err := conn.Run(ctx, cmd, &out, io.Discard); err != nil {
		return marker.Marker{}, false, fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return marker.Marker{}, false, nil
	}
	m, _, err := marker.Decode([]byte(raw))
	if err != nil {
		return marker.Marker{}, false, fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	return m, true, nil
}
