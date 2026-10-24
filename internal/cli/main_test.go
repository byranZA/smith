package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/byranZA/smith/internal/misetrust"
)

// standInMise runs the command after mise exec's "--" as it stands, as mise does with no config to apply.
const standInMise = `#!/bin/sh
while [ "$#" -gt 0 ] && [ "$1" != -- ]; do shift; done
shift
exec "$@"
`

// untrusted is argv with the trust misetrust.Command wraps mise in taken off, leaving the mise invocation the shell would go on to exec.
func untrusted(argv []string) []string {
	if len(argv) > 4 && argv[0] == "sh" && argv[1] == "-c" && argv[2] == misetrust.Script {
		return argv[4:]
	}
	return argv
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mise")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := runWithMise(m, dir)
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(code)
}

// runWithMise runs the tests with a stand-in mise in dir first on PATH, so a test driving real git through mise exec needs no mise on the machine.
func runWithMise(m *testing.M, dir string) int {
	if err := os.WriteFile(filepath.Join(dir, "mise"), []byte(standInMise), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}
