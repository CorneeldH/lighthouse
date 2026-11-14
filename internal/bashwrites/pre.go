package bashwrites

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// preDeadline is how long pre may seed; the hook timeout is 5 s.
var preDeadline = 4 * time.Second

// seed is seedRoot; a test swaps it.
var seed = seedRoot

// stringField is the JSON string at key, false if it is missing or no string.
func stringField(fields map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := fields[key]
	if !ok {
		return "", false
	}
	var s string
	return s, json.Unmarshal(raw, &s) == nil
}

func runPre(raw []byte, now func() time.Time) error {
	projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
	if projectDir == "" {
		return nil
	}
	start := now().UnixNano()
	if text := os.Getenv("LIGHTHOUSE_BASH_GATES_START_NS"); text != "" {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return err
		}
		start = n
	}
	deadline := now().Add(preDeadline)

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return errors.New("the payload is not a JSON object")
	}
	var input map[string]json.RawMessage
	if err := json.Unmarshal(payload["tool_input"], &input); err != nil {
		return err
	}
	command, ok := stringField(input, "command")
	if !ok {
		return errors.New("tool_input.command is not a string")
	}
	cwd, ok := stringField(payload, "cwd")
	if !ok {
		return errors.New("cwd is not a string")
	}
	kept, _, err := ProjectFilter(DeriveRoots(command, cwd), projectDir, deadline)
	if err != nil {
		return err
	}

	session := ""
	if _, present := payload["session_id"]; present {
		if session, ok = stringField(payload, "session_id"); !ok {
			return errors.New("session_id is not a string")
		}
	}
	state, err := StateDir(session)
	if err != nil {
		return err
	}
	key, err := MarkerKey(payload)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(kept))
	for root := range kept {
		roots = append(roots, root)
	}
	sort.Strings(roots)

	// The marker first: if pre is killed at its timeout during seeding, post still has the roots
	// and the start time. A root without a seed falls back in post: overchecking, never blind.
	if err := writeMarker(state, key, start, roots); err != nil {
		return err
	}

	// A best-effort peek without a lock saves a git status per already known root. An unreadable
	// ledger is not seeded (that is the fallback of post), and nor is anything while the sentinel
	// ledger.reset exists: a missing root may have had earlier session writes. The real decision
	// is taken again under the lock, as post creates the sentinel under that same lock.
	reset := filepath.Join(state, resetName)
	peek := readLedger(filepath.Join(state, "ledger.json"))
	if !peek.Unreadable && !exists(reset) {
		for _, root := range roots {
			if !now().Before(deadline) {
				break
			}
			if _, known := peek.Roots[root]; known {
				continue
			}
			record, err := seed(root, start, deadline)
			if err != nil {
				continue // a failing seed skips post's baseline, not pre
			}
			err = withLedger(state, func(l *Ledger) error {
				if _, known := l.Roots[root]; known || exists(reset) {
					return nil
				}
				return l.Set(root, record)
			})
			if err != nil {
				return err
			}
		}
	}
	PruneMarkers(state, now())
	return nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Pre is the PreToolUse step: write the marker, seed new roots, clean up old markers. The marker
// comes before the seeding and the seeding stops at the pre deadline.
//
// Always 0: a PreToolUse exit 2 blocks the Bash call itself, and a broken seed or marker step
// must not hold up any work (post notices the gap). Every error and every panic is swallowed.
func Pre(raw []byte, now func() time.Time) (code int) {
	defer func() { _ = recover() }()
	_ = runPre(raw, now)
	return 0
}
