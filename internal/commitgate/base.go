package commitgate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/bashwrites"
)

// EmptyTree is the tree git hashes for an empty repository: the base of a first commit.
const EmptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

const (
	gitDeadline = 30 * time.Second
	unborn      = "unborn"
)

// Moved is a root whose HEAD is another commit than before the Bash call.
type Moved struct{ Root, Base, Head string }

// StateDir is `${XDG_RUNTIME_DIR:-/tmp}/lighthouse-commit-test-gate/<session_id>/`, mode 0700. With
// create false it is only the path: MovedRoots runs after every Bash call and must not fail on a
// state dir it cannot make.
func StateDir(sessionID string, create bool) (string, error) {
	if strings.Contains(sessionID, "/") || sessionID == ".." {
		return "", fmt.Errorf("invalid session_id: '%s'", sessionID)
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}
	base = filepath.Join(base, "lighthouse-commit-test-gate")
	path := filepath.Join(base, sessionID)
	if create {
		if err := os.MkdirAll(base, 0o755); err != nil {
			return "", err
		}
		if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
			return "", err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return "", err
		}
	}
	return path, nil
}

// markerPath is the marker file of the Bash call in raw (its hook payload).
func markerPath(raw []byte, create bool) (string, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("payload: %w", err)
	}
	session := ""
	if s, ok := payload["session_id"]; ok {
		if err := json.Unmarshal(s, &session); err != nil {
			session = strings.TrimSpace(string(s))
		}
	}
	state, err := StateDir(session, create)
	if err != nil {
		return "", err
	}
	key, err := bashwrites.MarkerKey(payload)
	if err != nil {
		return "", err
	}
	return filepath.Join(state, key+".json"), nil
}

// heads is the marker's `heads`: root to sha, in the order the roots were recorded.
type heads struct{ roots, shas []string }

func (h heads) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, root := range h.roots {
		if i > 0 {
			buf.WriteByte(',')
		}
		for j, text := range []string{root, h.shas[i]} {
			if j == 1 {
				buf.WriteByte(':')
			}
			part, err := json.Marshal(text)
			if err != nil {
				return nil, err
			}
			buf.Write(part)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Record writes the HEAD of every root to the marker of this Bash call (raw is its hook payload).
func Record(raw []byte, roots []string) error {
	if err := record(raw, roots); err != nil {
		return fmt.Errorf("commit_base: %w", err)
	}
	return nil
}

func record(raw []byte, roots []string) error {
	marker, err := markerPath(raw, true)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(gitDeadline)
	h := heads{}
	seen := map[string]int{}
	for _, root := range roots {
		head, err := bashwrites.HeadOf(root, deadline)
		if err != nil {
			return err
		}
		if i, dup := seen[root]; dup { // a Python dict keeps the first place and the last value
			h.shas[i] = head
			continue
		}
		seen[root] = len(h.roots)
		h.roots, h.shas = append(h.roots, root), append(h.shas, head)
	}
	dir := filepath.Dir(marker)
	bashwrites.PruneMarkers(dir, time.Now())
	return bashwrites.AtomicWriteJSON(dir, marker, map[string]any{"heads": h})
}

// readHeads reads the `heads` object of a marker in the order of its keys, as a Python dict does.
func readHeads(data []byte) (rows [][2]string, err error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errors.New("marker is not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		if tok != "heads" {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, err
			}
			continue
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
			return nil, errors.New("heads is not a JSON object")
		}
		index := map[string]int{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, err
			}
			var sha string
			if err := dec.Decode(&sha); err != nil {
				return nil, fmt.Errorf("head of %v: %w", key, err)
			}
			if i, dup := index[key.(string)]; dup {
				rows[i][1] = sha
				continue
			}
			index[key.(string)] = len(rows)
			rows = append(rows, [2]string{key.(string), sha})
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		for dec.More() { // the rest of the object
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
		}
		return rows, nil
	}
	return nil, errors.New("marker has no heads")
}

// MovedRoots reads and deletes the marker of this Bash call (raw is its hook payload) and returns
// the roots whose HEAD is now another commit, in the order of the marker. No marker gives nil, nil;
// an unborn base is EmptyTree.
func MovedRoots(raw []byte) ([]Moved, error) {
	moved, err := movedRoots(raw)
	if err != nil {
		return nil, fmt.Errorf("commit_base: %w", err)
	}
	return moved, nil
}

func movedRoots(raw []byte) ([]Moved, error) {
	marker, err := markerPath(raw, false)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = os.Remove(marker)
	rows, err := readHeads(data)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(gitDeadline)
	var result []Moved
	for _, row := range rows {
		now, err := bashwrites.HeadOf(row[0], deadline)
		if err != nil {
			return nil, err
		}
		if now == row[1] {
			continue
		}
		base := row[1]
		if base == unborn {
			base = EmptyTree
		}
		result = append(result, Moved{Root: row[0], Base: base, Head: now})
	}
	return result, nil
}

// ChangedFiles are the paths changed between base and head, both sides of a rename, sorted.
func ChangedFiles(root, base, head string) ([]string, error) {
	out, err := bashwrites.Git(root, time.Now().Add(gitDeadline),
		"diff", "--name-only", "--no-renames", "-z", base, head)
	if err != nil {
		return nil, fmt.Errorf("commit_base: %w", err)
	}
	var files []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}
