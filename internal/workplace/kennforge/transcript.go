package kennforge

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

var _ workplace.Transcripts = (*Adapter)(nil)

const (
	transcriptTimeout = 30 * time.Second
	// notInContainer is the exit code of containerScript for a container without the transcript.
	notInContainer = 3
	// deltaManifest is the tar member of containerScript that holds, per file, its size and the
	// offset its member starts at: "<size> <from> <name>" per line.
	deltaManifest = ".lighthouse-delta"
)

// containerScript tars the transcript files of session $2 (the main transcript and its subagent
// transcripts) out of the project directory $1 under the container's ~/.claude/projects, each
// from the offset $3 names for it: a list "<name>:<offset>,…", where a missing name is 0 and an
// offset past the end of the file is 0 too. It takes the size of each file before it reads, so
// a member ends there while claude appends, and tars the members with the manifest
// deltaManifest from a temporary directory it removes again.
const containerScript = `d="$HOME/.claude/projects/$1"; [ -f "$d/$2.jsonl" ] || exit 3; cd "$d" || exit 4; ` +
	`t=$(mktemp -d) || exit 4; trap 'rm -rf "$t"' EXIT; mkdir -p "$t/$2/subagents" || exit 4; ` +
	`: > "$t/` + deltaManifest + `" || exit 4; ` +
	`for f in "$2.jsonl" "$2"/subagents/*.jsonl; do [ -f "$f" ] || continue; ` +
	`s=$(wc -c < "$f") || exit 4; s=$(($s)); o=0; set -f; IFS=,; ` +
	`for p in $3; do [ "${p%:*}" = "$f" ] && o=${p##*:}; done; unset IFS; set +f; ` +
	`[ "$o" -le "$s" ] || o=0; ` +
	`tail -c +$((o+1)) "$f" | head -c $((s-o)) > "$t/$f" || exit 4; ` +
	`printf '%s %s %s\n' "$s" "$o" "$f" >> "$t/` + deltaManifest + `" || exit 4; done; ` +
	`cd "$t" || exit 4; tar -cf - ` + deltaManifest + ` "$2.jsonl" "$2"; r=$?; exit $r`

// Transcript reads the transcript of session sessionID in the workspace, as
// workplace.ReadTranscript does. With the software profile claude runs in the shared
// devcontainer and keeps its transcripts in a volume the host cannot read (#237): the session's
// files then come out of the container, and a transcript in no container wraps fs.ErrNotExist.
// With any other profile they are on this host, under Home. A 404 is workplace.ErrGone. On
// another node lighthouse serve sends the new bytes and the tick reads its mirror of them
// (nodeTranscript, #327).
func (a *Adapter) Transcript(ctx context.Context, workspaceID, sessionID string) (workplace.Transcript, error) {
	if err := notForge(workspaceID); err != nil {
		return workplace.Transcript{}, err
	}
	n, err := a.serve()
	if err != nil {
		return workplace.Transcript{}, err
	}
	if n != nil {
		return a.nodeTranscript(ctx, n, workspaceID, sessionID)
	}
	w, err := a.transcriptWorkspace(ctx, workspaceID, sessionID)
	if err != nil {
		return workplace.Transcript{}, err
	}
	if a.Profile == "software" {
		deltas, err := a.containerDelta(ctx, w.WorktreeDir, sessionID, nil)
		if err != nil {
			return workplace.Transcript{}, err
		}
		return readDeltas(deltas, sessionID)
	}
	dir, err := workplace.HostTranscriptDir(a.Home, workspaceID, w.WorktreeDir)
	if err != nil {
		return workplace.Transcript{}, err
	}
	return workplace.ReadTranscript(dir, sessionID)
}

