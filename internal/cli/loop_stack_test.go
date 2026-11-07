package cli

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/hint"
	"github.com/byranZA/smith/internal/loop"
	"github.com/byranZA/smith/internal/staging"
)

// recordingStack fakes the stack a run on a box brings up and down.
type recordingStack struct{ ups, downs []string }

func (r *recordingStack) Ensure(_ context.Context, dir string, _ io.Writer) error {
	r.ups = append(r.ups, dir)
	return nil
}

func (r *recordingStack) Release(context.Context, io.Writer) error {
	if len(r.downs) >= len(r.ups) {
		return nil
	}
	r.downs = append(r.downs, "down")
	return nil
}

// onABox is a box resolver for a machine that has been set up as a box, with
// a workspace that holds no repos.
func onABox(t *testing.T) boxResolver {
	t.Helper()
	return resolvedBoxPlacing(t.TempDir(), "api")
}

// offABox is a box resolver for a machine nothing was staged on.
func offABox(inv hint.Invocation) (boxConfig, error) {
	return boxConfig{}, &staging.AbsentError{Path: "/var/lib/smith/blueprint.yaml", Hint: inv}
}

const stackFile = "stack:\n  up: make up\n  down: make down\n"

func TestLoopListOnABoxShowsTheStackOnFromTheRepoFile(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, stackFile)
	w.box = onABox(t)

	stdout, stderr, code := runLoopListIn(t, w, "42")

	if want := "push:   on (built-in default)\nstack:  on (repo file)\n"; code != 0 || !strings.Contains(stdout, want) {
		t.Errorf("exit %d, stderr %q, stdout =\n%s\nwant 0 holding %q", code, stderr, stdout, want)
	}
}

func TestLoopListOnABoxWithNoStackShowsNoStackLine(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, "push: true\n")
	w.box = onABox(t)

	stdout, _, code := runLoopListIn(t, w, "42")

	if code != 0 || strings.Contains(stdout, "stack:") {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 and no stack line", code, stdout)
	}
}

func TestLoopListOffABoxShowsNoStackLine(t *testing.T) {
	t.Parallel()
	gh := labelledSpec()
	w, _ := repoWith(t, gh, &idleAgent{}, stackFile)
	w.box = offABox

	stdout, _, code := runLoopListIn(t, w, "42")

	if code != 0 || strings.Contains(stdout, "stack:") {
		t.Errorf("exit %d, stdout =\n%s\nwant 0 and no stack line", code, stdout)
	}
}

func TestLoopRunOnABoxBringsTheDeclaredStackUpAndDown(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = onABox(t)
	stack := &recordingStack{}
	w.newStack = func() loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 1 || len(stack.downs) != 1 || !strings.Contains(stdout, "stack:  on (repo file)\n") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want 0, one up, one down and the stack line", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOffABoxRunsNoStack(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, stackFile)
	w.box = offABox
	stack := &recordingStack{}
	w.newStack = func() loop.Stacker { return stack }

	stdout, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 0 || len(stack.downs) != 0 || strings.Contains(stdout, "stack:") {
		t.Errorf("exit %d, ups %v, downs %v, stdout %q, stderr %q; want 0 and no stack", code, stack.ups, stack.downs, stdout, stderr)
	}
}

func TestLoopRunOnABoxWhoseRepoDeclaresNoStackRunsNoStack(t *testing.T) {
	t.Parallel()
	gh := &fakeTracker{issues: map[string]string{"80": taskJSON(80, "OPEN", "ready-for-agent")}}
	w, _ := repoWith(t, gh, &fakeAgent{gh: gh}, "push: true\n")
	w.box = onABox(t)
	stack := &recordingStack{}
	w.newStack = func() loop.Stacker { return stack }

	_, stderr, code := runLoopRun(t, w, "80")

	if code != 0 || len(stack.ups) != 0 {
		t.Errorf("exit %d, ups %v, stderr %q; want 0 and no stack", code, stack.ups, stderr)
	}
}
