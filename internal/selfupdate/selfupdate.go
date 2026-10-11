// Package selfupdate replaces the running smith binary with a published
// release: it finds the newest release, downloads a platform's archive,
// verifies it against the release's checksums and swaps it in atomically.
//
// It is the only code in local smith that reaches GitHub, and it does so
// through a Getter, so every caller and test decides what the network is.
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/byranZA/smith/internal/release"
)

// Getter fetches the body a URL serves. A response other than success is an
// error.
type Getter interface {
	Get(ctx context.Context, url string) ([]byte, error)
}

// Latest returns the version of the newest published release, without the
// tag's "v", from one call to GitHub's latest-release endpoint.
func Latest(ctx context.Context, g Getter) (string, error) {
	body, err := g.Get(ctx, release.LatestURL)
	if err != nil {
		return "", fmt.Errorf("look up the latest smith release: %w", err)
	}
	var latest struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &latest); err != nil {
		return "", fmt.Errorf("decode the latest smith release: %w", err)
	}
	version := release.Installable(latest.Tag)
	if version == "" {
		return "", fmt.Errorf("the latest smith release names no version: tag %q", latest.Tag)
	}
	return version, nil
}

// binaryName is the name of the smith binary inside a release archive.
const binaryName = "smith"

// Install downloads asset, verifies it and replaces the binary at path with
// the one it carries, returning the path of the file it replaced. A symlink at
// path is followed, so the file it points to is replaced and the link is left
// in place. Every failure leaves the old binary where it was.
func Install(ctx context.Context, g Getter, asset release.Asset, path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve the smith binary %s: %w", path, err)
	}
	archive, err := g.Get(ctx, asset.URL)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	checksums, err := g.Get(ctx, asset.ChecksumsURL)
	if err != nil {
		return "", fmt.Errorf("download checksums.txt: %w", err)
	}
	if err := verify(archive, asset.Name, checksums); err != nil {
		return "", err
	}
	binary, err := extract(archive)
	if err != nil {
		return "", fmt.Errorf("unpack %s: %w", asset.Name, err)
	}
	if err := replace(target, binary); err != nil {
		return "", err
	}
	return target, nil
}

// verify checks archive against the digest checksums.txt publishes for name,
// returning a *ChecksumError when they differ or no line names it.
func verify(archive []byte, name string, checksums []byte) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != name {
			continue
		}
		if fields[0] != got {
			return &ChecksumError{Asset: name, Want: fields[0], Got: got}
		}
		return nil
	}
	return &ChecksumError{Asset: name, Got: got}
}

// extract returns the smith binary a gzipped tar archive carries.
func extract(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s binary in the archive", binaryName)
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != binaryName {
			continue
		}
		binary, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", hdr.Name, err)
		}
		return binary, nil
	}
}

// replace writes binary to a temporary file beside target, makes it executable
// and renames it over target, so target is either the old binary or the new
// one and never a partial write.
func replace(target string, binary []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".smith-update-*")
	if errors.Is(err, fs.ErrPermission) {
		return &UnwritableError{Binary: target, Dir: dir}
	}
	if err != nil {
		return fmt.Errorf("write beside %s: %w", target, err)
	}
	if err := writeExecutable(tmp, binary); err != nil {
		return errors.Join(err, removeTemp(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return errors.Join(fmt.Errorf("replace %s: %w", target, err), removeTemp(tmp.Name()))
	}
	return nil
}

// writeExecutable writes binary to f, closes it and marks it executable.
func writeExecutable(f *os.File, binary []byte) error {
	if _, err := f.Write(binary); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", f.Name(), err), f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.Name(), err)
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil { // #nosec G302 -- a binary on PATH must be executable by its users.
		return fmt.Errorf("make %s executable: %w", f.Name(), err)
	}
	return nil
}

// removeTemp removes a temporary file an install abandoned.
func removeTemp(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// ChecksumError reports a downloaded archive the release's checksums.txt does
// not vouch for: its digest differs from the published one, or no line names
// it. The download is abandoned with no retry.
type ChecksumError struct {
	// Asset is the archive's file name.
	Asset string
	// Want is the digest checksums.txt publishes, empty when no line names it.
	Want string
	// Got is the digest of what was downloaded.
	Got string
}

// Error implements error.
func (e *ChecksumError) Error() string {
	if e.Want == "" {
		return fmt.Sprintf("checksums.txt does not cover %s, so it cannot be verified", e.Asset)
	}
	return fmt.Sprintf("checksum mismatch for %s: published %s, downloaded %s", e.Asset, e.Want, e.Got)
}

// UnwritableError reports an install directory this user cannot write to, such
// as a root-owned /usr/local/bin. smith never escalates to sudo itself, so the
// fix is the operator's.
type UnwritableError struct {
	// Binary is the smith binary that would have been replaced.
	Binary string
	// Dir is the directory it lives in.
	Dir string
}

// Error implements error.
func (e *UnwritableError) Error() string {
	return fmt.Sprintf(`cannot replace %s: you cannot write to %s.

smith does not escalate to sudo. Either make the directory yours:
  sudo chown "$USER" %s
or reinstall smith into a directory you own that is on your PATH, such as
~/.local/bin, and run smith update from there`, e.Binary, e.Dir, e.Dir)
}
