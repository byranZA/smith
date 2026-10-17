package repofile

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
)

// Commit is the repo file and the ejected loop prompt as one commit of a repo
// holds them.
type Commit struct {
	// File is the repo file, empty when the commit has none.
	File File
	// Prompt is the ejected loop prompt, meaningful only when Ejected.
	Prompt []byte
	// Ejected reports whether the commit holds an ejected loop prompt.
	Ejected bool
}

// Committed reads the repo file and the ejected loop prompt as ref holds them
// in the git repo at gitDir, bare or not, with no checkout. Either may be
// absent; a ref the repo lacks and a repo file that does not parse are refused.
func Committed(ctx context.Context, git Runner, gitDir, ref string) (Commit, error) {
	filePath, promptPath := dirName+"/"+fileName, dirName+"/"+promptName
	listed, err := gitOut(ctx, git, gitDir, "ls-tree", "--name-only", ref, "--", filePath, promptPath)
	if err != nil {
		return Commit{}, fmt.Errorf("read the repo file at %s in %s: %w", ref, gitDir, err)
	}
	present := strings.Fields(string(listed))
	var c Commit
	if slices.Contains(present, filePath) {
		data, err := gitOut(ctx, git, gitDir, "show", ref+":"+filePath)
		if err != nil {
			return Commit{}, fmt.Errorf("read the repo file at %s in %s: %w", ref, gitDir, err)
		}
		if c.File, err = Parse(data); err != nil {
			return Commit{}, fmt.Errorf("%s:%s in %s: %w", ref, filePath, gitDir, err)
		}
	}
	if slices.Contains(present, promptPath) {
		if c.Prompt, err = gitOut(ctx, git, gitDir, "show", ref+":"+promptPath); err != nil {
			return Commit{}, fmt.Errorf("read the loop prompt at %s in %s: %w", ref, gitDir, err)
		}
		c.Ejected = true
	}
	return c, nil
}

// gitOut runs git with args in the repo at gitDir and returns its stdout, with
// what git said on stderr carried into the error.
func gitOut(ctx context.Context, git Runner, gitDir string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	if err := git.Run(ctx, "git", append([]string{"-C", gitDir}, args...), nil, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
