package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
	"github.com/byranZA/smith/internal/session"
)

// registryEnv declares two repos and clones both, so a test can pin that every
// verb reads the same registry rather than only the repo it was handed.
func registryEnv(t *testing.T) session.Env {
	t.Helper()
	workspace := t.TempDir()
	writeBareRepo(t, workspace, "smith", "main")
	writeBareRepo(t, workspace, "web", "main")
	return session.Env{
		Workspace: workspace,
		Repos:     []session.Repo{{Name: "smith"}, {Name: "web"}},
		Git:       connection.System(),
		Tmux:      &tmuxServer{},
		Exec:      &fakeExec{},
	}
}

// TestEveryVerbAddressesTheNameListReports locks in that the five verbs share
// one registry: the name list prints for a session in the second declared repo
// is the name attach, stop and rm resolve, and the branch each of them reads
// back is the one git holds rather than the one the name spells.
func TestEveryVerbAddressesTheNameListReports(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	started, err := session.Start(ctx, env, session.StartRequest{Repo: "web", Branch: "web/spec-42"})
	if err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	listed, err := session.List(ctx, env, session.Filter{})
	if err != nil {
		t.Fatalf("List() err = %v", err)
	}
	if len(listed) != 1 || listed[0].Name != started.Name || listed[0].Branch != "web/spec-42" {
		t.Fatalf("List() = %+v, want one %q on branch %q", listed, started.Name, "web/spec-42")
	}
	name := listed[0].Name

	if err := session.Attach(ctx, env, name, session.Interact); err != nil {
		t.Fatalf("Attach(%q) err = %v", name, err)
	}
	if err := session.Stop(ctx, env, name); err != nil {
		t.Fatalf("Stop(%q) err = %v", name, err)
	}
	removed, err := session.Remove(ctx, env, []string{name}, false)
	if err != nil {
		t.Fatalf("Remove(%q) err = %v", name, err)
	}
	if len(removed) != 1 || removed[0].Repo != "web" || removed[0].Branch != "web/spec-42" {
		t.Fatalf("Remove() = %+v, want the web repo's branch %q", removed, "web/spec-42")
	}
}

// TestEveryVerbRefusesAnUnknownNameAlike locks in that name resolution has one
// home: a name no session holds reads the same at every verb that takes one,
// so a typo cannot mean three different things.
func TestEveryVerbRefusesAnUnknownNameAlike(t *testing.T) {
	ctx := context.Background()
	env := registryEnv(t)
	if _, err := session.Start(ctx, env, session.StartRequest{Repo: "smith", Branch: "spec-42"}); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	refusals := map[string]error{
		"attach": session.Attach(ctx, env, "smith-ghost", session.Observe),
		"stop":   session.Stop(ctx, env, "smith-ghost"),
	}
	_, refusals["rm"] = session.Remove(ctx, env, []string{"smith-ghost"}, false)

	for verb, err := range refusals {
		if err == nil {
			t.Fatalf("%s of an unknown name err = nil, want a refusal", verb)
		}
		for _, want := range []string{"smith-ghost", "session list"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s of an unknown name err = %q, want it to contain %q", verb, err, want)
			}
		}
	}
}
