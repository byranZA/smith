package bootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptJoinsThePartsInListedOrderABlankLineApart(t *testing.T) {
	t.Parallel()
	parts := map[string]string{
		"script/core.sh":            "PHASES=(access)\n",
		"script/preflight.sh":       "# preflight\n",
		"script/apt.sh":             "APT_LOCK_TIMEOUT=9\nAPT_LOCK_OPTS=(-o \"T=${APT_LOCK_TIMEOUT}\")\n",
		"script/phases-system.sh":   "# phases-system\n",
		"script/phases-identity.sh": "# phases-identity\n",
		"script/ssh-hardening.sh":   "# ssh-hardening\n",
		"script/access.sh":          "# access\n",
		"script/setup.sh":           "# setup\n",
		"script/probe.sh":           "# probe\n",
		"script/main.sh":            "main \"$@\"\n",
	}

	stdout, stderr, err := runAssembled(t, parts)
	if err != nil {
		t.Fatalf("assembling the fixture parts failed: %v\n%s", err, stderr)
	}

	want := "PHASES=(access)\n\n# preflight\n\n" +
		"APT_LOCK_TIMEOUT=9\nAPT_LOCK_OPTS=(-o \"T=${APT_LOCK_TIMEOUT}\")\n\n" +
		"# phases-system\n\n# phases-identity\n\n# ssh-hardening\n\n# access\n\n" +
		"# setup\n\n# probe\n\nmain \"$@\"\n"
	if stdout != want {
		t.Errorf("Script = %q, want %q", stdout, want)
	}
}

func TestScriptPartsDisagreeingWithTheListPanicAtInit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		overlay map[string]string
		part    string
	}{
		{
			name:    "part on disk left out of the list",
			overlay: map[string]string{"script.go": withOrderEdited(t, "\t\"probe.sh\",\n", "")},
			part:    "probe.sh",
		},
		{
			name:    "unlisted part added to script/",
			overlay: map[string]string{"script/extra.sh": "echo extra\n"},
			part:    "extra.sh",
		},
		{
			name:    "listed part missing from disk",
			overlay: map[string]string{"script/probe.sh": removed},
			part:    "probe.sh",
		},
		{
			name:    "part listed twice",
			overlay: map[string]string{"script.go": withOrderEdited(t, "\t\"main.sh\",\n", "\t\"main.sh\",\n\t\"core.sh\",\n")},
			part:    "core.sh",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, stderr, err := runAssembled(t, tt.overlay)

			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatalf("assembly exited with %v, want a panic at init\n%s", err, stderr)
			}
			if !strings.Contains(stderr, "panic: bootstrap: assemble the embedded script") || !strings.Contains(stderr, tt.part) {
				t.Errorf("stderr = %q, want an assembly panic naming %s", stderr, tt.part)
			}
		})
	}
}

func TestScriptIsFramedByTheShebangAndTheMainCall(t *testing.T) {
	t.Parallel()
	if !strings.HasPrefix(Script, "#!/usr/bin/env bash\n") {
		t.Errorf("Script does not open with the shebang: %q", Script[:min(len(Script), 40)])
	}
	if !strings.HasSuffix(Script, "\nmain \"$@\"\n") {
		t.Errorf("Script does not end by running main: %q", Script[max(0, len(Script)-40):])
	}
}

// removed marks an overlay entry whose file the fixture build deletes.
const removed = "\x00removed"

// withOrderEdited returns script.go's source with old in its order list replaced by new.
func withOrderEdited(t *testing.T, old, new string) string {
	t.Helper()
	src, err := os.ReadFile("script.go")
	if err != nil {
		t.Fatalf("read script.go: %v", err)
	}
	if !bytes.Contains(src, []byte(old)) {
		t.Fatalf("script.go has no %q to edit", old)
	}
	return strings.Replace(string(src), old, new, 1)
}

// runAssembled runs testdata/printscript against this package with overlay applied.
func runAssembled(t *testing.T, overlay map[string]string) (stdout, stderr string, err error) {
	t.Helper()
	pkg, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	replace := make(map[string]string, len(overlay))
	for rel, content := range overlay {
		if content == removed {
			replace[filepath.Join(pkg, rel)] = ""
			continue
		}
		file := filepath.Join(dir, strings.ReplaceAll(rel, "/", "_"))
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatalf("write overlay file: %v", err)
		}
		replace[filepath.Join(pkg, rel)] = file
	}
	spec, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		t.Fatalf("encode overlay: %v", err)
	}
	specPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(specPath, spec, 0o644); err != nil {
		t.Fatalf("write overlay: %v", err)
	}

	var out, errOut bytes.Buffer
	cmd := exec.Command("go", "run", "-overlay", specPath, "./testdata/printscript")
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}
