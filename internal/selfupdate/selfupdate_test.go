package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byranZA/smith/internal/release"
)

// fakeGetter serves canned bodies by URL in place of GitHub, and records each
// URL it was asked for.
type fakeGetter struct {
	bodies map[string][]byte
	asked  []string
}

func (f *fakeGetter) Get(_ context.Context, url string) ([]byte, error) {
	f.asked = append(f.asked, url)
	body, ok := f.bodies[url]
	if !ok {
		return nil, fmt.Errorf("GET %s: 404 Not Found", url)
	}
	return body, nil
}

func TestLatestReportsTheNewestReleaseWithoutTheTagPrefix(t *testing.T) {
	t.Parallel()
	g := &fakeGetter{bodies: map[string][]byte{
		release.LatestURL: []byte(`{"tag_name":"v0.3.1","name":"v0.3.1","draft":false}`),
	}}

	got, err := Latest(t.Context(), g)

	if err != nil {
		t.Fatalf("Latest() errored: %v", err)
	}
	if got != "0.3.1" {
		t.Errorf("Latest() = %q, want %q", got, "0.3.1")
	}
}

// tarball is a gzipped tar archive holding the given files, the shape the
// release pipeline publishes.
func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar entry %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

// checksumLine is the line checksums.txt carries for one archive.
func checksumLine(archive []byte, name string) string {
	sum := sha256.Sum256(archive)
	return hex.EncodeToString(sum[:]) + "  " + name + "\n"
}

// publishedRelease is a getter serving the 0.3.0 darwin/arm64 archive holding
// binary, and a checksums.txt that covers it truthfully.
func publishedRelease(t *testing.T, binary string) (*fakeGetter, release.Asset) {
	t.Helper()
	asset, err := release.ForPlatform("0.3.0", "darwin", "arm64")
	if err != nil {
		t.Fatalf("ForPlatform: %v", err)
	}
	archive := tarball(t, map[string]string{"LICENSE": "MIT", "README.md": "readme", "smith": binary})
	return &fakeGetter{bodies: map[string][]byte{
		asset.URL:          archive,
		asset.ChecksumsURL: []byte("0000000000000000000000000000000000000000000000000000000000000000  smith_0.3.0_linux_amd64.tar.gz\n" + checksumLine(archive, asset.Name)),
	}}, asset
}

// installedBinary writes an old smith binary into a fresh directory and
// returns its path.
func installedBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "smith")
	if err := os.WriteFile(path, []byte("old smith"), 0o755); err != nil {
		t.Fatalf("write old binary: %v", err)
	}
	return path
}

// readFile returns the content of path, failing the test when it cannot.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestInstallReplacesTheBinaryWithTheReleasedOne(t *testing.T) {
	t.Parallel()
	g, asset := publishedRelease(t, "new smith")
	path := installedBinary(t)

	if _, err := Install(t.Context(), g, asset, path); err != nil {
		t.Fatalf("Install() errored: %v", err)
	}

	if got := readFile(t, path); got != "new smith" {
		t.Errorf("binary at %s = %q, want %q", path, got, "new smith")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("binary mode = %v, want it executable", info.Mode())
	}
}

func TestInstallAbortsOnAChecksumMismatchLeavingTheOldBinary(t *testing.T) {
	t.Parallel()
	g, asset := publishedRelease(t, "new smith")
	g.bodies[asset.URL] = tarball(t, map[string]string{"smith": "tampered smith"})
	path := installedBinary(t)

	_, err := Install(t.Context(), g, asset, path)

	var mismatch *ChecksumError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Install() error = %v, want a *ChecksumError", err)
	}
	if got := readFile(t, path); got != "old smith" {
		t.Errorf("binary at %s = %q, want the old %q", path, got, "old smith")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("install dir holds %d entries, want only the old binary", len(entries))
	}
}

func TestInstallRefusesAnArchiveTheChecksumsDoNotCover(t *testing.T) {
	t.Parallel()
	g, asset := publishedRelease(t, "new smith")
	g.bodies[asset.ChecksumsURL] = []byte("0000000000000000000000000000000000000000000000000000000000000000  smith_0.3.0_linux_amd64.tar.gz\n")
	path := installedBinary(t)

	_, err := Install(t.Context(), g, asset, path)

	var mismatch *ChecksumError
	if !errors.As(err, &mismatch) {
		t.Fatalf("Install() error = %v, want a *ChecksumError", err)
	}
	if got := readFile(t, path); got != "old smith" {
		t.Errorf("binary at %s = %q, want the old %q", path, got, "old smith")
	}
}

func TestInstallIntoAnUnwritableDirectoryNamesThePathAndChangesNothing(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes through directory permissions")
	}
	g, asset := publishedRelease(t, "new smith")
	path := installedBinary(t)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := Install(t.Context(), g, asset, path)

	var unwritable *UnwritableError
	if !errors.As(err, &unwritable) {
		t.Fatalf("Install() error = %v, want an *UnwritableError", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the directory %s", err, dir)
	}
	if got := readFile(t, path); got != "old smith" {
		t.Errorf("binary at %s = %q, want the old %q", path, got, "old smith")
	}
}

func TestInstallThroughASymlinkReplacesItsTargetAndKeepsTheLink(t *testing.T) {
	t.Parallel()
	g, asset := publishedRelease(t, "new smith")
	target := installedBinary(t)
	link := filepath.Join(t.TempDir(), "smith")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	replaced, err := Install(t.Context(), g, asset, link)
	if err != nil {
		t.Fatalf("Install() errored: %v", err)
	}

	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve %s: %v", target, err)
	}
	if replaced != resolvedTarget {
		t.Errorf("Install() replaced %q, want the link's target %q", replaced, resolvedTarget)
	}
	if got := readFile(t, target); got != "new smith" {
		t.Errorf("binary at the link's target = %q, want %q", got, "new smith")
	}
	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("Lstat(%s) = %v, %v; want the symlink left in place", link, info, err)
	}
}
