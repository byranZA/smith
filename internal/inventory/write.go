package inventory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// fileMode is the permission the inventory file is written with. It is one
// operator's address book for their own boxes; no other account on the machine
// has business reading it.
const fileMode os.FileMode = 0o600

// ErrSkewNewer reports a write refused because the file on disk was written by
// a newer smith than this build understands. Overwriting it would drop the
// entries this build could not decode, so the file is left exactly as it is.
var ErrSkewNewer = errors.New("the inventory was written by a newer smith: upgrade smith, or the entries it holds would be lost")

// Write replaces the inventory at path with inv, stamped with the schema
// version this build writes. The caller passes the skew it read, because the
// decision the file's version drives belongs to the write, not the read: a file
// a newer smith owns is refused and left byte-identical rather than truncated
// down to what this build could decode.
//
// The write is atomic — a temp file in the same directory, renamed over the
// target — so an interrupted smith cannot leave the operator with a truncated
// address book and no way to rebuild it. The directory is expected to exist:
// creating the config home is config.EnsureHome's job, and every write path
// calls it first.
func Write(path string, inv Inventory, skew Skew) error {
	if skew == SkewNewer {
		return fmt.Errorf("refusing to write %s: %w", path, ErrSkewNewer)
	}
	inv.SchemaVersion = SchemaVersion
	data, err := Encode(inv)
	if err != nil {
		return err
	}
	return replace(path, append(data, '\n'))
}

// replace writes data over the file at path atomically: a temp file beside it,
// then a rename. A failure before the rename leaves the original untouched, and
// the temp file is removed rather than left for the operator to find.
func replace(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	name := tmp.Name()
	if err := writeAndClose(tmp, data); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", name, err), remove(name))
	}
	if err := os.Rename(name, path); err != nil {
		return errors.Join(fmt.Errorf("replace %s: %w", path, err), remove(name))
	}
	return nil
}

// writeAndClose writes data to f, gives it the inventory's permissions, and
// closes it.
func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Chmod(fileMode); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// remove deletes the abandoned temp file, reporting a failure that would leave
// it behind rather than swallowing it.
func remove(name string) error {
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("remove temporary file %s: %w", name, err)
	}
	return nil
}
