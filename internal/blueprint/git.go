package blueprint

import "fmt"

// gitconfigPath is the file git reads a box-wide identity from. Git reads it
// or ~/.config/git/config and never both, so this is the one path the
// identity fields and a box placement can collide on.
const gitconfigPath = "~/.gitconfig"

// GitConflicts refuses an identity and a box placement that would both write
// ~/.gitconfig. Identity fields mean smith writes that file itself, so a box
// placement to it would land on top of what smith wrote, in whichever order
// the two happened to run. Only the operator knows which they meant, so the
// pair is refused rather than resolved.
//
// It takes the identity and the placements rather than a whole document,
// because the two writers need not be declared in the same file: git is
// preference-settable and placements are blueprint-only, so the pair smith
// would actually apply is only visible once the precedence chain has run.
func GitConflicts(identity Git, placements []Placement) []Finding {
	if identity == (Git{}) {
		return nil
	}
	for _, p := range placements {
		if p.To != gitconfigPath {
			continue
		}
		return []Finding{{Path: "git", Message: fmt.Sprintf(
			"git identity fields and a box placement to %q are mutually exclusive, because both write that file: choose which one does, and drop the other",
			gitconfigPath)}}
	}
	return nil
}
