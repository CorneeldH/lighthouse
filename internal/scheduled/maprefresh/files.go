package maprefresh

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// The agent may have left symlinks in the directories it writes. Nothing here ever follows one:
// it looks with Lstat, removes with os.Remove (which unlinks a link, never its target) and creates
// files with O_EXCL after removing what was there.

// cause is the error without the raw absolute path a *fs.PathError carries.
func cause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// fileErr is an error about rel, a path relative to the work tree, for a run reason.
func fileErr(rel string, err error) error {
	return fmt.Errorf("%s: %w", scheduled.ShowPath(rel), cause(err))
}

// stat looks at rel (slash form, relative to dir) without following a symlink in any component:
// a symlink on the way is an error. A missing component gives fs.ErrNotExist.
func stat(dir, rel string) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	var info fs.FileInfo
	for i := range parts {
		var err error
		info, err = os.Lstat(filepath.Join(dir, filepath.FromSlash(strings.Join(parts[:i+1], "/"))))
		if err != nil {
			return nil, fileErr(rel, err)
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return nil, fmt.Errorf("%s: is a symlink", scheduled.ShowPath(strings.Join(parts[:i+1], "/")))
		case i < len(parts)-1 && !info.IsDir():
			return nil, fmt.Errorf("%s: is not a directory", scheduled.ShowPath(strings.Join(parts[:i+1], "/")))
		}
	}
	return info, nil
}

// unlinkSymlinks removes every symlink at or under rel (relative to dir): a symlink among the
// components of rel, or inside the directory it names. A link is unlinked, its target is left
// alone; the walk does not follow links.
func unlinkSymlinks(dir, rel string) error {
	parts := strings.Split(rel, "/")
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		full := filepath.Join(dir, filepath.FromSlash(sub))
		info, err := os.Lstat(full)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return fileErr(sub, err)
		case info.Mode()&fs.ModeSymlink != 0:
			if err := os.Remove(full); err != nil {
				return fileErr(sub, err)
			}
			return nil
		case !info.IsDir():
			return nil
		}
	}
	root := filepath.Join(dir, filepath.FromSlash(rel))
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fileErr(rel, err)
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return fileErr(rel, err)
		}
		return nil
	})
}

// writeFresh writes data to full (inside dir, at rel) as a new regular file: whatever file or
// link is there goes first, and the create is O_EXCL, so the write cannot land through a link.
func writeFresh(full, rel string, data []byte) error {
	if info, err := os.Lstat(full); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s: is a directory", scheduled.ShowPath(rel))
		}
		if err := os.Remove(full); err != nil {
			return fileErr(rel, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fileErr(rel, err)
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fileErr(rel, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fileErr(rel, err)
	}
	if err := f.Close(); err != nil {
		return fileErr(rel, err)
	}
	return nil
}
