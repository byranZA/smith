package staging

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/byranZA/smith/internal/marker"
)

// PointerError reports a run that supplied no blueprint against a box whose
// marker records one. It names the blueprint the box was built from, because
// that is the thing the operator has to pass to re-run.
type PointerError struct {
	// Blueprint is the blueprint pointer the box's marker records.
	Blueprint string
}

// Error implements error.
func (e *PointerError) Error() string {
	return fmt.Sprintf(
		"this box was built from the blueprint %q: re-run with --blueprint %s, "+
			"or the staged config would come to describe a box that no longer matches it",
		e.Blueprint, e.Blueprint)
}

// CheckPointer applies the blueprint-pointer rule, before the run has touched
// anything: a run that names no blueprint against a box whose marker records
// one is refused, naming the blueprint the box was built from.
//
// This is the only rule under which the pointer cannot come to describe a box
// that no longer matches it. The alternatives are worse in both directions: a
// blueprint-less run that converged the staged tree would strip the box of its
// workspace, and one that left the tree alone would leave the pointer pointing
// at a blueprint the box is no longer being maintained against.
//
// A run that names a blueprint is not this rule's business — the operator has
// said what the box is to become — so the box is not even asked. A box whose
// marker records no blueprint, or that carries no marker at all, has no
// pointer: the run proceeds and stages nothing, which is the flag-only path.
//
// It reads and never writes, so a refusal leaves the staged document and the
// staged placements exactly as they were.
func CheckPointer(ctx context.Context, conn Conn, blueprintName string) error {
	if blueprintName != "" {
		return nil
	}
	recorded, err := pointer(ctx, conn)
	if err != nil {
		return err
	}
	if recorded == "" {
		return nil
	}
	return &PointerError{Blueprint: recorded}
}

// pointer reads the blueprint pointer the box's marker records, empty when the
// box carries no marker or its marker records none. A missing marker is not a
// failure — a box provisioned before the pointer existed simply has none — so
// the read swallows the absent file rather than the whole run failing on it.
func pointer(ctx context.Context, conn Conn) (string, error) {
	var out bytes.Buffer
	cmd := fmt.Sprintf("cat %s 2>/dev/null || true", marker.Path)
	if err := conn.Run(ctx, cmd, &out, io.Discard); err != nil {
		return "", fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	raw := strings.TrimSpace(out.String())
	if raw == "" {
		return "", nil
	}
	// A marker written by a newer smith still decodes to the raw facts this
	// build recognizes, and the pointer is one of them, so the skew is not
	// this rule's business.
	m, _, err := marker.Decode([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("read the box's marker at %s: %w", marker.Path, err)
	}
	return m.Blueprint, nil
}
