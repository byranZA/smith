// Command smith turns a fresh VPS into a provisioned, secured, reachable remote
// development machine and manages coding agents on it.
//
// main stays tiny: it delegates to the cli package and maps the run to a
// process exit code.
package main

import (
	"os"

	"github.com/byranZA/smith/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
