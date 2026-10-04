package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/connection"
)

// stageBox writes a staged blueprint, and a staged resolution when resolution
// is not nil, under a box state directory of the test's own, and answers with
// that directory.
func stageBox(t *testing.T, document string, resolution []byte) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "blueprint.yaml"), []byte(document), 0o644); err != nil {
		t.Fatalf("stage the blueprint: %v", err)
	}
	if resolution != nil {
		if err := os.WriteFile(filepath.Join(root, "resolved.json"), resolution, 0o644); err != nil {
			t.Fatalf("stage the resolution: %v", err)
		}
	}
	return root
}

// stagedIn reads the box configuration staged under root, as on-box smith
// reads it under /etc/smith.
func stagedIn(root string) boxResolver {
	return func() (boxConfig, error) { return stagedBoxConfigIn(root) }
}

func TestSessionStartCreatesTheWorktreeUnderAPreferenceOnlyWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "code")
	writeBareRepo(t, workspace, "smith")
	root := stageBox(t, "repos:\n  - url: https://forge.test/smith.git\n",
		[]byte(`{"access": "public", "terminal": "tmux", "workspace": "~/code"}`))

	_, stderr, code := runSessionOn(t,
		onBox(t, stagedIn(root), root, connection.System(), &fakeTmux{}, &fakeExec{}),
		"start", "--repo", "smith", "--branch", "spec-42", "--detach")

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr)
	}
	worktree := filepath.Join(workspace, "smith", "worktrees", "spec-42")
	if _, err := os.Stat(worktree); err != nil {
		t.Errorf("worktree at %s: %v, want it created under the staged workspace", worktree, err)
	}
}

func TestSessionVerbsRefuseABoxWithNoUsableStagedResolution(t *testing.T) {
	tests := []struct {
		name       string
		resolution []byte
		code       int
		says       []string
	}{
		{"absent", nil, exitStagedBlueprintAbsent, []string{"no resolution is staged", "machine setup"}},
		{"malformed", []byte(`{"workspace": "~/co`), exitStagedBlueprintMalformed, []string{"staged resolution", "is malformed", "machine setup"}},
		{"null", []byte(`null`), exitStagedBlueprintMalformed, []string{"staged resolution", "is malformed", "workspace", "machine setup"}},
		{"no workspace", []byte(`{"access": "public", "terminal": "tmux"}`), exitStagedBlueprintMalformed, []string{"staged resolution", "workspace is missing", "machine setup"}},
		{"unknown access", []byte(`{"access": "vpn", "terminal": "tmux", "workspace": "~/code"}`), exitStagedBlueprintMalformed, []string{"staged resolution", `"vpn"`, "machine setup"}},
	}
	verbs := [][]string{
		{"start", "--repo", "smith", "--branch", "spec-42", "--detach"},
		{"list"},
	}
	for _, tt := range tests {
		for _, verb := range verbs {
			t.Run(tt.name+"/"+verb[0], func(t *testing.T) {
				root := stageBox(t, "repos:\n  - url: https://forge.test/smith.git\n", tt.resolution)
				tmux := &fakeTmux{}

				cmd := newSessionCmd(onBox(t, stagedIn(root), root, connection.System(), tmux, &fakeExec{}))
				cmd.SetArgs(verb)
				cmd.SetOut(io.Discard)
				cmd.SilenceUsage, cmd.SilenceErrors = true, true
				err := cmd.Execute()

				for _, want := range tt.says {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Errorf("refusal = %v, want it to contain %q", err, want)
					}
				}
				if code := codeFromError(err); code != tt.code {
					t.Errorf("exit code = %d, want %d", code, tt.code)
				}
				if len(tmux.calls) != 0 {
					t.Errorf("a refused verb ran tmux %v, want nothing", tmux.calls)
				}
			})
		}
	}
}
