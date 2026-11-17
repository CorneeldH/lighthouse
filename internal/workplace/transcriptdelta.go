package workplace

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// FileDelta is the part of one transcript file of a session past an offset: what a mirror
// that holds the first From bytes appends to become the file of Size bytes.
type FileDelta struct {
	// Name is the file relative to the transcript dir, with slashes: <session>.jsonl or
	// <session>/subagents/<file>.jsonl.
	Name string
	// Size is the length of the file when it was read; Data ends there.
	Size int64
	// From is the offset Data starts at: the offset asked for, or 0 when the file is shorter
	// than that offset (rotated, or a new session with the same id), and Data is then the
	// whole file.
	From int64
	// Data is the bytes [From, Size) of the file.
	Data []byte
}

// SessionFile reports whether name, relative with slashes, is a transcript file of session
// sessionID: <session>.jsonl or <session>/subagents/<file>.jsonl.
func SessionFile(name, sessionID string) bool {
	if name == sessionID+".jsonl" {
		return true
	}
	base, ok := strings.CutPrefix(name, sessionID+"/subagents/")
	return ok && base == path.Base(base) && base != ".." && strings.HasSuffix(base, ".jsonl")
}

// ReadDelta reads, for every transcript file of session sessionID in dir (the main transcript
// first, then the subagent transcripts as ReadTranscript finds them), the bytes from the offset
// from holds for its Name; a file without one is read whole. Keys of from that are no file of
// the session are ignored. The length of each file is taken before it is read, so Data always
// ends at Size, also while claude appends. A missing main transcript wraps fs.ErrNotExist.
func ReadDelta(dir, sessionID string, from map[string]int64) ([]FileDelta, error) {
	if !SafeSession.MatchString(sessionID) {
		return nil, fmt.Errorf("transcript delta: unsafe session id %q", sessionID)
	}
	main, err := readFileDelta(dir, sessionID+".jsonl", from)
	if err != nil {
		return nil, err
	}
	out := []FileDelta{main}
	subs, err := filepath.Glob(filepath.Join(dir, sessionID, "subagents", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, p := range subs {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() {
			continue
		}
		d, err := readFileDelta(dir, sessionID+"/subagents/"+filepath.Base(p), from)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// readFileDelta reads the file name in dir from the offset from holds for it.
func readFileDelta(dir, name string, from map[string]int64) (FileDelta, error) {
	f, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return FileDelta{}, fmt.Errorf("transcript delta: %w", err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return FileDelta{}, fmt.Errorf("transcript delta: %w", err)
	}
	if !st.Mode().IsRegular() {
		return FileDelta{}, fmt.Errorf("transcript delta: %s is no regular file", f.Name())
	}
	size := st.Size()
	off := from[name]
	if off < 0 || off > size {
		off = 0
	}
	data := make([]byte, size-off)
	if _, err := io.ReadFull(io.NewSectionReader(f, off, size-off), data); err != nil {
		return FileDelta{}, fmt.Errorf("transcript delta: read %s: %w", f.Name(), err)
	}
	return FileDelta{Name: name, Size: size, From: off, Data: data}, nil
}
