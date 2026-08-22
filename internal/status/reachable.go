package status

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/marker"
)

// Conn is the narrow slice of a connection the read-only box probes need: run
// a remote command and collect what it printed. Nothing here ships a file or
// mutates a box, so nothing here asks for more.
type Conn interface {
	Run(ctx context.Context, remoteCmd string, stdout, stderr io.Writer) error
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

// ProbeAll reports, for each named box, whether smith could reach it. The
// probes run concurrently — one per box, all in flight at once — because the
// fan-out's cost is entirely waiting on boxes that may never answer, and an
// operator listing a handful of them should wait for the slowest, not the sum.
//
// It never writes: reachability is a display concern, so what comes back is a
// value the caller renders and drops. An unreachable box is a false in the
// result, not an error and not a reason to prune an entry — a failed connect
// means rebooting, off-tailnet or firewalled at least as often as it means the
// box is gone. Only a probe that could not run at all — no ssh binary, say —
// is an error, and then no results come back, because a listing that silently
// marked every box unreachable would be a lie about the boxes.
func ProbeAll(ctx context.Context, conns map[string]Conn) (map[string]bool, error) {
	type result struct {
		name      string
		reachable bool
		err       error
	}
	results := make(chan result, len(conns))
	var wg sync.WaitGroup
	for name, conn := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reachable, err := connection.Reachable(ctx, conn)
			results <- result{name: name, reachable: reachable, err: err}
		}()
	}
	wg.Wait()
	close(results)

	reach := make(map[string]bool, len(conns))
	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, fmt.Errorf("probe box %s: %w", r.name, r.err))
			continue
		}
		reach[r.name] = r.reachable
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return reach, nil
}
