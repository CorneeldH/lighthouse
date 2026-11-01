package cli

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strings"
)

// exportTree unpacks the tracked content of ref in repoDir (git archive: no .git, no untracked or
// ignored files, export-ignore honoured) into a new lighthouse-triage-* temp directory. Entries
// are written through an os.Root, so none can land outside it. remove deletes the directory and
// is safe to call more than once; on an error the directory is already gone and remove is nil.
func exportTree(ctx context.Context, repoDir, ref string) (dir string, remove func(), err error) {
	dir, err = os.MkdirTemp("", "lighthouse-triage-")
	if err != nil {
		return "", nil, err
	}
	if err := unpackArchive(ctx, repoDir, ref, dir); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// unpackArchive runs git archive for ref and unpacks the tar stream into dir.
func unpackArchive(ctx context.Context, repoDir, ref, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	gctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stderr strings.Builder
	cmd := exec.CommandContext(gctx, "git", "-C", repoDir, "archive", "--format=tar", ref)
	cmd.Stderr = &stderr
	cmd.WaitDelay = judgeWaitDelay
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git archive: %w", err)
	}

	unpackErr := unpackTar(root, stdout)
	if unpackErr != nil {
		cancel() // git may be blocked on a full pipe
	} else {
		_, _ = io.Copy(io.Discard, stdout) // let git finish its trailing padding
	}
	waitErr := cmd.Wait()
	msg := strings.TrimSpace(stderr.String())
	if len(msg) > judgeStderr {
		msg = msg[:judgeStderr]
	}
	if unpackErr != nil {
		if msg != "" {
			// git may have died first: keep its message next to the truncated stream
			return fmt.Errorf("%w (git archive: %s)", unpackErr, msg)
		}
		return unpackErr // git's own error is only the cancel
	}
	if waitErr != nil {
		return fmt.Errorf("git archive: %w: %s", waitErr, msg)
	}
	return nil
}

// unpackTar writes every entry of the tar stream r into root.
func unpackTar(root *os.Root, r io.Reader) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read archive: %w", err)
		}
		name := path.Clean(h.Name)
		if hasGitComponent(name) {
			return fmt.Errorf("archive entry %q: a .git path component is not allowed", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeXGlobalHeader:
			continue
		case tar.TypeDir:
			err = root.MkdirAll(name, 0o755)
		case tar.TypeReg:
			err = writeEntry(root, name, h, tr)
		case tar.TypeSymlink:
			if err = root.MkdirAll(path.Dir(name), 0o755); err == nil {
				err = root.Symlink(h.Linkname, name)
			}
		default:
			err = fmt.Errorf("unsupported entry type %q", h.Typeflag)
		}
		if err != nil {
			return fmt.Errorf("archive entry %q: %w", h.Name, err)
		}
	}
}

// writeEntry creates the regular file name in root with the permission bits of h.
func writeEntry(root *os.Root, name string, h *tar.Header, r io.Reader) error {
	if err := root.MkdirAll(path.Dir(name), 0o755); err != nil {
		return err
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fs.FileMode(h.Mode).Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// hasGitComponent reports whether a component of the cleaned name is .git, in any letter case:
// the export holds no repository metadata, and a .git file or directory could redirect the harness.
func hasGitComponent(name string) bool {
	for _, c := range strings.Split(name, "/") {
		if strings.EqualFold(c, ".git") {
			return true
		}
	}
	return false
}
