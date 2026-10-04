package staging

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

// adasResolution is a resolution carrying every fixed-key field it stages.
func adasResolution() Resolution {
	return Resolution{
		Access:    "tailscale",
		Terminal:  "tmux",
		Workspace: "~/code",
		Git:       Identity{UserName: "Ada", UserEmail: "ada@example.com"},
	}
}

// stageResolution stages data as the resolution under a fresh box root and returns that root.
func stageResolution(t *testing.T, data []byte) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "resolved.json"), data, 0o644); err != nil {
		t.Fatalf("write staged resolution: %v", err)
	}
	return root
}

func TestPlanStagesTheResolutionRootOwnedAt0644(t *testing.T) {
	tree := Plan([]byte("access: public\n"), blueprint.Blueprint{}, adasResolution())

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"path", tree.Resolution.File.Path, "/etc/smith/resolved.json"},
		{"mode", tree.Resolution.File.Mode, "0644"},
		{"owner", tree.Resolution.File.Owner, "root:root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("Resolution.File %s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestResolveStagesTheResolvedFixedKeyFieldsAsJSON(t *testing.T) {
	tree, err := Resolve(Plan([]byte("access: public\n"), blueprint.Blueprint{}, adasResolution()), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := `{
  "access": "tailscale",
  "terminal": "tmux",
  "workspace": "~/code",
  "git": {
    "user_name": "Ada",
    "user_email": "ada@example.com"
  }
}
`
	if got := string(tree.Resolution.File.Bytes); got != want {
		t.Errorf("staged resolution =\n%s\nwant\n%s", got, want)
	}
}

func TestResolveLeavesAnUndeclaredIdentityOutOfTheResolution(t *testing.T) {
	r := Resolution{Access: "public", Terminal: "tmux", Workspace: "~/workspace"}
	tree, err := Resolve(Plan(nil, blueprint.Blueprint{}, r), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	for _, unwanted := range []string{`"git"`, "provider"} {
		if strings.Contains(string(tree.Resolution.File.Bytes), unwanted) {
			t.Errorf("staged resolution = %s, want no %q", tree.Resolution.File.Bytes, unwanted)
		}
	}
}

func TestLoadResolutionReadsBackWhatWasStaged(t *testing.T) {
	tree, err := Resolve(Plan(nil, blueprint.Blueprint{}, adasResolution()), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	root := stageResolution(t, tree.Resolution.File.Bytes)

	got, err := LoadResolution(root)
	if err != nil {
		t.Fatalf("LoadResolution() error = %v", err)
	}
	if got != adasResolution() {
		t.Errorf("LoadResolution() = %+v, want %+v", got, adasResolution())
	}
}

func TestLoadResolutionRefusesAnAbsentResolutionAsAbsent(t *testing.T) {
	root := t.TempDir()

	_, err := LoadResolution(root)
	var absent *AbsentError
	if !errors.As(err, &absent) {
		t.Fatalf("LoadResolution() err = %v, want an *AbsentError", err)
	}
	for _, want := range []string{"machine setup", filepath.Join(root, "resolved.json"), "resolution"} {
		if !strings.Contains(absent.Error(), want) {
			t.Errorf("LoadResolution() err = %q, want it to name %q", absent, want)
		}
	}
}

func TestLoadResolutionRefusesInvalidJSONAsMalformed(t *testing.T) {
	root := stageResolution(t, []byte(`{"workspace": "~/co`))

	_, err := LoadResolution(root)
	var malformed *MalformedError
	if !errors.As(err, &malformed) {
		t.Fatalf("LoadResolution() err = %v, want a *MalformedError", err)
	}
	for _, want := range []string{"machine setup", "resolution"} {
		if !strings.Contains(malformed.Error(), want) {
			t.Errorf("LoadResolution() err = %q, want it to name %q", malformed, want)
		}
	}
}

func TestConvergeStagesTheResolutionRootOwnedAt0644(t *testing.T) {
	tree := resolvedTreeOf(t, adasResolution())
	box := &fakeBox{}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	want := File{Path: ResolutionPath, Bytes: tree.Resolution.File.Bytes, Mode: "0644", Owner: "root:root"}
	if !box.wrote(want) {
		t.Errorf("the resolution was not put in place root-owned at 0644:\n%s", strings.Join(box.commands, "\n"))
	}
	if change, ok := changeOf(result, ResolutionPath); !ok || change != Staged {
		t.Errorf("Result reported %s as %v, want it staged", ResolutionPath, change)
	}
}

func TestConvergeLeavesAnUnchangedResolutionAlone(t *testing.T) {
	tree := resolvedTreeOf(t, adasResolution())
	box := &fakeBox{files: map[string]string{ResolutionPath: string(tree.Resolution.File.Bytes)}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if change, ok := changeOf(result, ResolutionPath); !ok || change != Unchanged {
		t.Errorf("Result reported %s as %v, want it unchanged", ResolutionPath, change)
	}
}

func TestConvergeReportsAChangedResolutionAsUpdated(t *testing.T) {
	before := resolvedTreeOf(t, Resolution{Access: "public", Terminal: "tmux", Workspace: "~/workspace"})
	tree := resolvedTreeOf(t, Resolution{Access: "public", Terminal: "tmux", Workspace: "~/code"})
	box := &fakeBox{files: map[string]string{ResolutionPath: string(before.Resolution.File.Bytes)}}

	result, err := Converge(context.Background(), box, tree)
	if err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if change, ok := changeOf(result, ResolutionPath); !ok || change != Updated {
		t.Errorf("Result reported %s as %v, want it updated", ResolutionPath, change)
	}
}

// resolvedTreeOf is the resolved tree of an empty blueprint staged with r.
func resolvedTreeOf(t *testing.T, r Resolution) Tree {
	t.Helper()
	tree, err := Resolve(Plan([]byte("access: public\n"), blueprint.Blueprint{}, r), operatorValues, operatorValues)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	return tree
}

func TestLoadResolutionRefusesAnIncompleteOrInvalidResolutionAsMalformed(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		names string
	}{
		{"null", `null`, "workspace"},
		{"empty object", `{}`, "workspace"},
		{"missing workspace", `{"access": "public", "terminal": "tmux"}`, "workspace"},
		{"null workspace", `{"access": "public", "terminal": "tmux", "workspace": null}`, "workspace"},
		{"empty workspace", `{"access": "public", "terminal": "tmux", "workspace": ""}`, "workspace"},
		{"missing access", `{"terminal": "tmux", "workspace": "~/code"}`, "access"},
		{"missing terminal", `{"access": "public", "workspace": "~/code"}`, "terminal"},
		{"unknown access", `{"access": "vpn", "terminal": "tmux", "workspace": "~/code"}`, `"vpn"`},
		{"unknown terminal", `{"access": "public", "terminal": "screen", "workspace": "~/code"}`, `"screen"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := stageResolution(t, []byte(tt.data))

			_, err := LoadResolution(root)
			var malformed *MalformedError
			if !errors.As(err, &malformed) {
				t.Fatalf("LoadResolution() err = %v, want a *MalformedError", err)
			}
			for _, want := range []string{"machine setup", "resolution", tt.names} {
				if !strings.Contains(malformed.Error(), want) {
					t.Errorf("LoadResolution() err = %q, want it to name %q", malformed, want)
				}
			}
		})
	}
}

func TestLoadResolutionAcceptsAnOmittedOrPartialIdentity(t *testing.T) {
	tests := []struct {
		name string
		data string
		want Identity
	}{
		{"omitted", `{"access": "public", "terminal": "tmux", "workspace": "~/code"}`, Identity{}},
		{"name only", `{"access": "public", "terminal": "tmux", "workspace": "~/code", "git": {"user_name": "Ada"}}`, Identity{UserName: "Ada"}},
		{"email only", `{"access": "public", "terminal": "tmux", "workspace": "~/code", "git": {"user_email": "ada@example.com"}}`, Identity{UserEmail: "ada@example.com"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := stageResolution(t, []byte(tt.data))

			got, err := LoadResolution(root)
			if err != nil {
				t.Fatalf("LoadResolution() error = %v", err)
			}
			if got.Git != tt.want {
				t.Errorf("LoadResolution().Git = %+v, want %+v", got.Git, tt.want)
			}
		})
	}
}
