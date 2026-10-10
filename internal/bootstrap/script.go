package bootstrap

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// scriptParts holds the shipped script's source parts, one concern per file.
//
//go:embed script/*.sh
var scriptParts embed.FS

// scriptOrder is the order the parts are joined in. Only the first (the
// shebang and set) and the last (the call that runs main) depend on their
// place; every part between declares globals and functions and runs nothing.
var scriptOrder = []string{
	"core.sh",
	"preflight.sh",
	"apt.sh",
	"phases-system.sh",
	"phases-identity.sh",
	"ssh-hardening.sh",
	"access.sh",
	"setup.sh",
	"probe.sh",
	"main.sh",
}

// Script is the shipped bootstrap.sh, scp'd to the box and run there: the
// embedded parts under script/ joined in scriptOrder.
var Script = mustJoinScriptParts(scriptParts, scriptOrder)

// mustJoinScriptParts returns the .sh parts under script/ joined in order, a
// blank line apart. It panics, naming the part, unless order lists each part
// on disk exactly once.
func mustJoinScriptParts(parts fs.FS, order []string) string {
	script, err := joinScriptParts(parts, order)
	if err != nil {
		panic(fmt.Sprintf("bootstrap: assemble the embedded script: %v", err))
	}
	return script
}

// joinScriptParts returns the .sh parts under script/ joined in order, a blank
// line apart, or an error naming the first part that order does not list
// exactly once.
func joinScriptParts(parts fs.FS, order []string) (string, error) {
	onDisk, err := fs.Glob(parts, "script/*.sh")
	if err != nil {
		return "", fmt.Errorf("list the parts: %w", err)
	}
	listed := make(map[string]bool, len(order))
	for _, name := range order {
		if listed[name] {
			return "", fmt.Errorf("part %s is listed twice", name)
		}
		listed[name] = true
	}
	for _, p := range onDisk {
		if name := path.Base(p); !listed[name] {
			return "", fmt.Errorf("part %s is not in the ordered list", name)
		}
	}
	bodies := make([]string, 0, len(order))
	for _, name := range order {
		body, err := fs.ReadFile(parts, path.Join("script", name))
		if err != nil {
			return "", fmt.Errorf("listed part %s: %w", name, err)
		}
		bodies = append(bodies, string(body))
	}
	return strings.Join(bodies, "\n"), nil
}
