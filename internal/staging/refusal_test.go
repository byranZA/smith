package staging

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/hint"
)

func TestRefusalsNameTheCommandThatRestagesTheBox(t *testing.T) {
	t.Parallel()
	relayed := hint.Invocation{Box: "smith-dev", Verb: "session start", Args: []string{"--repo=smith", "--branch=try"}}
	typed := hint.Invocation{Verb: "session start", Args: []string{"--repo=smith", "--branch=try"}}
	parse := errors.New("line 3: unknown key")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			"absent blueprint, relayed",
			&AbsentError{Path: "/etc/smith/blueprint.yaml", Hint: relayed},
			"no blueprint is staged on this box at /etc/smith/blueprint.yaml: run `smith machine setup smith-dev` to stage one",
		},
		{
			"absent blueprint, typed on the machine",
			&AbsentError{Path: "/etc/smith/blueprint.yaml", Hint: typed},
			"no blueprint is staged on this box at /etc/smith/blueprint.yaml: run `smith machine setup` from the operator's machine to stage one\n" +
				"if this is the operator's machine, name the box the command is for: `smith session start <box> --repo=smith --branch=try`",
		},
		{
			"absent resolution, relayed to a literal target",
			&AbsentError{Path: "/etc/smith/resolved.json", Hint: hint.Invocation{Box: "smith@10.0.0.4", Verb: "workspace converge"}},
			"no resolution is staged on this box at /etc/smith/resolved.json: run `smith machine setup smith@10.0.0.4` to stage one",
		},
		{
			"malformed blueprint, relayed",
			&MalformedError{Path: "/etc/smith/blueprint.yaml", Err: parse, Hint: relayed},
			"the staged blueprint at /etc/smith/blueprint.yaml is malformed: line 3: unknown key\nrun `smith machine setup smith-dev` to re-stage it",
		},
		{
			"malformed blueprint, typed on the box",
			&MalformedError{Path: "/etc/smith/blueprint.yaml", Err: parse, Hint: typed},
			"the staged blueprint at /etc/smith/blueprint.yaml is malformed: line 3: unknown key\nrun `smith machine setup` from the operator's machine to re-stage it",
		},
		{
			"missing placement, relayed",
			&MissingPlacementError{Repo: "api", Destination: ".env", Path: "/etc/smith/placements/api/.env", Hint: relayed},
			"the placement to in repo api .env has no staged bytes at /etc/smith/placements/api/.env: run `smith machine setup smith-dev` to stage them",
		},
		{
			"missing placement, typed on the box",
			&MissingPlacementError{Destination: "~/.npmrc", Path: "/etc/smith/placements/box/.npmrc", Hint: typed},
			"the placement to ~/.npmrc has no staged bytes at /etc/smith/placements/box/.npmrc: run `smith machine setup` from the operator's machine to stage them",
		},
		{
			"missing value, relayed",
			&MissingValueError{Name: "GH_TOKEN", Path: "/etc/smith/env.json", Hint: relayed},
			"the variable GH_TOKEN has no staged value in /etc/smith/env.json: run `smith machine setup smith-dev` to stage one",
		},
		{
			"missing value, typed on the box",
			&MissingValueError{Repo: "api", Name: "GH_TOKEN", Path: "/etc/smith/env.json", Hint: typed},
			"the variable in repo api GH_TOKEN has no staged value in /etc/smith/env.json: run `smith machine setup` from the operator's machine to stage one",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("%T.Error() = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestLoadNamesTheBoxItWasRelayedToInItsRefusal(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, err := Load(root, hint.Invocation{Box: "smith-dev", Verb: "session list"})
	want := "no blueprint is staged on this box at " + DocumentPathIn(root) + ": run `smith machine setup smith-dev` to stage one"
	if err == nil || err.Error() != want {
		t.Errorf("Load(%q) err = %v, want %q", root, err, want)
	}
}
