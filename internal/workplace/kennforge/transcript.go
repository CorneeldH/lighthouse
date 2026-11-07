package kennforge

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

var _ workplace.Transcripts = (*Adapter)(nil)

const (
	transcriptTimeout = 30 * time.Second
	// notInContainer is the exit code of containerScript for a container without the transcript.
	notInContainer = 3
)

// containerScript tars the main transcript of session $2, and its subagent transcripts, out of
// the project directory $1 under the container's ~/.claude/projects.
const containerScript = `d="$HOME/.claude/projects/$1"; [ -f "$d/$2.jsonl" ] || exit 3; cd "$d" || exit 4; ` +
	`if [ -d "$2/subagents" ]; then exec tar -cf - "$2.jsonl" "$2/subagents"; fi; exec tar -cf - "$2.jsonl"`

var safeSession = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// Transcript reads the transcript of session sessionID in the workspace, as
// workplace.ReadTranscript does. With the software profile claude runs in the shared
// devcontainer and keeps its transcripts in a volume the host cannot read (#237): the session's
// files then come out of the container, and a transcript in no container wraps fs.ErrNotExist.
// With any other profile they are on this host, under Home. A 404 is workplace.ErrGone.
func (a *Adapter) Transcript(ctx context.Context, workspaceID, sessionID string) (workplace.Transcript, error) {
	if err := notForge(workspaceID); err != nil {
		return workplace.Transcript{}, err
	}
	if !safeSession.MatchString(sessionID) {
		return workplace.Transcript{}, fmt.Errorf("transcript in workspace %s: unsafe session id %q", workspaceID, sessionID)
	}
	w, err := a.readWorkspace(ctx, workspaceID)
	if err != nil {
		return workplace.Transcript{}, err
	}
	if a.Profile == "software" {
		return a.containerTranscript(ctx, w.WorktreeDir, sessionID)
	}
	dir, err := workplace.HostTranscriptDir(a.Home, workspaceID, w.WorktreeDir)
	if err != nil {
		return workplace.Transcript{}, err
	}
	return workplace.ReadTranscript(dir, sessionID)
}

func (a *Adapter) containerTranscript(ctx context.Context, worktree, sessionID string) (workplace.Transcript, error) {
	project := filepath.Base(workplace.ProjectDir("/", worktree))
	containers, err := a.devcontainers(ctx)
	if err != nil {
		return workplace.Transcript{}, err
	}
	for _, c := range containers {
		out, code, err := a.Run(ctx, transcriptTimeout, "podman", "exec", c, "sh", "-c", containerScript,
			"sh", project, sessionID)
		if err != nil {
			return workplace.Transcript{}, fmt.Errorf("transcript of %s in container %s: %w", sessionID, c, err)
		}
		switch code {
		case notInContainer:
			continue
		case 0:
			return untarTranscript(out, sessionID)
		}
		return workplace.Transcript{}, fmt.Errorf("transcript of %s in container %s: exit code %d", sessionID, c, code)
	}
	return workplace.Transcript{}, fmt.Errorf("transcript of %s: in no devcontainer: %w", sessionID, fs.ErrNotExist)
}

// untarTranscript unpacks the session's files from stream into a temporary directory, reads
// them there and removes the directory. A member that is not one of the session's files is
// skipped, so nothing lands outside that directory.
func untarTranscript(stream, sessionID string) (workplace.Transcript, error) {
	dir, err := os.MkdirTemp("", "lighthouse-transcript-")
	if err != nil {
		return workplace.Transcript{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	tr := tar.NewReader(strings.NewReader(stream))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return workplace.Transcript{}, fmt.Errorf("transcript of %s: read tar: %w", sessionID, err)
		}
		if h.Typeflag != tar.TypeReg || !sessionFile(h.Name, sessionID) {
			continue
		}
		if err := writeMember(filepath.Join(dir, filepath.FromSlash(h.Name)), tr); err != nil {
			return workplace.Transcript{}, fmt.Errorf("transcript of %s: %w", sessionID, err)
		}
	}
	return workplace.ReadTranscript(dir, sessionID)
}

// sessionFile reports whether name is <session>.jsonl or <session>/subagents/<file>.jsonl.
func sessionFile(name, sessionID string) bool {
	if name == sessionID+".jsonl" {
		return true
	}
	base, ok := strings.CutPrefix(name, sessionID+"/subagents/")
	return ok && base == path.Base(base) && base != ".." && strings.HasSuffix(base, ".jsonl")
}

func writeMember(dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
