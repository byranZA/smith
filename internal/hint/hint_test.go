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

func TestRerun(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		inv  Invocation
		want string
	}{
		{"typed on the box gains the box", Invocation{Verb: "session start", Args: []string{"--repo=smith", "--branch=x"}}, "smith session start <box> --repo=smith --branch=x"},
		{"relayed swaps the box it named", Invocation{Box: "dev", Verb: "session list"}, "smith session list <box>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.inv.Rerun("<box>"); got != tt.want {
				t.Errorf("%+v.Rerun(%q) = %q, want %q", tt.inv, "<box>", got, tt.want)
			}
		})
	}
}
