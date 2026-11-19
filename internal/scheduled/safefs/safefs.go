// Package safefs holds the file operations of scheduled jobs that never follow a symlink the agent
// may have left in the work tree.
package safefs

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

// Cause is the error without the raw absolute path a *fs.PathError carries.
func Cause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// FileErr is an error about rel, a path relative to the work tree, for a run reason.
func FileErr(rel string, err error) error {
	return fmt.Errorf("%s: %w", scheduled.ShowPath(rel), Cause(err))
}

// Stat looks at rel (slash form, relative to dir) without following a symlink in any component:
// a symlink on the way is an error. A missing component gives fs.ErrNotExist.
func Stat(dir, rel string) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	var info fs.FileInfo
	for i := range parts {
		var err error
		info, err = os.Lstat(filepath.Join(dir, filepath.FromSlash(strings.Join(parts[:i+1], "/"))))
		if err != nil {
			return nil, FileErr(rel, err)
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

// UnlinkSymlinks removes every symlink at or under rel (relative to dir): a symlink among the
// components of rel, or inside the directory it names. A link is unlinked, its target is left
// alone; the walk does not follow links.
func UnlinkSymlinks(dir, rel string) error {
	parts := strings.Split(rel, "/")
	for i := range parts {
		sub := strings.Join(parts[:i+1], "/")
		full := filepath.Join(dir, filepath.FromSlash(sub))
		info, err := os.Lstat(full)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return FileErr(sub, err)
		case info.Mode()&fs.ModeSymlink != 0:
			if err := os.Remove(full); err != nil {
				return FileErr(sub, err)
			}
			return nil
		case !info.IsDir():
			return nil
		}
	}
	root := filepath.Join(dir, filepath.FromSlash(rel))
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return FileErr(rel, err)
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return FileErr(rel, err)
		}
		return nil
	})
}

// WriteFresh writes data to full (inside dir, at rel) as a new regular file: whatever file or
// link is there goes first, and the create is O_EXCL, so the write cannot land through a link.
func WriteFresh(full, rel string, data []byte) error {
	if info, err := os.Lstat(full); err == nil {
		if info.IsDir() {
			return fmt.Errorf("%s: is a directory", scheduled.ShowPath(rel))
		}
		if err := os.Remove(full); err != nil {
			return FileErr(rel, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return FileErr(rel, err)
	}
	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return FileErr(rel, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return FileErr(rel, err)
	}
	if err := f.Close(); err != nil {
		return FileErr(rel, err)
	}
	return nil
}
