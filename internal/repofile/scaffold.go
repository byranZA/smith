package repofile

import (
	"bytes"
	_ "embed"
	"fmt"
	"io/fs"

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

// Outcome is what Scaffold did: the repo file's path and whether the starter
// was written there or an existing file was left alone.
type Outcome struct {
	// Path is where the repo file is.
	Path string
	// Created reports whether the starter was written. False means a repo file
	// was already there and was left untouched.
	Created bool
}

// Scaffold writes the commented starter repo file into repo unless one is
// already there, which is never overwritten. Each value set in values is
// written uncommented in place of its placeholder; the rest stay commented,
// so the starter as written leaves every field unset.
func Scaffold(repo Repo, values File) (Outcome, error) {
	content, err := render(values)
	if err != nil {
		return Outcome{}, err
	}
	created, err := starter.WriteAbsent(repo.Path(), content, dirMode, fileMode)
	if err != nil {
		return Outcome{}, fmt.Errorf("scaffold repo file: %w", err)
	}
	return Outcome{Path: repo.Path(), Created: created}, nil
}

// render returns the starter with each value set in values uncommented in
// place of its placeholder line.
func render(values File) ([]byte, error) {
	content := starterFile
	for _, field := range []struct{ key, value, placeholder string }{
		{"agent", values.Agent, "claude"},
		{"model", values.Model, "opus"},
		{"effort", values.Effort, "high"},
	} {
		if field.value == "" {
			continue
		}
		commented := []byte(fmt.Sprintf("# %s: %s\n", field.key, field.placeholder))
		if !bytes.Contains(content, commented) {
			return nil, fmt.Errorf("render repo file %s: the starter carries no placeholder for it", field.key)
		}
		scalar, err := yaml.Marshal(field.value)
		if err != nil {
			return nil, fmt.Errorf("render repo file %s: %w", field.key, err)
		}
		content = bytes.Replace(content, commented, append([]byte(field.key+": "), scalar...), 1)
	}
	return content, nil
}
