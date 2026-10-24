package workbench_test

import (
	"errors"
	"testing"

	"github.com/byranZA/smith/internal/workbench"
)

// env is an environment lookup holding vars, and nothing else.
func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestStateHomeIsUnderAnAbsoluteXDGStateHomeElseUnderHome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{"absolute XDG_STATE_HOME", map[string]string{"XDG_STATE_HOME": "/xdg/state", "HOME": "/home/op"}, "/xdg/state/smith"},
		{"unset XDG_STATE_HOME", map[string]string{"HOME": "/home/op"}, "/home/op/.local/state/smith"},
		{"empty XDG_STATE_HOME", map[string]string{"XDG_STATE_HOME": "", "HOME": "/home/op"}, "/home/op/.local/state/smith"},
		{"relative XDG_STATE_HOME", map[string]string{"XDG_STATE_HOME": "xdg/state", "HOME": "/home/op"}, "/home/op/.local/state/smith"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := workbench.StateHome(env(tt.vars))
			if err != nil || got != tt.want {
				t.Errorf("StateHome(%v) = %q, %v, want %q, nil", tt.vars, got, err, tt.want)
			}
		})
	}
}

func TestStateHomeWithNoXDGStateHomeAndNoHomeIsAnError(t *testing.T) {
	t.Parallel()

	got, err := workbench.StateHome(env(map[string]string{"XDG_STATE_HOME": "rel"}))

	if !errors.Is(err, workbench.ErrNoStateHome) || got != "" {
		t.Errorf("StateHome() = %q, %v, want \"\", %v", got, err, workbench.ErrNoStateHome)
	}
}
