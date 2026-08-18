// PROTOTYPE — name selection. #58 selects a blueprint by bare name
// (`smith machine setup dev --blueprint acme`). This file asks what
// "by bare name" means once files have extensions, and what happens
// when a directory-per-blueprint sits alongside a file-per-blueprint.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolve mirrors the obvious implementation: try each known extension in turn.
func resolve(root, name string) (string, []string) {
	var hits []string
	for _, cand := range []string{
		filepath.Join(root, name+".yaml"),
		filepath.Join(root, name+".yml"),
		filepath.Join(root, name+".toml"),
		filepath.Join(root, name, "blueprint.yaml"),
		filepath.Join(root, name, "blueprint.toml"),
		filepath.Join(root, name), // extensionless
	} {
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			hits = append(hits, cand)
		}
	}
	if len(hits) == 0 {
		return "", nil
	}
	return hits[0], hits
}

func resolveDemo(root string, names []string) {
	for _, n := range names {
		pick, hits := resolve(root, n)
		switch {
		case pick == "":
			fmt.Printf("  %-10s → not found\n", n)
		case len(hits) > 1:
			fmt.Printf("  %-10s → %s   ** AMBIGUOUS: also %v **\n", n, pick, hits[1:])
		default:
			fmt.Printf("  %-10s → %s\n", n, pick)
		}
	}
}
