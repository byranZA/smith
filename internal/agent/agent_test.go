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
	if !errors.As(err, &unknown) || err.Error() != `unknown agent "gemini": known agents are claude, codex, pi` {
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

func TestCodexAndPiAreGivenTheModelVerbatimAndTheirOwnEffort(t *testing.T) {
	tests := []struct {
		name        string
		agent       string
		interactive bool
		opts        Options
		want        Command
	}{
		{"codex unattended", "codex", false, Options{},
			Command{Name: "codex", Args: []string{"exec", "--yolo", "do #43"}}},
		{"codex unattended with a model", "codex", false, Options{Model: "gpt-5.5-codex"},
			Command{Name: "codex", Args: []string{"exec", "--yolo", "--model", "gpt-5.5-codex", "do #43"}}},
		{"codex unattended low effort", "codex", false, Options{Effort: Low},
			Command{Name: "codex", Args: []string{"exec", "--yolo", "--config", `model_reasoning_effort="low"`, "do #43"}}},
		{"codex unattended medium effort", "codex", false, Options{Effort: Medium},
			Command{Name: "codex", Args: []string{"exec", "--yolo", "--config", `model_reasoning_effort="medium"`, "do #43"}}},
		{"codex unattended model and high effort", "codex", false, Options{Model: "o3", Effort: High},
			Command{Name: "codex", Args: []string{"exec", "--yolo", "--model", "o3", "--config", `model_reasoning_effort="high"`, "do #43"}}},
		{"codex interactive", "codex", true, Options{},
			Command{Name: "codex", Args: []string{"do #43"}, Attached: true}},
		{"codex interactive model and high effort", "codex", true, Options{Model: "o3", Effort: High},
			Command{Name: "codex", Args: []string{"--model", "o3", "--config", `model_reasoning_effort="high"`, "do #43"}, Attached: true}},
		{"pi unattended", "pi", false, Options{},
			Command{Name: "pi", Args: []string{"--print", "do #43"}}},
		{"pi unattended with a model", "pi", false, Options{Model: "anthropic/some-new-model"},
			Command{Name: "pi", Args: []string{"--model", "anthropic/some-new-model", "--print", "do #43"}}},
		{"pi unattended low effort", "pi", false, Options{Effort: Low},
			Command{Name: "pi", Args: []string{"--thinking", "low", "--print", "do #43"}}},
		{"pi unattended medium effort", "pi", false, Options{Effort: Medium},
			Command{Name: "pi", Args: []string{"--thinking", "medium", "--print", "do #43"}}},
		{"pi unattended model and high effort", "pi", false, Options{Model: "anthropic/some-new-model", Effort: High},
			Command{Name: "pi", Args: []string{"--model", "anthropic/some-new-model", "--thinking", "high", "--print", "do #43"}}},
		{"pi interactive", "pi", true, Options{},
			Command{Name: "pi", Args: []string{"do #43"}, Attached: true}},
		{"pi interactive model and high effort", "pi", true, Options{Model: "anthropic/some-new-model", Effort: High},
			Command{Name: "pi", Args: []string{"--model", "anthropic/some-new-model", "--thinking", "high", "do #43"}, Attached: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := Lookup(tt.agent)
			if err != nil {
				t.Fatalf("Lookup(%s) error = %v", tt.agent, err)
			}

			got := adapter.Unattended("do #43", tt.opts)
			if tt.interactive {
				got = adapter.Interactive("do #43", tt.opts)
			}

			if got.Name != tt.want.Name || !slices.Equal(got.Args, tt.want.Args) ||
				!slices.Equal(got.Env, tt.want.Env) || got.Attached != tt.want.Attached {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
