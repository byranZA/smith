package cli

import "github.com/byranZA/smith/internal/session"

// sessionListDocument is the JSON document `session list --json` prints.
type sessionListDocument struct {
	SchemaVersion int                `json:"schema_version"`
	Sessions      []sessionListEntry `json:"sessions"`
}

// sessionListEntry is one session in the document.
type sessionListEntry struct {
	Name     string `json:"name"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch,omitempty"`
	Worktree string `json:"worktree"`
	Live     bool   `json:"live"`
	Dirty    bool   `json:"dirty"`
	Unpushed int    `json:"unpushed"`
	Driver   string `json:"driver"`
	Outcome  string `json:"outcome,omitempty"`
}

// newSessionListDocument shapes sessions as the document, with an empty list rather than null when there are none.
func newSessionListDocument(sessions []session.Session) sessionListDocument {
	doc := sessionListDocument{SchemaVersion: 1, Sessions: make([]sessionListEntry, 0, len(sessions))}
	for _, s := range sessions {
		doc.Sessions = append(doc.Sessions, sessionListEntry{
			Name: s.Name, Repo: s.Repo, Branch: s.Branch, Worktree: s.Worktree,
			Live: s.Live, Dirty: s.Dirty, Unpushed: s.Unpushed,
			Driver: s.Driver.String(), Outcome: s.Outcome,
		})
	}
	return doc
}
