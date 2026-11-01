package misetrust_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// reportingMise is a stand-in mise that prints the config paths it was told to trust, then each argument in brackets.
const reportingMise = `#!/bin/sh
printf '%s|' "$MISE_TRUSTED_CONFIG_PATHS"
for arg; do printf '[%s]' "$arg"; done
`

// mise is the path of the stand-in mise, written once before any test runs: a test writing its own while another starts a process races on Linux's ETXTBSY (golang/go#22315).
var mise string

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

// runWithMise writes the stand-in mise into dir, then runs the tests.
func runWithMise(m *testing.M, dir string) int {
	mise = filepath.Join(dir, "mise")
	if err := os.WriteFile(mise, []byte(reportingMise), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}
