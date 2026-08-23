package staging

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// declaresEnv is a blueprint exporting one variable on the box and one in a
// repo, which is the whole of what the staged env carries.
func declaresEnv() blueprint.Blueprint {
	return blueprint.Blueprint{
		Env: map[string]string{"GITHUB_TOKEN": "env:GH_TOKEN"},
		Repos: []blueprint.Repo{{
			Name: "acme",
			URL:  "https://forge.test/acme.git",
			Env:  map[string]string{"LOG_LEVEL": "literal:debug"},
		}},
	}
}

// operatorValues stands in for the operator's own value resolver: it answers
// the references their machine can read and nothing else, so a test drives the
// real resolution seam without reaching the environment it runs in.
func operatorValues(ref string) (string, error) {
	switch ref {
	case "env:GH_TOKEN":
		return "ghp_fromtheoperator", nil
	case "literal:debug":
		return "debug", nil
	default:
		return "", fmt.Errorf("nothing resolves %q", ref)
	}
}

// stageEnv writes a resolved tree's staged env under root, the way Converge
// puts it on a box, so a test can read it back through the on-box reader.
func stageEnv(t *testing.T, root string, tree Tree) {
	t.Helper()
	path := EnvPathIn(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("make the box state directory: %v", err)
	}
	if err := os.WriteFile(path, tree.Env.File.Bytes, 0o600); err != nil {
		t.Fatalf("stage the env: %v", err)
	}
}

// TestPlanStagesTheEnvSmithOwnedAt0600 proves the resolved values are staged as
// a file of their own, readable by the account that consumes them and nothing
// else: they are provisioned secrets, not the world-readable document.
func TestPlanStagesTheEnvSmithOwnedAt0600(t *testing.T) {
	tree := Plan([]byte("access: public\n"), declaresEnv())

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"path", tree.Env.File.Path, "/etc/smith/env.json"},
		{"mode", tree.Env.File.Mode, "0600"},
		{"owner", tree.Env.File.Owner, "smith:smith"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Env.File %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
	if len(tree.Env.File.Bytes) != 0 {
		t.Errorf("Env.File.Bytes = %q, want nothing until the references are resolved", tree.Env.File.Bytes)
	}
}

// TestResolveStagesTheValueOfEveryDeclaredReference proves the operator's own
// machine is what reads an env: or a file: reference, and that what travels to
// the box is the value rather than the reference. A box re-resolving
// env:GH_TOKEN against its own environment would export nothing at all.
func TestResolveStagesTheValueOfEveryDeclaredReference(t *testing.T) {
	root := t.TempDir()

	tree, err := Resolve(Plan([]byte("access: public\n"), declaresEnv()), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	stageEnv(t, root, tree)

	if strings.Contains(string(tree.Env.File.Bytes), "env:GH_TOKEN") {
		t.Errorf("staged env = %q, want the value rather than the reference", tree.Env.File.Bytes)
	}
	tests := []struct {
		scope string
		repo  string
		name  string
		want  string
	}{
		{"box", "", "GITHUB_TOKEN", "ghp_fromtheoperator"},
		{"repo", "acme", "LOG_LEVEL", "debug"},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			got, err := ReadValue(root, tt.repo, tt.name)
			if err != nil {
				t.Fatalf("ReadValue() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ReadValue(%q, %q) = %q, want %q", tt.repo, tt.name, got, tt.want)
			}
		})
	}
}

// TestResolveEnumeratesEveryValueItCannotResolve proves an env the operator's
// machine cannot resolve refuses the whole run by name, with the box untouched
// and every failure reported rather than the first.
func TestResolveEnumeratesEveryValueItCannotResolve(t *testing.T) {
	b := blueprint.Blueprint{
		Env:   map[string]string{"GITHUB_TOKEN": "env:ABSENT", "NPM_TOKEN": "file:/nowhere"},
		Repos: []blueprint.Repo{{Name: "acme", URL: "https://forge.test/acme.git"}},
	}

	_, err := Resolve(Plan([]byte("access: public\n"), b), operatorValues, operatorValues)

	if err == nil {
		t.Fatal("Resolve() error = nil, want both unresolvable values refused")
	}
	for _, want := range []string{"GITHUB_TOKEN", "NPM_TOKEN"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Resolve() error = %v, want it to name %q", err, want)
		}
	}
	var unresolved *UnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("Resolve() error = %v, want an *UnresolvedError", err)
	}
}

// TestResolveReadsAValueThroughTheValueResolver proves a blueprint value
// resolves by the rules its own field carries — literal: among them — while a
// placement source keeps the narrower set. The two resolvers are separate
// because the schemes they accept are.
func TestResolveReadsAValueThroughTheValueResolver(t *testing.T) {
	b := blueprint.Blueprint{Env: map[string]string{"LOG_LEVEL": "literal:debug"}}
	refuse := func(ref string) (string, error) { return "", fmt.Errorf("no source resolves %q", ref) }

	tree, err := Resolve(Plan([]byte("access: public\n"), b), refuse, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if got := string(tree.Env.File.Bytes); !strings.Contains(got, "debug") {
		t.Errorf("staged env = %q, want the literal resolved through the value resolver", got)
	}
}

// TestReadValueRefusesAVariableWithNoStagedValue proves a box never falls back
// to its own environment for a variable the operator declared: the refusal
// names the variable and points at the one command that stages a value for it.
func TestReadValueRefusesAVariableWithNoStagedValue(t *testing.T) {
	root := t.TempDir()
	tree, err := Resolve(Plan([]byte("access: public\n"), declaresEnv()), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	stageEnv(t, root, tree)
	t.Setenv("UNDECLARED", "from-the-box")

	_, err = ReadValue(root, "", "UNDECLARED")

	if err == nil {
		t.Fatal("ReadValue() error = nil, want a variable with no staged value refused")
	}
	var missing *MissingValueError
	if !errors.As(err, &missing) {
		t.Fatalf("ReadValue() error = %v, want a *MissingValueError", err)
	}
	for _, want := range []string{"UNDECLARED", "smith machine setup"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ReadValue() error = %v, want it to name %q", err, want)
		}
	}
}

// TestReadValueRefusesOnABoxWithNothingStaged proves a box staged before the
// env artifact existed refuses by name rather than reading the value out of
// its own environment.
func TestReadValueRefusesOnABoxWithNothingStaged(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "from-the-box")

	_, err := ReadValue(t.TempDir(), "", "GITHUB_TOKEN")

	var missing *MissingValueError
	if !errors.As(err, &missing) {
		t.Fatalf("ReadValue() error = %v, want a *MissingValueError", err)
	}
}

// TestConvergeStagesTheResolvedEnvOnTheBox proves the values the operator's
// machine resolved are what reach the box, staged smith-owned at 0600 beside
// the document, so on-box smith reads a value rather than resolving a
// reference of its own.
func TestConvergeStagesTheResolvedEnvOnTheBox(t *testing.T) {
	tree, err := Resolve(Plan([]byte("access: public\n"), declaresEnv()), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	box := &fakeBox{}

	if _, err := Converge(context.Background(), box, tree); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}

	want := File{Path: EnvPath, Bytes: tree.Env.File.Bytes, Mode: "0600", Owner: "smith:smith"}
	if !box.wrote(want) {
		t.Errorf("the env was not staged smith-owned at 0600:\n%s", strings.Join(box.commands, "\n"))
	}
	for _, value := range []string{"ghp_fromtheoperator", "debug"} {
		if !strings.Contains(string(tree.Env.File.Bytes), value) {
			t.Errorf("staged env = %q, want it to carry %q", tree.Env.File.Bytes, value)
		}
	}
}
