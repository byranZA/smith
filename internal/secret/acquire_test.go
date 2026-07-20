package secret

import (
	"errors"
	"testing"
)

// fakeTerminal is a test double for the interactive boundary.
type fakeTerminal struct {
	interactive bool
	secret      string
	err         error
	prompted    bool
	gotPrompt   string
}

func (f *fakeTerminal) Interactive() bool { return f.interactive }

func (f *fakeTerminal) ReadSecret(prompt string) (string, error) {
	f.prompted = true
	f.gotPrompt = prompt
	return f.secret, f.err
}

func TestAcquire(t *testing.T) {
	t.Run("a given reference is resolved and the terminal is untouched", func(t *testing.T) {
		t.Setenv("TS_KEY", "tskey-from-env")
		term := &fakeTerminal{interactive: true}
		got, err := Acquire("env:TS_KEY", "auth key: ", term)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if got != "tskey-from-env" {
			t.Errorf("Acquire = %q, want %q", got, "tskey-from-env")
		}
		if term.prompted {
			t.Error("Acquire prompted the terminal despite a reference being given")
		}
	})

	t.Run("an omitted reference prompts an interactive terminal and trims", func(t *testing.T) {
		term := &fakeTerminal{interactive: true, secret: "  tskey-typed \n"}
		got, err := Acquire("", "auth key: ", term)
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		if !term.prompted {
			t.Error("Acquire did not prompt the interactive terminal")
		}
		if term.gotPrompt != "auth key: " {
			t.Errorf("prompt = %q, want %q", term.gotPrompt, "auth key: ")
		}
		if got != "tskey-typed" {
			t.Errorf("Acquire = %q, want %q", got, "tskey-typed")
		}
	})

	t.Run("an omitted reference on a non-interactive terminal fails fast", func(t *testing.T) {
		term := &fakeTerminal{interactive: false}
		_, err := Acquire("", "auth key: ", term)
		if !errors.Is(err, ErrNoReference) {
			t.Fatalf("Acquire error = %v, want %v", err, ErrNoReference)
		}
		if term.prompted {
			t.Error("Acquire prompted a non-interactive terminal")
		}
	})

	t.Run("a bad reference surfaces the resolve error", func(t *testing.T) {
		term := &fakeTerminal{interactive: true}
		_, err := Acquire("tskey-bare", "auth key: ", term)
		if !errors.Is(err, ErrBareLiteral) {
			t.Fatalf("Acquire error = %v, want %v", err, ErrBareLiteral)
		}
	})
}
