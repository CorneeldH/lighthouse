package kennforge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const (
	// mirrorRoot is the directory under the state dir that holds the transcript mirrors of runs
	// on other nodes: <state>/mirror/<node_id>/<session>/.
	mirrorRoot = "mirror"
	// mirrorLock is the lock file in a session's mirror dir. Its name is no transcript file, so
	// workplace.ReadTranscript never reads it.
	mirrorLock = ".lock"
	// lockTries bounds how often nodeTranscript takes the lock again after RemoveMirror took the
	// mirror away under it.
	lockTries = 3
)

// safeNodeID is a node id that is one path component: no separator, no "." or "..".
var safeNodeID = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// checkMirror checks that node id and session id each make one safe path component.
func checkMirror(nodeID, sessionID string) error {
	if !safeNodeID.MatchString(nodeID) {
		return fmt.Errorf("transcript mirror: unsafe node id %q", nodeID)
	}
	if !workplace.SafeSession.MatchString(sessionID) {
		return fmt.Errorf("transcript mirror: unsafe session id %q", sessionID)
	}
	return nil
}

func mirrorPath(stateDir, nodeID, sessionID string) string {
	return filepath.Join(stateDir, mirrorRoot, nodeID, sessionID)
}

// mirrorDir is the mirror of session sessionID on a.Node: <StateDir>/mirror/<node_id>/<session>/.
// The caller checks both names with checkMirror first.
func (a *Adapter) mirrorDir(sessionID string) string {
	return mirrorPath(a.StateDir, a.Node, sessionID)
}

// nodeTranscript is Transcript on another node. The tick keeps a mirror of the session's
// transcript files and asks serve, per file, only for the bytes past the mirror's length; it
// appends them, replaces a file the node answers whole (From 0: shorter on the node, rotated or
// a new session with the same id), drops a file the node no longer has, and then reads the
// mirror with workplace.ReadTranscript, as on self. A lock on the mirror dir spans the lengths,
// the write and the read: lighthouse run measures without tick.lock, and two fetches must never
// append the same delta. A session without a transcript on the node wraps fs.ErrNotExist; an
// unreachable node, a 5xx, a 401 or 403, or an answer that does not fit the mirror is
// ErrNotMeasurable and ErrUnanswered: the run is not measurable this tick, never silent. A
// transcript without a timestamp is ErrNotMeasurable alone, as on self.
func (a *Adapter) nodeTranscript(ctx context.Context, n *NodeClient, workspaceID, sessionID string) (workplace.Transcript, error) {
	dir, release, err := a.syncMirror(ctx, n, workspaceID, sessionID)
	if err != nil {
		return workplace.Transcript{}, err
	}
	defer release()
	return workplace.ReadTranscript(dir, sessionID)
}

// syncMirror brings the mirror of the session on a.Node up to date, as nodeTranscript describes,
// and returns its dir. The lock on the mirror dir stays held until release is called, so the
// caller reads the files without a concurrent fetch appending to them; release is nil after an
// error. The errors are those of nodeTranscript.
func (a *Adapter) syncMirror(ctx context.Context, n *NodeClient, workspaceID, sessionID string) (dir string, release func(), err error) {
	if err := checkMirror(a.Node, sessionID); err != nil {
		return "", nil, fmt.Errorf("transcript in workspace %s: %w", workspaceID, err)
	}
	if a.StateDir == "" {
		return "", nil, fmt.Errorf("transcript of %s on node %s: no state dir for the mirror", sessionID, a.Node)
	}
	dir = a.mirrorDir(sessionID)
	lock, err := lockMirror(dir)
	if err != nil {
		return "", nil, fmt.Errorf("transcript of %s on node %s: %w", sessionID, a.Node, err)
	}
	fail := func(err error) (string, func(), error) {
		_ = lock.Close()
		return "", nil, err
	}
	have, err := mirrorLengths(dir, sessionID)
	if err != nil {
		return fail(fmt.Errorf("transcript of %s on node %s: mirror: %w", sessionID, a.Node, err))
	}
	deltas, err := n.Transcript(ctx, workspaceID, sessionID, a.Profile, have)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Errorf("transcript of %s on node %s: %w", sessionID, a.Node, err))
		}
		return fail(fmt.Errorf("transcript of %s on node %s: %w: %w", sessionID, a.Node, notMeasurable(err), workplace.ErrUnanswered))
	}
	if err := applyDeltas(dir, have, deltas); err != nil {
		if errors.Is(err, workplace.ErrNotMeasurable) { // an answer that does not fit the mirror
			err = fmt.Errorf("%w: %w", err, workplace.ErrUnanswered)
		}
		return fail(fmt.Errorf("transcript of %s on node %s: mirror: %w", sessionID, a.Node, err))
	}
	return dir, func() { _ = lock.Close() }, nil
}

