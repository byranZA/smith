package session

import "strings"

// tmuxNamespace prefixes every tmux session smith owns. It is what keeps a
// session smith started apart from one the operator started by hand: a bare
// "smith-main" typed at a prompt is not "smith/smith-main", so smith never
// claims it and never kills it.
const tmuxNamespace = "smith/"

// DeriveName returns the name of the session for a repo and a branch:
// "<repo>-<branch>", sanitized to one path segment.
//
// Names are derived, never chosen, because the name is the only string the
// other session verbs take and it has to be reachable from the pair the
// operator already knows. The derivation is deliberately lossy — every
// character outside [A-Za-z0-9._-] becomes a dash — so a branch is never
// recovered from a name: smith reads the true branch back from git.
func DeriveName(repo, branch string) string {
	return sanitize(repo + "-" + branch)
}

// TmuxSession returns the tmux session a session of that name runs under:
// "smith/<name>". The namespace exists so smith's sessions and the operator's
// own cannot collide, in either direction.
func TmuxSession(name string) string {
	return tmuxNamespace + name
}

// worktreeDir is the directory a branch's worktree occupies below its repo's
// worktrees directory: the branch, sanitized to one path segment. It is the
// branch alone rather than the session name because the repo is already the
// directory above.
func worktreeDir(branch string) string {
	return sanitize(branch)
}

// sanitize reduces s to one path segment by replacing every character outside
// [A-Za-z0-9._-] with a dash.
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}
