package hint

import "testing"

func TestCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  Invocation
		verb string
		args []string
		want string
	}{
		{"typed on the box names no box", Invocation{}, "session start", []string{"--repo", "smith"}, "smith session start --repo smith"},
		{"relayed names the box after the verb", Invocation{Box: "smith-dev"}, "session start", []string{"--repo", "smith"}, "smith session start smith-dev --repo smith"},
		{"relayed to a literal target names it as written", Invocation{Box: "smith@10.0.0.4"}, "session rm", []string{"smith-x", "--force"}, "smith session rm smith@10.0.0.4 smith-x --force"},
		{"a verb with no arguments", Invocation{Box: "dev"}, "session list", nil, "smith session list dev"},
		{"a branch the shell would expand", Invocation{}, "session start", []string{"--branch", "feature/$HOME"}, "smith session start --branch 'feature/$HOME'"},
		{"a branch the shell would split", Invocation{}, "session start", []string{"--branch", "feature/a;b"}, "smith session start --branch 'feature/a;b'"},
		{"an apostrophe", Invocation{}, "session start", []string{"--branch", "it's"}, `smith session start --branch 'it'\''s'`},
		{"an empty value", Invocation{}, "session start", []string{"--base", ""}, "smith session start --base ''"},
		{"a box name with a space", Invocation{Box: "my box"}, "session list", nil, "smith session list 'my box'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inv.Command(tt.verb, tt.args...); got != tt.want {
				t.Errorf("%+v.Command(%q, %q) = %q, want %q", tt.inv, tt.verb, tt.args, got, tt.want)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  Invocation
		want string
	}{
		{"typed on the box", Invocation{}, "smith session start --repo <name> --branch <name>"},
		{"relayed", Invocation{Box: "smith-dev"}, "smith session start smith-dev --repo <name> --branch <name>"},
		{"relayed to a box name with a space", Invocation{Box: "my box"}, "smith session start 'my box' --repo <name> --branch <name>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inv.Usage("session start", "--repo <name> --branch <name>"); got != tt.want {
				t.Errorf("%+v.Usage(%q, %q) = %q, want %q", tt.inv, "session start", "--repo <name> --branch <name>", got, tt.want)
			}
		})
	}
}

func TestRerun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  Invocation
		want string
	}{
		{"typed on the box gains the box", Invocation{Verb: "session start", Args: []string{"--repo=smith", "--branch=x"}}, "smith session start <box> --repo=smith --branch=x"},
		{"relayed swaps the box it named", Invocation{Box: "dev", Verb: "session list"}, "smith session list <box>"},
		{"a flag value the shell would split", Invocation{Verb: "session start", Args: []string{"--branch=a b"}}, "smith session start <box> '--branch=a b'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inv.Rerun(); got != tt.want {
				t.Errorf("%+v.Rerun() = %q, want %q", tt.inv, got, tt.want)
			}
		})
	}
}
