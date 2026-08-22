package cli

import (
	"strings"
	"testing"
)

// TestEverySessionVerbTravelsTheSameWay pins the envelope every session verb
// shares: a named box sends the verb to the smith on that box, spelled as the
// operator typed it, and a verb that hands over the terminal travels by
// replacing smith with ssh rather than by streaming.
func TestEverySessionVerbTravelsTheSameWay(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		connect bool
		want    string
	}{
		{name: "list", args: []string{"list", "dev", "--names"}, want: "'session' 'list' '--names=true'"},
		{name: "stop", args: []string{"stop", "dev", "smith-main"}, want: "'session' 'stop' 'smith-main'"},
		{name: "rm", args: []string{"rm", "dev", "smith-main", "--force"}, want: "'session' 'rm' 'smith-main' '--force=true'"},
		{name: "start detached", args: []string{"start", "dev", "--repo=smith", "--branch=spec-42", "--detach"}, want: "'session' 'start' '--branch=spec-42' '--detach=true' '--repo=smith'"},
		{name: "start connecting", args: []string{"start", "dev", "--repo=smith", "--branch=spec-42"}, connect: true, want: "'session' 'start' '--branch=spec-42' '--repo=smith'"},
		{name: "attach", args: []string{"attach", "dev", "smith-main", "--interact"}, connect: true, want: "'session' 'attach' 'smith-main' '--interact=true'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeInventory(t, dir, `{"schema_version":1,"boxes":{"dev":{"target":"smith@100.92.14.7"}}}`)
			ssh, execer := &fakeSSHRelay{}, &fakeExec{}

			_, stderr, code := runSessionOn(t, laptop(t, dir, ssh, execer, &fakeRunner{}, &fakeRunner{}), tt.args...)

			if code != 0 {
				t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
			}
			line := ssh.calls
			got := ""
			switch {
			case tt.connect:
				got = execer.line(t)
				if len(ssh.calls) != 0 {
					t.Errorf("ssh streamed %v, want a connecting verb to exec instead", line)
				}
				if !strings.Contains(got, " -t ") {
					t.Errorf("exec argv = %q, want a terminal requested", got)
				}
			default:
				got = ssh.line(t)
				if len(execer.calls) != 0 {
					t.Errorf("exec called %v, want a reporting verb to stream instead", execer.calls)
				}
			}
			if !strings.Contains(got, "smith@100.92.14.7") {
				t.Errorf("argv = %q, want it to name the resolved target", got)
			}
			if !strings.Contains(got, tt.want) {
				t.Errorf("argv = %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

// TestNoSessionVerbDialsWithNoBoxNamed pins the other half of the envelope:
// with no box on the command line every verb runs against this machine, and
// none of them opens a connection to do it.
func TestNoSessionVerbDialsWithNoBoxNamed(t *testing.T) {
	for _, args := range [][]string{
		{"list"},
		{"stop", "smith-main"},
		{"rm", "smith-main"},
		{"start", "--repo=smith", "--branch=spec-42", "--detach"},
		{"attach", "smith-main"},
	} {
		t.Run(args[0], func(t *testing.T) {
			workspace := t.TempDir()
			writeBareRepo(t, workspace, "smith")
			w := onBoxWiring(t, resolvedBox(workspace, "smith"), &fakeTmux{}, &fakeExec{})

			runSessionOn(t, w, args...)
		})
	}
}
