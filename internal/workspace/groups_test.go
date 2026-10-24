package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// groupBox stands in for the box's account and group databases and its tmux
// server, recording the argv of every command it is asked to run.
type groupBox struct {
	calls [][]string
	// gids are the groups the box holds, each with its id.
	gids map[string]int
	// member are the groups the smith user's account is in.
	member map[string]bool
	// sessions are the sessions smith's tmux server is running, none when no
	// server is running.
	sessions []string
}

// tmuxPID is the process id the fake's tmux server runs as.
const tmuxPID = 4242

func (g *groupBox) Run(_ context.Context, name string, args []string, _ io.Reader, stdout, _ io.Writer) error {
	argv := append([]string{name}, args...)
	g.calls = append(g.calls, argv)
	switch line := strings.Join(argv, " "); {
	case strings.HasPrefix(line, "getent group "):
		gid, ok := g.gids[args[1]]
		if !ok {
			return exitStatus(2)
		}
		return write(stdout, fmt.Sprintf("%s:x:%d:\n", args[1], gid))
	case line == "id -un":
		return write(stdout, "smith\n")
	case line == "id -nG smith":
		names := []string{"smith"}
		for group := range g.member {
			names = append(names, group)
		}
		return write(stdout, strings.Join(names, " ")+"\n")
	case strings.HasPrefix(line, "sudo usermod -aG "):
		for _, group := range strings.Split(args[2], ",") {
			g.member[group] = true
		}
		return nil
	case strings.HasPrefix(line, "tmux list-sessions"):
		if len(g.sessions) == 0 {
			return exitStatus(1)
		}
		var out strings.Builder
		for _, s := range g.sessions {
			fmt.Fprintf(&out, "%d %s\n", tmuxPID, s)
		}
		return write(stdout, out.String())
	}
	return fmt.Errorf("unexpected command %q", strings.Join(argv, " "))
}

// write puts canned command output on w.
func write(w io.Writer, s string) error {
	if _, err := io.WriteString(w, s); err != nil {
		return fmt.Errorf("write canned output: %w", err)
	}
	return nil
}

// mutations are the commands the run launched under sudo, the only ones that
// change the box.
func (g *groupBox) mutations() [][]string {
	var found [][]string
	for _, argv := range g.calls {
		if argv[0] == "sudo" {
			found = append(found, argv)
		}
	}
	return found
}

// procWith is a /proc holding the tmux server's status with the given
// supplementary group ids.
func procWith(t *testing.T, groups string) string {
	t.Helper()
	proc := t.TempDir()
	dir := filepath.Join(proc, fmt.Sprint(tmuxPID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the tmux server's proc directory: %v", err)
	}
	status := "Name:\ttmux: server\nUid:\t1000\t1000\t1000\t1000\nGroups:\t" + groups + "\n"
	if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644); err != nil {
		t.Fatalf("write the tmux server's status: %v", err)
	}
	return proc
}

// convergeGroups runs a plan of one groups unit against box.
func convergeGroups(t *testing.T, box *groupBox, proc string, groups ...string) Outcome {
	t.Helper()
	result, err := Converge(t.Context(), Env{Command: box, Proc: proc}, []Unit{{Step: Groups, Groups: groups}}, io.Discard)
	if err != nil {
		t.Fatalf("Converge() err = %v, want nil", err)
	}
	return result.Outcomes[0]
}

func TestConvergeGroupsAddsSmithToOnlyTheMissingGroups(t *testing.T) {
	box := &groupBox{gids: map[string]int{"docker": 999, "kvm": 993}, member: map[string]bool{"kvm": true}}

	got := convergeGroups(t, box, t.TempDir(), "docker", "kvm")

	if got.Err != nil {
		t.Fatalf("Converge(groups docker, kvm).Err = %v, want nil", got.Err)
	}
	if want := [][]string{{"sudo", "usermod", "-aG", "docker", "smith"}}; !reflect.DeepEqual(box.mutations(), want) {
		t.Errorf("Converge(groups docker, kvm) ran %v, want %v", box.mutations(), want)
	}
	if want := "added smith to docker"; got.Summary != want {
		t.Errorf("Converge(groups docker, kvm).Summary = %q, want %q", got.Summary, want)
	}
}

