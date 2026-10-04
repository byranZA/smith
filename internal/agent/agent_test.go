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

	got := adapter.Unattended("do #43", Options{})

	want := Command{
		Name: "claude",
		Args: []string{"--permission-mode", "auto", "--print", "do #43"},
		Env:  []string{"CLAUDE_CODE_DISABLE_BACKGROUND_TASKS=1"},
	}
	if got.Name != want.Name || !slices.Equal(got.Args, want.Args) || !slices.Equal(got.Env, want.Env) {
		t.Errorf("Unattended() = %+v, want %+v", got, want)
	}
}

func TestClaudeIsGivenTheModelVerbatimAndItsOwnEffort(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"model only", Options{Model: "claude-opus-5-5[1m]"}, []string{"--permission-mode", "auto", "--model", "claude-opus-5-5[1m]", "--print", "do #43"}},
		{"low effort", Options{Effort: Low}, []string{"--permission-mode", "auto", "--effort", "low", "--print", "do #43"}},
		{"medium effort", Options{Effort: Medium}, []string{"--permission-mode", "auto", "--effort", "medium", "--print", "do #43"}},
		{"model and high effort", Options{Model: "opus", Effort: High}, []string{"--permission-mode", "auto", "--model", "opus", "--effort", "high", "--print", "do #43"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := Lookup("claude")
			if err != nil {
				t.Fatalf("Lookup(claude) error = %v", err)
			}

			got := adapter.Unattended("do #43", tt.opts)

			if !slices.Equal(got.Args, tt.want) {
				t.Errorf("Unattended() args = %q, want %q", got.Args, tt.want)
			}
		})
	}
}

func TestLookupRefusesAnUnknownAgentByName(t *testing.T) {
	_, err := Lookup("gemini")

	var unknown *UnknownError
	if !errors.As(err, &unknown) || err.Error() != `unknown agent "gemini": known agents are claude` {
		t.Errorf("Lookup(gemini) error = %v, want an UnknownError naming gemini and the known agents", err)
	}
}

func TestParseEffortAcceptsTheScaleAndNothingElse(t *testing.T) {
	tests := []struct {
		in      string
		want    Effort
		wantErr string
	}{
		{"", "", ""},
		{"low", Low, ""},
		{"medium", Medium, ""},
		{"high", High, ""},
		{"extreme", "", `unknown effort "extreme": the scale is low, medium, high`},
		{"HIGH", "", `unknown effort "HIGH": the scale is low, medium, high`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseEffort(tt.in)

			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			if got != tt.want || gotErr != tt.wantErr {
				t.Errorf("ParseEffort(%q) = %q, %q; want %q, %q", tt.in, got, gotErr, tt.want, tt.wantErr)
			}
		})
	}
}

func TestClaudeRunsInteractiveAttachedToTheTerminal(t *testing.T) {
	adapter, err := Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(claude) error = %v", err)
	}

	got := adapter.Interactive("do #43", Options{Model: "opus", Effort: High})

	want := []string{"--model", "opus", "--effort", "high", "do #43"}
	if got.Name != "claude" || !slices.Equal(got.Args, want) || !got.Attached {
		t.Errorf("Interactive() = %+v, want claude attached with args %q", got, want)
	}
}

func TestClaudeRunsUnattendedDetachedFromTheTerminal(t *testing.T) {
	adapter, err := Lookup("claude")
	if err != nil {
		t.Fatalf("Lookup(claude) error = %v", err)
	}

	if got := adapter.Unattended("do #43", Options{}); got.Attached {
		t.Errorf("Unattended() = %+v, want it detached from the terminal", got)
	}
}
