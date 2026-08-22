package staging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stage writes document as the staged blueprint of a box rooted at a fresh
// temporary directory, and answers with that root.
func stage(t *testing.T, document string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blueprint.yaml"), []byte(document), 0o644); err != nil {
		t.Fatalf("write staged blueprint: %v", err)
	}
	return root
}

func TestLoadReadsTheStagedBlueprint(t *testing.T) {
	root := stage(t, `workspace: /home/smith/work
tools:
  node: "22"
env:
  NPM_TOKEN: env:NPM_TOKEN
repos:
  - name: api
    url: git@github.com:acme/api.git
`)

	b, err := Load(root)
	if err != nil {
		t.Fatalf("Load() err = %v, want nil", err)
	}
	if b.Workspace != "/home/smith/work" {
		t.Errorf("Workspace = %q, want %q", b.Workspace, "/home/smith/work")
	}
	if b.Tools["node"] != "22" {
		t.Errorf("Tools[node] = %q, want %q", b.Tools["node"], "22")
	}
	if b.Env["NPM_TOKEN"] != "env:NPM_TOKEN" {
		t.Errorf("Env[NPM_TOKEN] = %q, want %q", b.Env["NPM_TOKEN"], "env:NPM_TOKEN")
	}
	if len(b.Repos) != 1 || b.Repos[0].Name != "api" {
		t.Errorf("Repos = %+v, want the api repo", b.Repos)
	}
}

func TestLoadRefusesAnAbsentStagedBlueprintAsAbsent(t *testing.T) {
	root := t.TempDir()

	_, err := Load(root)
	var absent *AbsentError
	if !errors.As(err, &absent) {
		t.Fatalf("Load() err = %v, want an *AbsentError", err)
	}
	if !strings.Contains(absent.Error(), "machine setup") {
		t.Errorf("Load() err = %q, want it to name machine setup", absent)
	}
	if want := filepath.Join(root, "blueprint.yaml"); !strings.Contains(absent.Error(), want) {
		t.Errorf("Load() err = %q, want it to name %q", absent, want)
	}
}

func TestLoadRefusesATruncatedStagedBlueprintAsMalformed(t *testing.T) {
	root := stage(t, "repos:\n  - name: \"api\n")

	_, err := Load(root)
	var malformed *MalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("Load() err = %v, want a *MalformedError", err)
	}
	if !strings.Contains(malformed.Error(), "machine setup") {
		t.Errorf("Load() err = %q, want it to name machine setup", malformed)
	}
}

func TestLoadRefusesAnUnknownKeyNamingTheKeyAndItsLine(t *testing.T) {
	root := stage(t, "access: tailscale\nterminals: tmux\n")

	_, err := Load(root)
	var malformed *MalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("Load() err = %v, want a *MalformedError", err)
	}
	for _, want := range []string{"terminals", "line 2"} {
		if !strings.Contains(malformed.Error(), want) {
			t.Errorf("Load() err = %q, want it to name %q", malformed, want)
		}
	}
}

func TestLoadWordsTheTwoRefusalsApart(t *testing.T) {
	_, absent := Load(t.TempDir())
	_, malformed := Load(stage(t, "repos:\n  - name: \"api\n"))

	if absent == nil || malformed == nil {
		t.Fatalf("Load() errs = %v, %v, want both refused", absent, malformed)
	}
	if absent.Error() == malformed.Error() {
		t.Fatalf("both refusals read %q, want them worded apart", absent)
	}
	// The absent refusal is a provisioning gap, not a corrupt document, and
	// must not read as one.
	if strings.Contains(absent.Error(), "malformed") {
		t.Errorf("absent refusal = %q, want it not to read as malformed", absent)
	}
	var m *MalformedError
	if errors.As(absent, &m) {
		t.Errorf("absent refusal = %v, want it not to be a *MalformedError too", absent)
	}
	var a *AbsentError
	if errors.As(malformed, &a) {
		t.Errorf("malformed refusal = %v, want it not to be an *AbsentError too", malformed)
	}
}

func TestLoadReadsTheDocumentTheWriterStages(t *testing.T) {
	document := []byte("access: tailscale\n")
	tree := Plan(document)
	root := stage(t, string(document))

	if got := filepath.Join(root, filepath.Base(tree.Document.Path)); got != DocumentPathIn(root) {
		t.Errorf("DocumentPathIn(%q) = %q, want the staged document's own name %q", root, DocumentPathIn(root), got)
	}
	if DocumentPathIn(Root) != DocumentPath {
		t.Errorf("DocumentPathIn(Root) = %q, want %q", DocumentPathIn(Root), DocumentPath)
	}
}
