package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/relay"
)

func TestAcceptRelayedFrom(t *testing.T) {
	tests := []struct {
		name        string
		local       string
		relayedFrom string
		wantErr     bool
	}{
		{"a matching version is accepted", "0.2.0", "0.2.0", false},
		{"a caller newer than the box is refused", "0.1.0", "0.2.0", true},
		{"a caller older than the box is refused", "0.3.0", "0.2.0", true},
		{"a patch-level difference is refused", "0.2.0", "0.2.1", true},
		{"a pre-release suffix is a difference", "0.2.0", "0.2.0-rc1", true},
		{"a leading v is a difference", "0.2.0", "v0.2.0", true},
		{"a dev build matches another dev build", "dev", "dev", false},
		{"a dev build against a release is refused", "dev", "0.2.0", true},
		{"a release against a dev build is refused", "0.2.0", "dev", true},
		{"no declared version is not checked at all", "0.2.0", "", false},
		{"no declared version is not checked on a dev build", "dev", "", false},
		{"whitespace is not a version", "0.2.0", " 0.2.0", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := acceptRelayedFrom(tt.local, tt.relayedFrom)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("acceptRelayedFrom(%q, %q) = %v, want nil", tt.local, tt.relayedFrom, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("acceptRelayedFrom(%q, %q) = nil, want a refusal", tt.local, tt.relayedFrom)
			}
			for _, want := range []string{tt.local, tt.relayedFrom} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal = %q, want it to name %q", err.Error(), want)
				}
			}
		})
	}
}

// TestRefusalDoesNotBorrowSchemaSkewVocabulary guards the one confusion this
// check invites: the marker's schema_version against a build's constant is a
// different axis, and an operator reading either message must not be told to
// look at the other.
func TestRefusalDoesNotBorrowSchemaSkewVocabulary(t *testing.T) {
	err := acceptRelayedFrom("0.1.0", "0.2.0")
	if err == nil {
		t.Fatal("acceptRelayedFrom() = nil, want a refusal")
	}
	for _, forbidden := range []string{"schema", "skew", "marker", "migrate"} {
		if strings.Contains(strings.ToLower(err.Error()), forbidden) {
			t.Errorf("refusal = %q, want it free of the schema-skew word %q", err.Error(), forbidden)
		}
	}
}

func TestRootRefusesAMismatchedRelay(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--relayed-from", "9.9.9", "version"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	err := root.Execute()
	if err == nil {
		t.Fatal("Execute() err = nil, want a refusal of the mismatched relay")
	}
	if code := codeFromError(err); code == 0 {
		t.Errorf("exit code = 0, want non-zero for a mismatched relay")
	}
	if strings.Contains(out.String(), "smith ") {
		t.Errorf("stdout = %q, want the refused command not to have run", out.String())
	}
	for _, want := range []string{"9.9.9", resolveVersion()} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal = %q, want it to name %q", err.Error(), want)
		}
	}
}

func TestRootRunsAMatchingRelay(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--relayed-from", resolveVersion(), "version"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() err = %v (stderr: %s)", err, errBuf.String())
	}
	if !strings.Contains(out.String(), "smith ") {
		t.Errorf("stdout = %q, want the version report", out.String())
	}
}

// TestRelayedFromIsHiddenFromHelp keeps the flag a wire detail between two
// smiths: an operator reading help never learns to type it.
func TestRelayedFromIsHiddenFromHelp(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"--help"})
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() err = %v", err)
	}
	if strings.Contains(out.String(), "relayed-from") {
		t.Errorf("help = %q, want no mention of the relaying flag", out.String())
	}
}

// TestRefusalExitsTheCodeTheRelayReads checks the wire contract from the box's
// side: a refused relay exits the status the relaying smith classifies as a
// version mismatch, so the other side never has to read the prose to know what
// happened.
func TestRefusalExitsTheCodeTheRelayReads(t *testing.T) {
	err := acceptRelayedFrom("0.1.0", "0.2.0")

	if got := codeFromError(err); got != relay.RefusalExitCode {
		t.Errorf("codeFromError() = %d, want the relay's refusal code %d", got, relay.RefusalExitCode)
	}
	if got := relay.Refusal("0.1.0", "0.2.0"); !strings.Contains(err.Error(), got) {
		t.Errorf("refusal = %q, want the relay's own wording %q", err, got)
	}
}

// TestRefusalCarriesTheVersionTheRelayReads checks the wire contract end to
// end from the box's side: what on-box smith prints when it refuses is
// something the relaying smith recovers this binary's version from, without
// either side reading the other's prose.
func TestRefusalCarriesTheVersionTheRelayReads(t *testing.T) {
	refusal := acceptRelayedFrom("0.1.0", "0.2.0")
	if refusal == nil {
		t.Fatal("acceptRelayedFrom() = nil, want a refusal")
	}
	ssh := &skewRefusingSSH{stderr: "smith: " + refusal.Error() + "\n"}

	err := relay.Run(context.Background(), ssh, relay.Verb{
		Target:  "smith@box",
		Version: "0.2.0",
		Args:    []string{"session", "list"},
	}, func() error { return nil }, io.Discard, io.Discard)

	var mismatch *relay.MismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("relay.Run() err = %v, want a MismatchError", err)
	}
	if mismatch.Box != "0.1.0" {
		t.Errorf("MismatchError.Box = %q, want the version the box refused with", mismatch.Box)
	}
}

// skewRefusingSSH is the box answering a relayed verb with the refusal on-box
// smith renders and the exit code it exits on.
type skewRefusingSSH struct {
	stderr string
}

func (f *skewRefusingSSH) Run(_ context.Context, _ string, _ []string, _ io.Reader, _, stderr io.Writer) error {
	if _, err := io.WriteString(stderr, f.stderr); err != nil {
		return fmt.Errorf("write canned refusal: %w", err)
	}
	return relayExit(relay.RefusalExitCode)
}