// lockMirror creates the mirror dir (0700) and takes the lock in it, waiting for a holder. A
// lock on a file RemoveMirror removed while this one waited is taken again on a fresh dir.
func lockMirror(dir string) (*os.File, error) {
	path := filepath.Join(dir, mirrorLock)
	for range lockTries {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		f, err := flock.Lock(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		held, herr := f.Stat()
		now, nerr := os.Stat(path)
		if herr == nil && nerr == nil && os.SameFile(held, now) {
			return f, nil
		}
		_ = f.Close()
	}
	return nil, fmt.Errorf("lock %s: the mirror was removed %d times while waiting", path, lockTries)
}

// mirrorLengths is the length of each transcript file of the session in the mirror, by its name
// relative to the mirror dir with slashes, as workplace.ReadDelta names them.
func mirrorLengths(dir, sessionID string) (map[string]int64, error) {
	have := map[string]int64{}
	st, err := os.Lstat(filepath.Join(dir, sessionID+".jsonl"))
	switch {
	case err == nil && st.Mode().IsRegular():
		have[sessionID+".jsonl"] = st.Size()
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return nil, err
	}
	subs, err := filepath.Glob(filepath.Join(dir, sessionID, "subagents", "*.jsonl"))
	if err != nil {
		return nil, err
	}
	for _, p := range subs {
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if st.Mode().IsRegular() {
			have[sessionID+"/subagents/"+filepath.Base(p)] = st.Size()
		}
	}
	return have, nil
}

// applyDeltas brings the mirror to the node's files. A delta from the mirror's length is
// appended; one from 0 replaces the file. Any other offset leaves a hole or overlap in the
// mirror: then nothing is written and the answer is ErrNotMeasurable. A file of the mirror the
// node no longer answers is removed.
func applyDeltas(dir string, have map[string]int64, deltas []workplace.FileDelta) error {
	seen := map[string]bool{}
	for _, d := range deltas {
		if seen[d.Name] {
			return fmt.Errorf("node answered %s twice: %w", d.Name, workplace.ErrNotMeasurable)
		}
		seen[d.Name] = true
		if d.From != 0 && d.From != have[d.Name] {
			return fmt.Errorf("node answered %s from %d, the mirror holds %d bytes: %w",
				d.Name, d.From, have[d.Name], workplace.ErrNotMeasurable)
		}
	}
	for _, d := range deltas {
		dst := filepath.Join(dir, filepath.FromSlash(d.Name))
		var err error
		if d.From == 0 {
			err = replaceFile(dst, d.Data)
		} else {
			err = appendFile(dst, d.Data)
		}
		if err != nil {
			return err
		}
	}
	for name := range have {
		if !seen[name] {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(name))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// replaceFile writes data as the whole of dst (0600) through a temporary file in its dir, so a
// failed write leaves the old mirror file whole.
func replaceFile(dst string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".replace-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// appendFile appends data to dst. A write that fails halfway still leaves a prefix of the
// node's file: the next fetch asks from its length.
func appendFile(dst string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// RemoveMirror removes the transcript mirror of session sessionID on node nodeID under
// stateDir, under its lock; a missing mirror is no error. It removes the node's dir as well
// once that is empty.
func RemoveMirror(stateDir, nodeID, sessionID string) error {
	if err := checkMirror(nodeID, sessionID); err != nil {
		return err
	}
	dir := mirrorPath(stateDir, nodeID, sessionID)
	lock, err := flock.Lock(filepath.Join(dir, mirrorLock))
	if errors.Is(err, fs.ErrNotExist) {
		return nil // no mirror dir
	}
	if err != nil {
		return fmt.Errorf("remove transcript mirror %s: %w", dir, err)
	}
	err = os.RemoveAll(dir)
	_ = lock.Close()
	if err != nil {
		return fmt.Errorf("remove transcript mirror %s: %w", dir, err)
	}
	_ = os.Remove(filepath.Dir(dir)) // only when empty; another session may still be mirrored
	return nil
}

// SweepMirrors removes every transcript mirror under stateDir that keep does not keep: the
// mirrors of runs that closed while no RemoveMirror ran. An entry that is no safe node or
// session name is no mirror of lighthouse and stays. No mirror dir is nothing to sweep.
func SweepMirrors(stateDir string, keep func(nodeID, sessionID string) bool) error {
	root := filepath.Join(stateDir, mirrorRoot)
	nodes, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sweep transcript mirrors: %w", err)
	}
	var errs []error
	for _, n := range nodes {
		if !n.IsDir() || !safeNodeID.MatchString(n.Name()) {
			continue
		}
		sessions, err := os.ReadDir(filepath.Join(root, n.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("sweep transcript mirrors: %w", err))
			continue
		}
		for _, s := range sessions {
			if !s.IsDir() || !workplace.SafeSession.MatchString(s.Name()) || keep(n.Name(), s.Name()) {
				continue
			}
			if err := RemoveMirror(stateDir, n.Name(), s.Name()); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
