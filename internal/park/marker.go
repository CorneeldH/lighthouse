// Package park reads and writes what the usage guard leaves behind: the resume markers of
// parked sessions and the PAUSE flag. The contract is usage-gate.md (Contract). Package park is a
// leaf: it imports nothing from the dispatcher.
package park

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Marker is one resume marker, <session_id>.json in the marker directory.
type Marker struct {
	SessionID  string `json:"session_id"`
	Cwd        string `json:"cwd"`
	Window     string `json:"window"`
	ResetAt    string `json:"reset_at"`
	CreatedAt  string `json:"created_at"`
	Source     string `json:"source"`
	Controller bool   `json:"controller,omitempty"`
	Pause      bool   `json:"pause,omitempty"`
}

var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Reset is reset_at as a time; false when it is not RFC 3339.
func (m Marker) Reset() (time.Time, bool) { return parseRFC3339(m.ResetAt) }

// Created is created_at as a time; false when it is not RFC 3339.
func (m Marker) Created() (time.Time, bool) { return parseRFC3339(m.CreatedAt) }

func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// Key is the dedup key of a resume of this marker. Every parking writes a new created_at, so
// every parking has its own key. Nobody parses it: compare Key values.
func (m Marker) Key() string {
	return "usage:" + m.SessionID + "@" + m.CreatedAt + ":" + m.ResetAt
}

// valid reports whether the marker counts for file <name>.json: the session_id fits the
// pattern and equals the file name, and both times read.
func (m Marker) valid(name string) bool {
	if !sessionIDPattern.MatchString(m.SessionID) || m.SessionID != name {
		return false
	}
	_, okReset := m.Reset()
	_, okCreated := m.Created()
	return okReset && okCreated
}

// ControllerResumeDir is <LIGHTHOUSE_STATE_DIR>/resume for a controller run: the one directory a
// run in a container shares with the host, whatever its HOME is (#513). ok is false outside a
// controller run or when LIGHTHOUSE_STATE_DIR is not an absolute path.
func ControllerResumeDir(getenv func(string) string) (dir string, ok bool) {
	state := getenv("LIGHTHOUSE_STATE_DIR")
	if !ControllerRun(getenv) || !filepath.IsAbs(state) {
		return "", false
	}
	return StateResumeDir(state), true
}

// StateResumeDir is the resume directory under the dispatcher state directory stateDir.
func StateResumeDir(stateDir string) string { return filepath.Join(stateDir, "resume") }

// MarkerDir is where this process writes its resume markers: ControllerResumeDir in a controller
// run, else lighthouse/resume under XDG_DATA_HOME, or under $HOME/.local/share when XDG_DATA_HOME
// is not an absolute path. The host reads both (HostMarkerDirs).
func MarkerDir(getenv func(string) string) (string, error) {
	if dir, ok := ControllerResumeDir(getenv); ok {
		return dir, nil
	}
	return userMarkerDir(getenv)
}

func userMarkerDir(getenv func(string) string) (string, error) {
	base := getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("neither XDG_DATA_HOME nor HOME is an absolute path: no place for the resume markers")
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "lighthouse", "resume"), nil
}

// HostMarkerDirs are the directories the controller reads markers from: the user's own
// (lighthouse/resume under XDG_DATA_HOME or HOME) and StateResumeDir(stateDir), where a run in a
// container leaves its marker. stateDir "" leaves out the second.
func HostMarkerDirs(getenv func(string) string, stateDir string) ([]string, error) {
	user, err := userMarkerDir(getenv)
	if err != nil {
		return nil, err
	}
	dirs := []string{user}
	if stateDir != "" {
		if d := StateResumeDir(stateDir); d != user {
			dirs = append(dirs, d)
		}
	}
	return dirs, nil
}

// readMarker reads <dir>/<id>.json; ok is false for a file that is missing, unreadable, not
// JSON or not a valid marker.
func readMarker(dir, id string) (Marker, bool) {
	b, err := os.ReadFile(filepath.Join(dir, id+".json"))
	if err != nil {
		return Marker{}, false
	}
	var m Marker
	if json.Unmarshal(b, &m) != nil || !m.valid(id) {
		return Marker{}, false
	}
	return m, true
}

// ReadMarkers returns the valid markers in dir, sorted by SessionID. It skips every other file
// without error; a missing dir gives nil, nil.
func ReadMarkers(dir string) ([]Marker, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var markers []Marker
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !sessionIDPattern.MatchString(id) {
			continue
		}
		if m, ok := readMarker(dir, id); ok {
			markers = append(markers, m)
		}
	}
	slices.SortFunc(markers, func(a, b Marker) int { return strings.Compare(a.SessionID, b.SessionID) })
	return markers, nil
}

// RemoveMarker deletes the marker of sessionID, but only while its Key still equals key: the
// guard may have parked the session again since. A missing marker is not an error.
func RemoveMarker(dir, sessionID, key string) error {
	if !sessionIDPattern.MatchString(sessionID) {
		return fmt.Errorf("invalid session id %q", sessionID)
	}
	m, ok := readMarker(dir, sessionID)
	if !ok || m.Key() != key {
		return nil
	}
	err := os.Remove(filepath.Join(dir, sessionID+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ReadMarkersIn is ReadMarkers over several directories, sorted by SessionID. A session with a
// marker in more than one directory keeps the one with the latest created_at.
func ReadMarkersIn(dirs []string) ([]Marker, error) {
	latest := map[string]Marker{}
	for _, dir := range dirs {
		ms, err := ReadMarkers(dir)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			if old, ok := latest[m.SessionID]; !ok || m.CreatedAt > old.CreatedAt {
				latest[m.SessionID] = m
			}
		}
	}
	out := make([]Marker, 0, len(latest))
	for _, m := range latest {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b Marker) int { return strings.Compare(a.SessionID, b.SessionID) })
	return out, nil
}

// UnreadableIn lists the session ids that have a <session_id>.json in dirs which is no valid
// marker (unreadable, not JSON, wrong content): the usage guard parked that session, but the
// controller cannot tell until when. A session that has a valid marker in any dir is not listed.
// Sorted; a missing dir lists none.
func UnreadableIn(dirs []string) []string {
	valid := map[string]bool{}
	bad := map[string]bool{}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			id, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok || e.IsDir() || !sessionIDPattern.MatchString(id) {
				continue
			}
			if _, ok := readMarker(dir, id); ok {
				valid[id] = true
			} else {
				bad[id] = true
			}
		}
	}
	var out []string
	for id := range bad {
		if !valid[id] {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// RemoveMarkerIn is RemoveMarker in every directory of dirs.
func RemoveMarkerIn(dirs []string, sessionID, key string) error {
	var errs []error
	for _, dir := range dirs {
		errs = append(errs, RemoveMarker(dir, sessionID, key))
	}
	return errors.Join(errs...)
}
