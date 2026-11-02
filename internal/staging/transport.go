package staging

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// Bundle is the staged tree as it crosses to the box: the directories and the
// files, each with the mode and owner it is to carry, and nothing else. It is
// what the box decodes the relay's stream into, and what Apply writes.
type Bundle struct {
	// Dirs are the directories to ensure, outermost first.
	Dirs []Dir
	// Files are the files to write, in the order they are written.
	Files []File
}

// BundleOf is the tree as the box applies it: its directories, then the
// document, the env, the resolution and each placement, in that order.
func BundleOf(tree Tree) Bundle {
	files := make([]File, 0, 3+len(tree.Placements))
	files = append(files, tree.Document, tree.Env.File, tree.Resolution.File)
	for _, p := range tree.Placements {
		files = append(files, p.File)
	}
	return Bundle{Dirs: tree.Dirs, Files: files}
}

// Encode writes the tree as one tar stream of a directory entry per Dir and a file entry per File. The stream is transport only and carries no compatibility promise.
func Encode(tree Tree) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	b := BundleOf(tree)
	for _, d := range b.Dirs {
		h, err := header(tar.TypeDir, d.Path, d.Mode, d.Owner, 0)
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, fmt.Errorf("encode %s: %w", d.Path, err)
		}
	}
	for _, f := range b.Files {
		h, err := header(tar.TypeReg, f.Path, f.Mode, f.Owner, int64(len(f.Bytes)))
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(h); err != nil {
			return nil, fmt.Errorf("encode %s: %w", f.Path, err)
		}
		if _, err := tw.Write(f.Bytes); err != nil {
			return nil, fmt.Errorf("encode %s: %w", f.Path, err)
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("encode the staged tree: %w", err)
	}
	return buf.Bytes(), nil
}

// header builds the tar header for one entry, parsing the octal mode and
// splitting the owner into the user and group names tar carries.
func header(kind byte, path, mode, owner string, size int64) (*tar.Header, error) {
	m, err := strconv.ParseInt(mode, 8, 32)
	if err != nil {
		return nil, fmt.Errorf("encode %s: mode %q is not octal", path, mode)
	}
	user, group, _ := strings.Cut(owner, ":")
	return &tar.Header{Typeflag: kind, Name: path, Mode: m, Uname: user, Gname: group, Size: size}, nil
}

// Decode reads a stream Encode wrote and checks every entry before returning any of it. A name outside /etc/smith/, an unclean name, or an entry that is not a plain directory or regular file refuses the whole stream.
func Decode(r io.Reader) (Bundle, error) {
	var b Bundle
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return b, nil
		}
		if err != nil {
			return Bundle{}, fmt.Errorf("decode the staged tree: %w", err)
		}
		name, err := checkedName(h)
		if err != nil {
			return Bundle{}, err
		}
		mode := fmt.Sprintf("%04o", h.Mode&0o7777)
		owner := h.Uname + ":" + h.Gname
		switch h.Typeflag {
		case tar.TypeDir:
			b.Dirs = append(b.Dirs, Dir{Path: name, Mode: mode, Owner: owner})
		case tar.TypeReg:
			data, err := io.ReadAll(tr)
			if err != nil {
				return Bundle{}, fmt.Errorf("decode %s: %w", name, err)
			}
			b.Files = append(b.Files, File{Path: name, Bytes: data, Mode: mode, Owner: owner})
		default:
			return Bundle{}, fmt.Errorf("refusing %s: entry type %q is neither a directory nor a regular file", h.Name, h.Typeflag)
		}
	}
}

// checkedName is the entry's path once it is known to sit inside Root.
func checkedName(h *tar.Header) (string, error) {
	switch h.Typeflag {
	case tar.TypeSymlink, tar.TypeLink:
		return "", fmt.Errorf("refusing %s: a symlink or hardlink entry", h.Name)
	}
	for _, elem := range strings.Split(h.Name, "/") {
		if elem == ".." {
			return "", fmt.Errorf("refusing %s: it has a .. element", h.Name)
		}
	}
	name := strings.TrimSuffix(h.Name, "/")
	if name != path.Clean(name) || (name != Root && !strings.HasPrefix(name, Root+"/")) {
		return "", fmt.Errorf("refusing %s: it is not a clean path inside %s/", h.Name, Root)
	}
	return name, nil
}
