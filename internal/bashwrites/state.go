package bashwrites

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/flock"
)

const (
	resetName    = "ledger.reset"
	markerMaxAge = 3600 * time.Second
)

// StateDir is `${XDG_RUNTIME_DIR:-/tmp}/lighthouse-bash-gates/<session_id>/`, created with mode
// 0700. A session_id with a "/" or equal to ".." could escape the state directory and is refused.
// Only the session directory itself must be 0700, so it is set once more after creation: a umask
// can influence the mode of the mkdir itself.
func StateDir(sessionID string) (string, error) {
	if strings.Contains(sessionID, "/") || sessionID == ".." {
		return "", fmt.Errorf("invalid session_id: '%s'", sessionID)
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}
	base = filepath.Join(base, "lighthouse-bash-gates")
	path := filepath.Join(base, sessionID)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(path, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return "", err
	}
	return path, nil
}

// Ledger is one session ledger: Roots (path to root rule) plus Unreadable.
//
// The rows stay raw JSON, so a field this version does not know, and a nanosecond signature above
// 2^53 (which a float64 would mangle), survive a write-back unchanged. A row that is not a JSON
// object drops only that root.
//
// Unreadable is true if ledger.json WAS there but held no valid JSON or no object: an empty but
// usable ledger, with a signal that something was broken. withLedger never writes back while
// Unreadable is still true on leaving; a caller that deliberately replaces the broken ledger
// (only post) clears it inside the body, and creates the sentinel ledger.reset under the same
// lock: the fresh ledger knows only the roots that post walked, so as long as the sentinel exists
// pre seeds no missing root, and post falls back for one and adds it.
type Ledger struct {
	Roots      map[string]json.RawMessage
	Unreadable bool
}

// Get decodes the row of root into v. False if there is no such root.
func (l *Ledger) Get(root string, v any) (bool, error) {
	raw, ok := l.Roots[root]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, v)
}

// Set replaces the row of root by the JSON of v.
func (l *Ledger) Set(root string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	l.Roots[root] = raw
	return nil
}

func isObject(raw []byte) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '{'
}

func readLedger(path string) *Ledger {
	raw, err := os.ReadFile(path)
	if err != nil {
		return &Ledger{Roots: map[string]json.RawMessage{}}
	}
	var rows map[string]json.RawMessage
	if !isObject(raw) || json.Unmarshal(raw, &rows) != nil {
		return &Ledger{Roots: map[string]json.RawMessage{}, Unreadable: true}
	}
	for root, row := range rows {
		if !isObject(row) {
			delete(rows, root)
		}
	}
	return &Ledger{Roots: rows}
}

// atomicWriteJSON writes v as JSON to target through a temporary file in dir and a rename.
func atomicWriteJSON(dir, target string, v any) (err error) {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*.json")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}

// withLedger reads ledger.json under an exclusive flock on ledger.lock, runs body, and writes the
// ledger back atomically, only if body returned nil and Unreadable is false then.
//
// The lock covers only the read, body and write-back, never a gate: pre (timeout 5 s) must not
// wait for a running post. Unreadable stays a no-write on purpose: pre must not silently replace
// a broken ledger with an empty one, or the "ledger unreadable" fallback of post loses its signal
// and what this session already wrote becomes unseen baseline.
func withLedger(state string, body func(*Ledger) error) error {
	lock, err := flock.Lock(filepath.Join(state, "ledger.lock"))
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	ledgerPath := filepath.Join(state, "ledger.json")
	ledger := readLedger(ledgerPath)
	if err := body(ledger); err != nil {
		return err
	}
	if ledger.Unreadable {
		return nil
	}
	return atomicWriteJSON(state, ledgerPath, ledger.Roots)
}

// splitExt is Python's os.path.splitext(name)[1]: leading dots of the base name do not start an
// extension.
func splitExt(name string) string {
	base := filepath.Base(name)
	trimmed := strings.TrimLeft(base, ".")
	i := strings.LastIndex(trimmed, ".")
	if i < 0 {
		return ""
	}
	return trimmed[i:]
}

// seedRoot is the baseline for root: gate files already dirty from before startNS, plus head.
//
// Source is `git status --porcelain -z -uall`, limited to paths with a gate extension that still
// exist AND whose ctime is before startNS: a path touched at or after the start of pre may
// already have been written by the command itself and does not belong in the baseline. Deleted
// paths do not end up in seen. A name that is not valid UTF-8 is left out: JSON cannot hold it
// and post reports it as COULD NOT CHECK.
func seedRoot(root string, startNS int64, deadline time.Time) (map[string]any, error) {
	data, err := Git(root, deadline, "status", "--porcelain", "-z", "-uall")
	if err != nil {
		return nil, err
	}
	changed, _ := ParsePorcelain(data)
	seen := map[string][]int64{}
	for rel := range changed {
		if _, ok := Gates[splitExt(rel)]; !ok || !isUTF8(rel) {
			continue
		}
		sig, ok := signatureOf(filepath.Join(root, rel))
		if !ok || sig[1] >= startNS {
			continue
		}
		seen[rel] = sig[:]
	}
	head, err := HeadOf(root, deadline)
	if err != nil {
		return nil, err
	}
	return map[string]any{"seen": seen, "head": head, "pending": []string{}, "claims": map[string]any{}}, nil
}

func isUTF8(s string) bool { return strings.ToValidUTF8(s, "") == s }

// marker is `<state>/<key>.json`: the start time of pre (`start_ns`) and the roots it derived
// (`roots`). It is read field by field, as the Python does: a mistyped field drops only itself.
type marker map[string]json.RawMessage

func writeMarker(state, key string, startNS int64, roots []string) error {
	if roots == nil {
		roots = []string{}
	}
	return atomicWriteJSON(state, filepath.Join(state, key+".json"), map[string]any{"start_ns": startNS, "roots": roots})
}

// readMarker is the marker of key, false if it is missing or not a JSON object.
func readMarker(state, key string) (marker, bool) {
	raw, err := os.ReadFile(filepath.Join(state, key+".json"))
	if err != nil || !isObject(raw) {
		return nil, false
	}
	var m marker
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	return m, true
}

// Roots are the string entries of `roots`; nil if it is not a list.
func (m marker) Roots() []string {
	var entries []json.RawMessage
	if json.Unmarshal(m["roots"], &entries) != nil {
		return nil
	}
	roots := []string{}
	for _, entry := range entries {
		var root string
		if json.Unmarshal(entry, &root) == nil {
			roots = append(roots, root)
		}
	}
	return roots
}

// startNS is `start_ns`, false if it is missing or not an integer.
func (m marker) startNS() (int64, bool) {
	var ns int64
	if json.Unmarshal(m["start_ns"], &ns) != nil {
		return 0, false
	}
	return ns, true
}

// since is the pre start time in whole epoch seconds, rounded down: the reflog epoch is in whole
// seconds, and rounding up would drop a commit from the same second as pre. Nil without a valid
// start_ns.
func (m marker) since() *int64 {
	ns, ok := m.startNS()
	if !ok {
		return nil
	}
	s := ns / 1e9
	if ns%1e9 < 0 {
		s-- // floor, as Python's //
	}
	return &s
}

// pruneMarkers removes marker files older than an hour. ledger.json, ledger.lock and the sentinel
// ledger.reset stay.
func pruneMarkers(state string, now time.Time) {
	entries, err := os.ReadDir(state)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == "ledger.json" || name == "ledger.lock" || name == resetName || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := os.Stat(filepath.Join(state, name))
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > markerMaxAge {
			_ = os.Remove(filepath.Join(state, name))
		}
	}
}
