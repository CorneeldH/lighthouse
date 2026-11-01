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

// MarkerDir is lighthouse/resume under XDG_DATA_HOME, or under $HOME/.local/share when
// XDG_DATA_HOME is not an absolute path.
func MarkerDir(getenv func(string) string) (string, error) {
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
