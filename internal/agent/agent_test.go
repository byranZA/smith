package agent

import (
	"errors"
	"slices"
	"testing"
)

func TestClaudeRunsHeadlessWithNoApprovalPrompts(t *testing.T) {
	adapter, err := Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(claude) error = %v", err)
	}

	got := adapter.Unattended("do #43")

	want := Command{
		Name: "claude",
		Args: []string{"--permission-mode", "auto", "--print", "do #43"},
		Env:  []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1"},
	}
	if got.Name != want.Name || !slices.Equal(got.Args, want.Args) || !slices.Equal(got.Env, want.Env) {
		t.Errorf("Unattended() = %+v, want %+v", got, want)
	}
}

func TestLookupRefusesAnUnknownAgentByName(t *testing.T) {
	_, err := Lookup("gemini")

	var unknown *UnknownError
	if !errors.As(err, &unknown) || err.Error() != `unknown agent "gemini": known agents are claude` {
		t.Errorf("Lookup(gemini) error = %v, want an UnknownError naming gemini and the known agents", err)
	}
}
