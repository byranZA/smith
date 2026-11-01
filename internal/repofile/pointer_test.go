package repofile

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanPointerPutsTheLineWhereEachAgentReadsIt(t *testing.T) {
	const mention = "see .smith/README.md\n"
	tests := []struct {
		name  string
		files map[string]string
		want  PointerPlan
	}{
		{"AGENTS.md only", map[string]string{"AGENTS.md": "a\n"}, PointerPlan{Append: []string{"AGENTS.md"}}},
		{"CLAUDE.md only", map[string]string{"CLAUDE.md": "c\n"}, PointerPlan{Append: []string{"CLAUDE.md"}}},
		{".claude/CLAUDE.md only", map[string]string{".claude/CLAUDE.md": "c\n"}, PointerPlan{Append: []string{".claude/CLAUDE.md"}}},
		{"import @AGENTS.md", map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "x\n@AGENTS.md\n"}, PointerPlan{Append: []string{"AGENTS.md"}}},
		{"import @./AGENTS.md", map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "@./AGENTS.md\n"}, PointerPlan{Append: []string{"AGENTS.md"}}},
		{"import in .claude/CLAUDE.md", map[string]string{"AGENTS.md": "a\n", ".claude/CLAUDE.md": "@AGENTS.md"}, PointerPlan{Append: []string{"AGENTS.md"}}},
		{"no import", map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "c\n"}, PointerPlan{Append: []string{"AGENTS.md", "CLAUDE.md"}}},
		{"an import that is not a whole line", map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "see @AGENTS.md\n"}, PointerPlan{Append: []string{"AGENTS.md", "CLAUDE.md"}}},
		{"neither", map[string]string{}, PointerPlan{Create: []string{"AGENTS.md"}}},
		{"AGENTS.md already points", map[string]string{"AGENTS.md": mention, "CLAUDE.md": "c\n"}, PointerPlan{Append: []string{"CLAUDE.md"}}},
		{"every file already points", map[string]string{"AGENTS.md": mention, "CLAUDE.md": mention}, PointerPlan{}},
		{"lone AGENTS.md already points", map[string]string{"AGENTS.md": mention}, PointerPlan{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PlanPointer(tt.files); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("PlanPointer = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyPointerAppendsTheLineAndCreatesTheFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := PointerPlan{Append: []string{"AGENTS.md"}, Create: []string{"CLAUDE.md"}}

	if err := ApplyPointer(At(root), plan); err != nil {
		t.Fatal(err)
	}

	line := "This repo's tasks are driven by smith: read `.smith/README.md` before running smith.\n"
	for path, want := range map[string]string{"AGENTS.md": "rules\n" + line, "CLAUDE.md": line} {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
