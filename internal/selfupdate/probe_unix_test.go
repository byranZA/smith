//go:build unix

package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeSmith writes an executable at a fresh path whose content is body.
func fakeSmith(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smith")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestProbeReportsABinaryThatAcceptsTheContinuation(t *testing.T) {
	got, err := Probe(t.Context(), fakeSmith(t, "#!/bin/sh\nexit 0\n"))

	if err != nil || !got {
		t.Errorf("Probe() = %v, %v; want true, nil", got, err)
	}
}

func TestProbeReportsABinaryThatRejectsTheContinuationAsOld(t *testing.T) {
	got, err := Probe(t.Context(), fakeSmith(t, "#!/bin/sh\necho 'Error: unknown command \"update\" for \"smith\"' >&2\nexit 1\n"))

	if err != nil || got {
		t.Errorf("Probe() = %v, %v; want false, nil", got, err)
	}
}

func TestProbeFailsWhenItCannotTellWhetherTheBinaryIsOld(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		body string
	}{
		{"the binary cannot be executed", context.Background(), "\x7fELF not really a binary"},
		{"the binary is killed by a signal", context.Background(), "#!/bin/sh\nkill -9 $$\n"},
		{"the probe is cancelled", cancelled, "#!/bin/sh\nexit 0\n"},
		{"the binary panics", context.Background(), "#!/bin/sh\nexit 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Probe(tt.ctx, fakeSmith(t, tt.body))

			if err == nil {
				t.Errorf("Probe() error = nil, want an error")
			}
		})
	}
}
