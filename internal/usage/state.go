package usage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Paths locates the files of the usage meter.
type Paths struct{ Dir string } // the lighthouse cache directory

// DefaultPaths returns ${XDG_CACHE_HOME:-$HOME/.cache}/lighthouse, as the
// shell guard computes it. A non-empty XDG_CACHE_HOME wins, even a relative
// one. os.UserCacheDir is not used: on macOS it points elsewhere than the guard.
func DefaultPaths() (Paths, error) {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return Paths{Dir: filepath.Join(xdg, "lighthouse")}, nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		return Paths{}, errors.New("neither XDG_CACHE_HOME nor HOME is set")
	}
	return Paths{Dir: filepath.Join(home, ".cache", "lighthouse")}, nil
}

// Cache is the JSON cache of the last measurement.
func (p Paths) Cache() string { return filepath.Join(p.Dir, "usage-window.json") }

// Alarm is the file the shell guard reads.
func (p Paths) Alarm() string { return filepath.Join(p.Dir, "usage-window.alarm") }

// Lock serialises refreshes.
func (p Paths) Lock() string { return filepath.Join(p.Dir, "usage-window.lock") }

// Workdir is the working directory of the CLI call.
func (p Paths) Workdir() string { return filepath.Join(p.Dir, "usage-refresh") }

const (
	schemaVersion = 1
	timeLayout    = "2006-01-02T15:04:05Z"
)

// weekdays is indexed by time.Weekday.
var weekdays = [7]string{"zo", "ma", "di", "wo", "do", "vr", "za"}

// WindowState is one window in the cache.
type WindowState struct {
	Pct        int    `json:"pct"`
	ResetAt    string `json:"reset_at"`    // UTC, "" without a reset
	ResetLocal string `json:"reset_local"` // "14:30" or "do 16:59", "" without a reset
}

// State is the cache file. It is the contract for GET /api/usage.
type State struct {
	Schema      int         `json:"schema"`
	GeneratedAt string      `json:"generated_at"`
	Session     WindowState `json:"session"`
	Week        WindowState `json:"week"`
}

// NewState turns a measurement into the cache form. Reset times are
// formatted in loc.
func NewState(w Windows, now time.Time, loc *time.Location) State {
	return State{
		Schema:      schemaVersion,
		GeneratedAt: now.UTC().Format(timeLayout),
		Session:     windowState(w.Session, loc, false),
		Week:        windowState(w.Week, loc, true),
	}
}

func windowState(w Window, loc *time.Location, withDay bool) WindowState {
	ws := WindowState{Pct: w.Pct}
	if w.Reset.IsZero() {
		return ws
	}
	ws.ResetAt = w.Reset.UTC().Format(timeLayout)
	local := w.Reset.In(loc)
	ws.ResetLocal = local.Format("15:04")
	if withDay {
		ws.ResetLocal = weekdays[local.Weekday()] + " " + ws.ResetLocal
	}
	return ws
}

// ReadState reads the cache. ok is false when the file is absent, unreadable,
// of another schema or has an unparseable generated_at: all count as no cache.
func ReadState(path string) (State, time.Time, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, time.Time{}, false
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil || s.Schema != schemaVersion {
		return State{}, time.Time{}, false
	}
	at, err := time.Parse(timeLayout, s.GeneratedAt)
	if err != nil {
		return State{}, time.Time{}, false
	}
	return s, at, true
}

// WriteState writes the cache atomically.
func WriteState(path string, s State) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

// writeAtomic replaces path with data through a temp file and a rename, so a
// reader sees the old or the new content, never a partial file.
func writeAtomic(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
