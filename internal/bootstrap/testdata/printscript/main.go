// Package main prints bootstrap.Script, so a test can observe what the
// package assembles at init from parts and an order list it overlays.
package main

import (
	"fmt"

	"github.com/byranZA/smith/internal/bootstrap"
)

// main writes the assembled script to stdout.
func main() {
	fmt.Print(bootstrap.Script)
}
