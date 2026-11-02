package staging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/byranZA/smith/internal/blueprint"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPlaceFromCheckoutCopiesTheFileWithDeclaredPerms(t *testing.T) {
	checkout, tree := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(checkout, "config", ".env"), "A=1\n")

	_, err := PlaceFromCheckout(checkout, tree, []blueprint.Placement{{To: "config/.env", Mode: "converge", Perms: "0640"}})

	info, statErr := os.Lstat(filepath.Join(tree, "config", ".env"))
	if err != nil || statErr != nil || readFile(t, filepath.Join(tree, "config", ".env")) != "A=1\n" || info.Mode() != 0o640 {
		t.Errorf("err %v, stat %v, info %v; want a regular 0640 copy holding A=1", err, statErr, info)
	}
}

func TestPlaceFromCheckoutConvergeFollowsTheCheckout(t *testing.T) {
	checkout, tree := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(checkout, ".env"), "new\n")
	writeFile(t, filepath.Join(tree, ".env"), "old\n")

	_, err := PlaceFromCheckout(checkout, tree, []blueprint.Placement{{To: ".env", Mode: "converge"}})

	if got := readFile(t, filepath.Join(tree, ".env")); err != nil || got != "new\n" {
		t.Errorf("err %v, held %q; want new", err, got)
	}
}

func TestPlaceFromCheckoutOnceLeavesTheWorktreesFile(t *testing.T) {
	checkout, tree := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(checkout, ".env"), "new\n")
	writeFile(t, filepath.Join(tree, ".env"), "agent\n")

	_, err := PlaceFromCheckout(checkout, tree, []blueprint.Placement{{To: ".env", Mode: "once"}})

	if got := readFile(t, filepath.Join(tree, ".env")); err != nil || got != "agent\n" {
		t.Errorf("err %v, held %q; want agent", err, got)
	}
}

func TestPlaceFromCheckoutWritesAFileNotALink(t *testing.T) {
	checkout, tree := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(checkout, ".env"), "A=1\n")

	if _, err := PlaceFromCheckout(checkout, tree, []blueprint.Placement{{To: ".env", Mode: "converge"}}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree, ".env"), "changed\n")

	if got := readFile(t, filepath.Join(checkout, ".env")); got != "A=1\n" {
		t.Errorf("checkout holds %q after the copy was written; want A=1", got)
	}
}
