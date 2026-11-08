package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// wsRecord is one workspace: workspaces/<id>.json.
type wsRecord struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	Branch    string    `json:"branch"`
	Adopted   bool      `json:"adopted"` // a worktree the adapter did not make itself
	CreatedAt time.Time `json:"created_at"`
}

// sessRecord is one launch: sessions/<workspace id>/<key>.json.
type sessRecord struct {
	Key       string    `json:"key"`
	SessionID string    `json:"session_id"`
	Mode      string    `json:"mode"`
	StartedAt time.Time `json:"started_at"`
}

// store holds the records of one repo under StateDir/local/<owner>/<name>.
type store struct{ dir string }

var repoPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// store is the record store of a.Repo. A Repo that is not owner/name, or a relative StateDir,
// is an error: either would put records where no later tick finds them.
func (a *Adapter) store() (store, error) {
	if !filepath.IsAbs(a.StateDir) {
		return store{}, fmt.Errorf("local workplace: state dir %q is not absolute", a.StateDir)
	}
	owner, name, ok := strings.Cut(a.Repo, "/")
	for _, p := range []string{owner, name} {
		if !ok || !repoPart.MatchString(p) || p == "." || p == ".." {
			return store{}, fmt.Errorf("local workplace: repo %q is not owner/name", a.Repo)
		}
	}
	return store{dir: filepath.Join(a.StateDir, "local", owner, name)}, nil
}

func (s store) workspacePath(id string) string {
	return filepath.Join(s.dir, "workspaces", id+".json")
}

func (s store) sessionDir(id string) string { return filepath.Join(s.dir, "sessions", id) }

func (s store) workspace(id string) (wsRecord, bool, error) {
	var r wsRecord
	ok, err := readJSON(s.workspacePath(id), &r)
	return r, ok, err
}

func (s store) workspaces() ([]wsRecord, error) {
	var out []wsRecord
	err := eachJSON(filepath.Join(s.dir, "workspaces"), func(path string) error {
		var r wsRecord
		if _, err := readJSON(path, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func (s store) putWorkspace(r wsRecord) error { return writeJSON(s.workspacePath(r.ID), r) }

func (s store) sessions(id string) ([]sessRecord, error) {
	var out []sessRecord
	err := eachJSON(s.sessionDir(id), func(path string) error {
		var r sessRecord
		if _, err := readJSON(path, &r); err != nil {
			return err
		}
		out = append(out, r)
		return nil
	})
	return out, err
}

func (s store) putSession(id string, r sessRecord) error {
	return writeJSON(filepath.Join(s.sessionDir(id), r.Key+".json"), r)
}

// pidPath holds "<pid> <start>\n", written by the launched child itself.
func (s store) pidPath(id, key string) string { return filepath.Join(s.sessionDir(id), key+".pid") }

// logPath holds stdout and stderr of one launch.
func (s store) logPath(key string) string { return filepath.Join(s.dir, "logs", key+".log") }

// ensureLogs makes the logs directory: the launch redirects into it and creates no directory.
func (s store) ensureLogs() error { return os.MkdirAll(filepath.Join(s.dir, "logs"), 0o700) }

// pid reads the pid file of a launch. A missing file is ok false; one that does not hold
// two numbers is an error.
func (s store) pid(id, key string) (pid int, start uint64, ok bool, err error) {
	path := s.pidPath(id, key)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	f := strings.Fields(string(b))
	if len(f) != 2 {
		return 0, 0, false, fmt.Errorf("pid file %s: %q is not \"<pid> <start>\"", path, b)
	}
	pid, perr := strconv.Atoi(f[0])
	start, serr := strconv.ParseUint(f[1], 10, 64)
	if perr != nil || serr != nil || pid <= 0 {
		return 0, 0, false, fmt.Errorf("pid file %s: %q is not \"<pid> <start>\"", path, b)
	}
	return pid, start, true, nil
}

// dropSession removes the record, pid file and log of one launch; a missing one is fine.
func (s store) dropSession(id, key string) error {
	for _, p := range []string{filepath.Join(s.sessionDir(id), key+".json"), s.pidPath(id, key), s.logPath(key)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// dropWorkspace removes every launch of the workspace, a pid file without record included,
// then the workspace record.
func (s store) dropWorkspace(id string) error {
	entries, err := os.ReadDir(s.sessionDir(id))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		for _, ext := range []string{".json", ".pid"} {
			if key, ok := strings.CutSuffix(name, ext); ok && !strings.HasPrefix(name, ".") {
				if err := s.dropSession(id, key); err != nil {
					return err
				}
			}
		}
	}
	if err := os.RemoveAll(s.sessionDir(id)); err != nil {
		return err
	}
	if err := os.Remove(s.workspacePath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func readJSON(path string, v any) (bool, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return false, fmt.Errorf("record %s: %w", path, err)
	}
	return true, nil
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return workplace.WriteFileAtomic(path, append(b, '\n'))
}

// eachJSON calls fn, in name order, on every record in dir; a missing dir holds none. A
// temporary file of WriteFileAtomic starts with a dot and is skipped.
func eachJSON(dir string, fn func(path string) error) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries { // os.ReadDir sorts by name
		if n := e.Name(); strings.HasSuffix(n, ".json") && !strings.HasPrefix(n, ".") && !e.IsDir() {
			if err := fn(filepath.Join(dir, n)); err != nil {
				return err
			}
		}
	}
	return nil
}
