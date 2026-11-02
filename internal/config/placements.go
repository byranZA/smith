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

// PlacementsFor returns the placements every blueprint in the config home
// declares for the repo whose clone URL is url, matching repo entries by URL
// alone. The placements are joined by destination path, each path once, in
// the order first declared; where entries disagree on a path's mode, the
// converge one wins. A home with no blueprints, or none declaring the repo,
// yields none. A blueprint that cannot be read or parsed is skipped, with one
// "smith: warning:" line naming it written to warn.
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
		data, err := os.ReadFile(path) // #nosec G304 -- a file listed in the operator's config home.
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