func TestConvergeGroupsAlreadyAMemberChangesNothing(t *testing.T) {
	box := &groupBox{gids: map[string]int{"docker": 999}, member: map[string]bool{"docker": true}}

	got := convergeGroups(t, box, t.TempDir(), "docker")

	if got.Err != nil {
		t.Fatalf("Converge(groups docker).Err = %v, want nil", got.Err)
	}
	if len(box.mutations()) != 0 {
		t.Errorf("Converge(groups docker) ran %v, want no mutating command", box.mutations())
	}
	if want := "nothing to change; smith already in docker"; got.Summary != want {
		t.Errorf("Converge(groups docker).Summary = %q, want %q", got.Summary, want)
	}
}

func TestConvergeGroupsFailsNamingAGroupTheBoxDoesNotHold(t *testing.T) {
	box := &groupBox{gids: map[string]int{"docker": 999}, member: map[string]bool{}}
	plan := []Unit{{Step: Groups, Groups: []string{"docker", "dokcer"}}, {Step: Orphans, Workspace: Workspace{Root: t.TempDir()}}}

	result, err := Converge(t.Context(), Env{Command: box, Proc: t.TempDir()}, plan, io.Discard)
	if err != nil {
		t.Fatalf("Converge() err = %v, want nil", err)
	}

	if len(result.Outcomes) != 2 {
		t.Fatalf("Converge() outcomes = %+v, want the groups step and the orphans step after it", result.Outcomes)
	}
	groups, orphans := result.Outcomes[0], result.Outcomes[1]
	if groups.Err == nil {
		t.Fatal("Converge(groups docker, dokcer) groups Err = nil, want the missing group refused")
	}
	for _, want := range []string{`"dokcer"`, "packages"} {
		if !strings.Contains(groups.Err.Error(), want) {
			t.Errorf("Converge(groups docker, dokcer) groups Err = %q, want it to mention %s", groups.Err, want)
		}
	}
	if orphans.Err != nil {
		t.Errorf("Converge() orphans Err = %v, want the later step converged regardless", orphans.Err)
	}
	if !result.Failed() {
		t.Error("Converge().Failed() = false, want the run reported as failed")
	}
}

func TestConvergeGroupsNotesATmuxServerThatLacksADeclaredGroup(t *testing.T) {
	box := &groupBox{gids: map[string]int{"docker": 999}, member: map[string]bool{}, sessions: []string{"api", "smith-loop"}}

	got := convergeGroups(t, box, procWith(t, "1000 27"), "docker")

	if got.Err != nil {
		t.Fatalf("Converge(groups docker).Err = %v, want the note to leave the step converged", got.Err)
	}
	want := "added smith to docker; note: the tmux server running sessions api, smith-loop lacks docker, and new sessions get it only once all of them are closed"
	if got.Summary != want {
		t.Errorf("Converge(groups docker).Summary = %q, want %q", got.Summary, want)
	}
}

func TestConvergeGroupsNotesNothingWhenNoTmuxServerLacksTheGroups(t *testing.T) {
	tests := []struct {
		name     string
		sessions []string
		groups   string
	}{
		{"no server running", nil, ""},
		{"a server already in the group", []string{"api"}, "1000 999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			box := &groupBox{gids: map[string]int{"docker": 999}, member: map[string]bool{"docker": true}, sessions: tt.sessions}

			got := convergeGroups(t, box, procWith(t, tt.groups), "docker")

			if got.Err != nil {
				t.Fatalf("Converge(groups docker).Err = %v, want nil", got.Err)
			}
			if want := "nothing to change; smith already in docker"; got.Summary != want {
				t.Errorf("Converge(groups docker).Summary = %q, want %q", got.Summary, want)
			}
		})
	}
}
