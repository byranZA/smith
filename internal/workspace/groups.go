package workspace

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// defaultProc is the process filesystem on a real box.
const defaultProc = "/proc"

// absentStatus is the status getent exits with when the database holds no
// entry for the key it was asked for.
const absentStatus = 2

// addGroups adds the smith user to each declared group it is not already in,
// keeping the groups it holds, and reports what it did. Membership is only ever
// added: a group the blueprint stopped declaring is left as it is.
//
// Membership reaches new logins only, and every smith session is a child of
// one tmux server, so the report also notes a running server that lacks a
// declared group — on every run, whether or not this one added anything.
func addGroups(ctx context.Context, env Env, groups []string, progress io.Writer) (string, error) {
	ids, err := groupIDs(ctx, env.Command, groups)
	if err != nil {
		return "", err
	}
	user, err := output(ctx, env.Command, "id", "-un")
	if err != nil {
		return "", fmt.Errorf("read the smith user's name: %w", err)
	}
	held, err := output(ctx, env.Command, "id", "-nG", user)
	if err != nil {
		return "", fmt.Errorf("read the groups %s is in: %w", user, err)
	}
	var missing []string
	for _, g := range groups {
		if !slices.Contains(strings.Fields(held), g) {
			missing = append(missing, g)
		}
	}
	summary := fmt.Sprintf("nothing to change; %s already in %s", user, strings.Join(groups, ", "))
	if len(missing) > 0 {
		args := []string{"usermod", "-aG", strings.Join(missing, ","), user}
		if err := env.Command.Run(ctx, "sudo", args, nil, progress, progress); err != nil {
			return "", fmt.Errorf("add %s to %s: %w", user, strings.Join(missing, ", "), err)
		}
		summary = fmt.Sprintf("added %s to %s", user, strings.Join(missing, ", "))
	}
	note, err := staleServerNote(ctx, env, groups, ids)
	if err != nil {
		return "", err
	}
	return summary + note, nil
}

// groupIDs looks each declared group up in the box's group database and
// returns its id by name. A group the box does not hold fails the lookup, named
// with where it should have come from: smith adds the user to groups and never
// creates one.
func groupIDs(ctx context.Context, run Runner, groups []string) (map[string]string, error) {
	ids := make(map[string]string, len(groups))
	var absent []string
	for _, g := range groups {
		entry, err := output(ctx, run, "getent", "group", g)
		var exit interface{ ExitCode() int }
		switch {
		case errors.As(err, &exit) && exit.ExitCode() == absentStatus:
			absent = append(absent, fmt.Sprintf("%q", g))
			continue
		case err != nil:
			return nil, fmt.Errorf("look up group %q: %w", g, err)
		}
		fields := strings.Split(entry, ":")
		if len(fields) < 3 {
			return nil, fmt.Errorf("look up group %q: getent answered %q, which is not a group entry", g, entry)
		}
		ids[g] = fields[2]
	}
	if len(absent) > 0 {
		return nil, fmt.Errorf("the box has no group %s: groups come from packages, so declare the package that creates it or check the name for a typo", strings.Join(absent, ", "))
	}
	return ids, nil
}

// staleServerNote is the note owed when the smith user's tmux server is running
// without one of the declared groups, naming its sessions, and empty when no
// server is running or the one that is carries them all. The server's own
// groups are read, not the user's membership: they are what every session it
// starts inherits.
func staleServerNote(ctx context.Context, env Env, groups []string, ids map[string]string) (string, error) {
	pid, sessions := tmuxServer(ctx, env.Command)
	if len(sessions) == 0 {
		return "", nil
	}
	status, err := os.ReadFile(filepath.Join(env.proc(), pid, "status"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the groups of the tmux server: %w", err)
	}
	carried := processGroups(string(status))
	var lacking []string
	for _, g := range groups {
		if !slices.Contains(carried, ids[g]) {
			lacking = append(lacking, g)
		}
	}
	if len(lacking) == 0 {
		return "", nil
	}
	return fmt.Sprintf("; note: the tmux server running sessions %s lacks %s, and new sessions get it only once all of them are closed",
		strings.Join(sessions, ", "), strings.Join(lacking, ", ")), nil
}

// tmuxServer is the process id of the tmux server on the default socket and
// the sessions it is running, none when no server is running. tmux answers a
// list with no server to ask by failing, so a failed list is read as the
// answer rather than as an error.
func tmuxServer(ctx context.Context, run Runner) (string, []string) {
	listing, err := output(ctx, run, "tmux", "list-sessions", "-F", "#{pid} #{session_name}")
	if err != nil {
		return "", nil
	}
	var pid string
	var sessions []string
	for _, line := range strings.Split(listing, "\n") {
		server, session, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid = server
		sessions = append(sessions, session)
	}
	return pid, sessions
}

// processGroups are the supplementary group ids a /proc status file lists on
// its Groups line.
func processGroups(status string) []string {
	for _, line := range strings.Split(status, "\n") {
		if rest, ok := strings.CutPrefix(line, "Groups:"); ok {
			return strings.Fields(rest)
		}
	}
	return nil
}

// output runs a command that answers on stdout and returns that answer,
// trimmed of the newline that ends it.
func output(ctx context.Context, run Runner, name string, args ...string) (string, error) {
	var out bytes.Buffer
	if err := run.Run(ctx, name, args, nil, &out, io.Discard); err != nil {
		return "", fmt.Errorf("run %s: %w", strings.Join(append([]string{name}, args...), " "), err)
	}
	return strings.TrimSpace(out.String()), nil
}