// TranscriptDelta reads the transcript files of session sessionID in the workspace from the
// offsets in from, as workplace.ReadDelta does, in the same place Transcript reads them: in
// the devcontainer with the software profile, on this host under Home with any other. It is
// what lighthouse serve answers the tick that mirrors a run on this node (#327). A session in
// no container, or without a main transcript, wraps fs.ErrNotExist; a project dir that is not
// on this host is workplace.ErrNotMeasurable; a 404 is workplace.ErrGone.
func (a *Adapter) TranscriptDelta(ctx context.Context, workspaceID, sessionID string, from map[string]int64) ([]workplace.FileDelta, error) {
	w, err := a.transcriptWorkspace(ctx, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	if a.Profile == "software" {
		return a.containerDelta(ctx, w.WorktreeDir, sessionID, from)
	}
	dir, err := workplace.HostTranscriptDir(a.Home, workspaceID, w.WorktreeDir)
	if err != nil {
		return nil, err
	}
	return workplace.ReadDelta(dir, sessionID, from)
}

// transcriptWorkspace checks what Transcript and TranscriptDelta read from and reads the
// workspace at the forge.
func (a *Adapter) transcriptWorkspace(ctx context.Context, workspaceID, sessionID string) (wireWorkspace, error) {
	if err := notForge(workspaceID); err != nil {
		return wireWorkspace{}, err
	}
	if a.Node != "" {
		return wireWorkspace{}, fmt.Errorf("transcript on node %s: %w", a.Node, workplace.ErrNotMeasurable)
	}
	if !workplace.SafeSession.MatchString(sessionID) {
		return wireWorkspace{}, fmt.Errorf("transcript in workspace %s: unsafe session id %q", workspaceID, sessionID)
	}
	return a.readWorkspace(ctx, workspaceID)
}

// containerDelta runs containerScript in each devcontainer until one holds the session.
func (a *Adapter) containerDelta(ctx context.Context, worktree, sessionID string, from map[string]int64) ([]workplace.FileDelta, error) {
	project := filepath.Base(workplace.ProjectDir("/", worktree))
	offsets := offsetList(sessionID, from)
	containers, err := a.devcontainers(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		out, code, err := a.Run(ctx, transcriptTimeout, "podman", "exec", c, "sh", "-c", containerScript,
			"sh", project, sessionID, offsets)
		if err != nil {
			return nil, fmt.Errorf("transcript of %s in container %s: %w", sessionID, c, err)
		}
		switch code {
		case notInContainer:
			continue
		case 0:
			return untarDelta(out, sessionID)
		}
		return nil, fmt.Errorf("transcript of %s in container %s: exit code %d", sessionID, c, code)
	}
	return nil, fmt.Errorf("transcript of %s: in no devcontainer: %w", sessionID, fs.ErrNotExist)
}

// offsetList is $3 of containerScript: "<name>:<offset>" for every file of the session in
// from with a positive offset, sorted, joined by commas. A name that is no file of the session
// never reaches the shell.
func offsetList(sessionID string, from map[string]int64) string {
	var parts []string
	for name, off := range from {
		if off > 0 && workplace.SessionFile(name, sessionID) && !strings.ContainsAny(name, ",:") {
			parts = append(parts, name+":"+strconv.FormatInt(off, 10))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// untarDelta reads the deltas of the session out of the tar stream of containerScript. A member
// that is not one of the session's files is skipped; a session file the manifest does not
// account for, or whose bytes do not run from its offset to its size, is an error.
func untarDelta(stream, sessionID string) ([]workplace.FileDelta, error) {
	var manifest map[string]manifestEntry
	var deltas []workplace.FileDelta
	tr := tar.NewReader(strings.NewReader(stream))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("transcript of %s: read tar: %w", sessionID, err)
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		if h.Name == deltaManifest {
			if manifest, err = parseManifest(tr); err != nil {
				return nil, fmt.Errorf("transcript of %s: %w", sessionID, err)
			}
			continue
		}
		if !workplace.SessionFile(h.Name, sessionID) {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("transcript of %s: read %s: %w", sessionID, h.Name, err)
		}
		deltas = append(deltas, workplace.FileDelta{Name: h.Name, Data: data})
	}
	main := -1
	for i := range deltas {
		e, ok := manifest[deltas[i].Name]
		if !ok || e.size-e.from != int64(len(deltas[i].Data)) {
			return nil, fmt.Errorf("transcript of %s: %s does not match its manifest line", sessionID, deltas[i].Name)
		}
		deltas[i].Size, deltas[i].From = e.size, e.from
		if deltas[i].Name == sessionID+".jsonl" {
			main = i
		}
	}
	if main < 0 {
		return nil, fmt.Errorf("transcript of %s: no main transcript in the tar: %w", sessionID, fs.ErrNotExist)
	}
	// The main transcript first, as workplace.ReadDelta answers.
	deltas[0], deltas[main] = deltas[main], deltas[0]
	return deltas, nil
}

// manifestEntry is one line of deltaManifest: the size of a file and the offset its member starts at.
type manifestEntry struct{ size, from int64 }

// parseManifest reads the lines "<size> <from> <name>" of deltaManifest.
func parseManifest(r io.Reader) (map[string]manifestEntry, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	m := map[string]manifestEntry{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, " ", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("manifest line %q", line)
		}
		size, err1 := strconv.ParseInt(f[0], 10, 64)
		from, err2 := strconv.ParseInt(f[1], 10, 64)
		if err1 != nil || err2 != nil || from < 0 || from > size {
			return nil, fmt.Errorf("manifest line %q", line)
		}
		m[f[2]] = manifestEntry{size, from}
	}
	return m, nil
}

// readDeltas writes whole-file deltas into a temporary directory, reads them there with
// workplace.ReadTranscript and removes the directory. Names are session files, so nothing
// lands outside that directory.
func readDeltas(deltas []workplace.FileDelta, sessionID string) (workplace.Transcript, error) {
	dir, err := os.MkdirTemp("", "lighthouse-transcript-")
	if err != nil {
		return workplace.Transcript{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, d := range deltas {
		if d.From != 0 || !workplace.SessionFile(d.Name, sessionID) {
			return workplace.Transcript{}, fmt.Errorf("transcript of %s: %s is no whole session file", sessionID, d.Name)
		}
		if err := writeMember(filepath.Join(dir, filepath.FromSlash(d.Name)), d.Data); err != nil {
			return workplace.Transcript{}, fmt.Errorf("transcript of %s: %w", sessionID, err)
		}
	}
	return workplace.ReadTranscript(dir, sessionID)
}

func writeMember(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
