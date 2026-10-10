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

// mustJoinScriptParts joins the .sh parts under script/ in parts in the given
// order, a blank line apart, and panics unless the order lists each part on
// disk exactly once. The parts are compiled into the binary, so a mismatch is
// a build that cannot be correct rather than a runtime condition, and a panic
// surfaces it at package initialization.
func mustJoinScriptParts(parts fs.FS, order []string) string {
	script, err := joinScriptParts(parts, order)
	if err != nil {
		panic(fmt.Sprintf("bootstrap: assemble the embedded script: %v", err))
	}
	return script
}

// joinScriptParts is mustJoinScriptParts returning its error.
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
