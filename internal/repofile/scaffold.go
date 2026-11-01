package repofile

import (
	"bytes"
	_ "embed"
	"fmt"
	"io/fs"
	"regexp"

	"go.yaml.in/yaml/v3"

	"github.com/byranZA/smith/internal/starter"
)

// dirMode and fileMode are the permissions the repo file and its directory are
// written with: both are committed with the repo, so both are readable to all.
const (
	dirMode  fs.FileMode = 0o755
	fileMode fs.FileMode = 0o644
)

//go:embed starter.yaml
var starterFile []byte

//go:embed guide.md
var guideFile []byte

// Outcome is what Scaffold did with one file: its path and whether it was
// written there or an existing file was left alone.
type Outcome struct {
	// Path is where the file is.
	Path string
	// Created reports whether the file was written. False means one was
	// already there and was left untouched.
	Created bool
}

// Scaffold writes a commented starter repo file into repo, with one preset
// filled from values and the work role on it, and the repo guide beside it.
// Each is written only if absent, and the outcomes name the repo file then the
// repo guide. It writes nothing when the starter would not load.
func Scaffold(repo Repo, values Preset) ([]Outcome, error) {
	content, err := render(values)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(content); err != nil {
		return nil, fmt.Errorf("scaffold repo file: %w", err)
	}
	files := []struct {
		path    string
		content []byte
	}{{repo.Path(), content}, {repo.GuidePath(), guideFile}}
	outcomes := make([]Outcome, 0, len(files))
	for _, f := range files {
		created, err := starter.WriteAbsent(f.path, f.content, dirMode, fileMode)
		if err != nil {
			return nil, fmt.Errorf("scaffold repo file: %w", err)
		}
		outcomes = append(outcomes, Outcome{Path: f.path, Created: created})
	}
	return outcomes, nil
}

// starterAgent is the agent the starter's preset names, and names it after.
const starterAgent = "claude"

// placeholder is one line of the starter that render fills in: the value to
// set, the line's text as the starter writes it, and the line to write in its
// place, with %s standing for the value as YAML.
type placeholder struct {
	key, value, text, line string
}

// render returns the starter with its preset filled in from values: the agent
// in place of starterAgent, in the preset's name, its agent and the work role,
// and each model or effort set uncommented in place of its placeholder line.
func render(values Preset) ([]byte, error) {
	content := starterFile
	placeholders := []placeholder{
		{"model", values.Model, "# model: opus", "    model: %s"},
		{"effort", values.Effort, "# effort: high", "    effort: %s"},
	}
	if values.Agent != starterAgent {
		placeholders = append(placeholders,
			placeholder{"preset", values.Agent, starterAgent + ":", "  %s:"},
			placeholder{"agent", values.Agent, "agent: " + starterAgent, "    agent: %s"},
			placeholder{WorkRole, values.Agent, WorkRole + ": " + starterAgent, "  " + WorkRole + ": %s"},
		)
	}
	for _, p := range placeholders {
		if p.value == "" {
			continue
		}
		line := regexp.MustCompile(`(?m)^ *` + regexp.QuoteMeta(p.text) + `$`)
		if !line.Match(content) {
			return nil, fmt.Errorf("render repo file %s: the starter carries no placeholder for it", p.key)
		}
		scalar, err := yaml.Marshal(p.value)
		if err != nil {
			return nil, fmt.Errorf("render repo file %s: %w", p.key, err)
		}
		content = line.ReplaceAllLiteral(content, fmt.Appendf(nil, p.line, bytes.TrimSuffix(scalar, []byte("\n"))))
	}
	return content, nil
}
