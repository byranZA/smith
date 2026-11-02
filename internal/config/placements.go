package config

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/byranZA/smith/internal/blueprint"
)

// Declared is a placement together with the blueprint file that declares it.
type Declared struct {
	// Blueprint is the file the placement is declared in.
	Blueprint string
	// Placement is the declared placement.
	Placement blueprint.Placement
}

// PlacementsFor returns the placements every blueprint in the config home declares for the repo with clone URL url, joined by destination path in first-declared order, with converge winning a mode disagreement. A blueprint that cannot be read or parsed is skipped, with a "smith: warning:" line naming it written to warn.
func PlacementsFor(home Home, url string, warn io.Writer) ([]Declared, error) {
	dir := filepath.Join(home.path, "blueprints")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list blueprints in %s: %w", dir, err)
	}
	var out []Declared
	at := map[string]int{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != blueprintExt {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		var b blueprint.Blueprint
		if err == nil {
			b, err = blueprint.Parse(data)
		}
		if err != nil {
			_, _ = fmt.Fprintf(warn, "smith: warning: skipping blueprint %s, which cannot be read: %v\n", path, err)
			continue
		}
		for _, r := range b.Repos {
			if !blueprint.SameRepo(r.URL, url) {
				continue
			}
			for _, p := range r.Placements {
				d := Declared{Blueprint: path, Placement: p}
				i, seen := at[p.To]
				switch {
				case !seen:
					at[p.To] = len(out)
					out = append(out, d)
				case p.Mode == "converge" && out[i].Placement.Mode != "converge":
					out[i] = d
				}
			}
		}
	}
	return out, nil
}
