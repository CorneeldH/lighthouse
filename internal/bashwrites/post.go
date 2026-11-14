package bashwrites

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// The PostToolUse step. Port of `_marker_roots` to `run_post` in the former
// pack/core/lib/bash_writes.py.

// defaultPostDeadline is the one deadline of post, from its start; with the grace per gate well
// within the hook timeout of 120 s.
const defaultPostDeadline = 90 * time.Second

// now is the clock of Post; a test swaps it (Python `_now`).
var now = time.Now

// postDeadline is LIGHTHOUSE_BASH_GATES_DEADLINE (seconds, a test-only seam) or 90 s.
func postDeadline() time.Duration {
	raw := strings.TrimSpace(os.Getenv("LIGHTHOUSE_BASH_GATES_DEADLINE"))
	if raw == "" {
		return defaultPostDeadline
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultPostDeadline
	}
	return time.Duration(seconds * float64(time.Second))
}

// pyStr is Python's str() of a JSON value: a string as is, null "None", a bool "True" or "False",
// anything else its JSON text. A missing value is def.
func pyStr(raw json.RawMessage, def string) string {
	if raw == nil {
		return def
	}
	text := strings.TrimSpace(string(raw))
	switch text {
	case "null":
		return "None"
	case "true":
		return "True"
	case "false":
		return "False"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return text
}

// postRoots are the existing and the vanished roots, both sorted: the marker roots plus the
// project roots of the post payload. The post cwd may have shifted, and a root in which a commit
// was made and then left must not drop out. A root that no longer exists (a deleted worktree) can
// no longer be checked and comes back separately, so post can name it. Fails with a *GitError if
// projectDir itself cannot be read.
func postRoots(payload map[string]json.RawMessage, markerRoots []string, projectDir string, until time.Time, o *Outcome) (present, gone []string, err error) {
	command := ""
	if input, ok := decodeObject(payload["tool_input"]); ok {
		_ = json.Unmarshal(input["command"], &command)
	}
	cwd := projectDir
	if c, ok := stringField(payload, "cwd"); ok && c != "" {
		cwd = c
	}
	kept, errs, err := ProjectFilter(DeriveRoots(command, cwd), projectDir, until)
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(errs)
	for _, line := range errs {
		o.block(prefix + " " + line)
	}
	roots := map[string]bool{}
	for _, root := range markerRoots {
		roots[root] = true
	}
	for root := range kept {
		roots[root] = true
	}
	for _, root := range sortedSet(roots) {
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			present = append(present, root)
		} else {
			gone = append(gone, root)
		}
	}
	return present, gone, nil
}

// rootPools is the pool per root, with the notices and the paths that cannot be looked at. A
// fallback root gets a notice only if it is in the marker (it already existed before pre); a root
// that came into being during the call (`git worktree add`, `git init`) falls back silently.
func rootPools(roots []string, ledger *Ledger, markerRoots []string, since *int64, until time.Time, o *Outcome) ([]pool, error) {
	inMarker := map[string]bool{}
	for _, root := range markerRoots {
		inMarker[root] = true
	}
	var pools []pool
	for _, root := range roots {
		view, err := rootViewOf(ledger, root, since, until)
		var p pool
		if err == nil {
			p, err = rootPool(view, until)
		}
		if err != nil {
			var ge *GitError
			var me *ManifestError
			if errors.As(err, &ge) || errors.As(err, &me) {
				o.cannot(root + " (" + err.Error() + ")")
				continue
			}
			return nil, err
		}
		pools = append(pools, p)
		if view.fallback && inMarker[root] {
			o.note(prefix+" fallback without ledger for "+root, false)
		}
		for _, n := range p.notes {
			o.note(prefix+" "+root+": "+n, false)
		}
		for _, u := range p.unchecked {
			o.cannot(filepath.Join(root, u.rel) + " (" + u.why + ")")
		}
	}
	return pools, nil
}

func postInState(payload map[string]json.RawMessage, o *Outcome, projectDir, state, key, repoRoot string, until time.Time) error {
	m, _ := readMarker(state, key)
	markerRoots := m.Roots()
	roots, gone, err := postRoots(payload, markerRoots, projectDir, until, o)
	if err != nil {
		var ge *GitError
		if !errors.As(err, &ge) {
			return err
		}
		o.cannot(projectDir + " (" + err.Error() + ")")
		return nil
	}
	for _, root := range gone {
		// Exit 0: the files vanished along with the root.
		o.note(prefix+" "+root+" no longer exists; not checked", false)
	}

	ledger := readLedger(filepath.Join(state, "ledger.json"))
	if ledger.Unreadable && len(roots) > 0 {
		o.note(prefix+" ledger unreadable; fallback for every root", false)
	}
	pools, err := rootPools(roots, ledger, markerRoots, m.since(), until, o)
	if err != nil || len(pools) == 0 {
		return err
	}
	files, replaced, err := claimFunc(state, key, pools)
	if err != nil {
		return err
	}
	if replaced {
		o.note(prefix+" unreadable ledger replaced; a root this call did not touch falls back at its next call", false)
	}
	return dispatch(files, state, key, repoRoot, until, o, len(pools) > 1)
}

func post(payload map[string]json.RawMessage, repoRoot string, o *Outcome) (err error) {
	projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
	if projectDir == "" {
		o.cannot("CLAUDE_PROJECT_DIR is missing, the project roots are unknown")
		return nil
	}
	until := now().Add(postDeadline())
	state, err := StateDir(pyStr(payload["session_id"], ""))
	if err != nil {
		return err
	}
	key, err := MarkerKey(payload)
	if err != nil {
		return err
	}
	// The claims of this post and its marker go, also after the deadline, an error or a panic.
	defer func() {
		if rerr := releaseClaims(state, key); rerr != nil && err == nil {
			err = rerr
		}
		_ = os.Remove(filepath.Join(state, key+".json"))
	}()
	return postInState(payload, o, projectDir, state, key, repoRoot, until)
}

// Post is the PostToolUse(Failure) step: source 1 and 2 per root, filtered on seen, claimed,
// dispatched sorted until the deadline, bundled. It returns the exit code (0 or 2), the JSON
// object for stdout (nil when there is nothing to report) and the text for stderr. repoRoot is the
// lighthouse root whose `.claude/hooks/lighthouse/` holds the gates.
//
// A payload that is not a JSON object, an unexpected error and a panic are COULD NOT CHECK with
// exit 2, never a crash. The event is hook_event_name, PostToolUse by default.
func Post(raw []byte, repoRoot string) (code int, output map[string]any, stderr string) {
	o := &Outcome{}
	payload, ok := decodeObject(raw)
	if !ok {
		o.cannot("the hook payload on stdin is not a JSON object")
		return o.Render("PostToolUse")
	}
	event := "PostToolUse"
	if e, ok := truthyText(payload["hook_event_name"]); ok {
		event = e
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				o.cannot(fmt.Sprintf("internal error in the dispatcher (panic: %v)", r))
			}
		}()
		if err := post(payload, repoRoot, o); err != nil {
			o.cannot(fmt.Sprintf("internal error in the dispatcher (%v)", err))
		}
	}()
	return o.Render(event)
}
