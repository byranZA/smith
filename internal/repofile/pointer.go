package repofile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/starter"
)

// PointerLine is the line `repo init` adds to an agent instruction file to
// point it at the repo guide.
const PointerLine = "This repo's tasks are driven by smith: read `.smith/README.md` before running smith."

// agentsFile and the claudeFiles are the instruction files, relative to the
// repo root, a pointer can go into. The claudeFiles are the two places Claude
// Code reads a project CLAUDE.md from.
const agentsFile = "AGENTS.md"

var claudeFiles = []string{"CLAUDE.md", filepath.Join(".claude", "CLAUDE.md")}

// guideMention is what marks an instruction file as already pointing at the
// repo guide.
const guideMention = ".smith/README.md"

// PointerPlan is what to do about the pointer line: the existing instruction
// files to append it to and the file to create to hold it. Paths are relative
// to the repo root. A zero plan means nothing needs adding.
type PointerPlan struct {
	// Append names the existing files the line goes at the end of.
	Append []string
	// Create names the file to create holding only the line.
	Create []string
}

// Empty reports whether the plan touches no file.
func (p PointerPlan) Empty() bool { return len(p.Append)+len(p.Create) == 0 }

// Files lists every file the plan touches, appended ones first.
func (p PointerPlan) Files() []string {
	return append(append([]string{}, p.Append...), p.Create...)
}

// PlanPointer decides where the pointer line goes, given the instruction files
// that exist at the repo root and their contents, keyed by path relative to
// the root. AGENTS.md alone gets it; a CLAUDE.md alone gets it; with both it
// goes into AGENTS.md, and into each CLAUDE.md that does not import it (a line
// that is exactly @AGENTS.md or @./AGENTS.md); with neither, a new AGENTS.md
// is created. A file already mentioning the repo guide is left out.
func PlanPointer(files map[string]string) PointerPlan {
	var claudes []string
	for _, p := range claudeFiles {
		if _, ok := files[p]; ok {
			claudes = append(claudes, p)
		}
	}
	_, hasAgents := files[agentsFile]
	if !hasAgents && len(claudes) == 0 {
		return PointerPlan{Create: []string{agentsFile}}
	}
	var targets []string
	if hasAgents {
		targets = append(targets, agentsFile)
	}
	for _, p := range claudes {
		if hasAgents && importsAgents(files[p]) {
			continue
		}
		targets = append(targets, p)
	}
	var plan PointerPlan
	for _, p := range targets {
		if !strings.Contains(files[p], guideMention) {
			plan.Append = append(plan.Append, p)
		}
	}
	return plan
}

// importsAgents reports whether a CLAUDE.md has a line that is exactly an
// import of AGENTS.md.
func importsAgents(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		switch strings.TrimRight(line, "\r") {
		case "@AGENTS.md", "@./AGENTS.md":
			return true
		}
	}
	return false
}

// ReadInstructions reads the agent instruction files that exist at the root of
// repo, keyed by path relative to it.
func ReadInstructions(repo Repo) (map[string]string, error) {
	files := map[string]string{}
	for _, p := range append([]string{agentsFile}, claudeFiles...) {
		b, err := os.ReadFile(filepath.Join(repo.root, p))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read instruction file %s: %w", p, err)
		}
		files[p] = string(b)
	}
	return files, nil
}

// ApplyPointer carries out plan: it appends PointerLine to each file in
// plan.Append, on a line of its own, and creates each file in plan.Create.
func ApplyPointer(repo Repo, plan PointerPlan) error {
	for _, p := range plan.Append {
		path := filepath.Join(repo.root, p)
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("point %s at the repo guide: %w", p, err)
		}
		sep := ""
		if len(b) > 0 && !strings.HasSuffix(string(b), "\n") {
			sep = "\n"
		}
		out := append(b, []byte(sep+PointerLine+"\n")...)
		if err := os.WriteFile(path, out, fileMode); err != nil {
			return fmt.Errorf("point %s at the repo guide: %w", p, err)
		}
	}
	for _, p := range plan.Create {
		if _, err := starter.WriteAbsent(filepath.Join(repo.root, p), []byte(PointerLine+"\n"), dirMode, fileMode); err != nil {
			return fmt.Errorf("point %s at the repo guide: %w", p, err)
		}
	}
	return nil
}
