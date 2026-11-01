package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// RunState is what a tick measured of one open run besides its stand. A run that is in
// RepoInput.Runs was measured; a field left zero found nothing.
type RunState struct {
	Gone       bool                  `json:"gone,omitempty"`       // the workplace no longer knows the workspace
	SessionAt  time.Time             `json:"session_at,omitzero"`  // forge updated_at of the run's session
	Transcript *workplace.Transcript `json:"transcript,omitempty"` // nil: not measurable
	Permission *Permission           `json:"permission,omitempty"`
	Parked     bool                  `json:"parked,omitempty"` // a resume marker for the run's session
}

// Permission is the permission prompt the session of a run last showed, as the
// permission-note hook wrote it.
type Permission struct {
	At     time.Time `json:"at"`
	Tool   string    `json:"tool"`
	Path   string    `json:"path"`
	Thread string    `json:"thread"` // "" the main transcript, else the subagent's file stem
}

// Duration is a time.Duration that reads and writes as text ("1h").
type Duration time.Duration

// MarshalText writes d as time.Duration.String does.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

// UnmarshalText reads d with time.ParseDuration.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("dispatch: duration %q: %w", b, err)
	}
	*d = Duration(v)
	return nil
}

// permissionVersion is the version of the note the permission-note hook writes.
const permissionVersion = 1

// ReadPermission reads <stateDir>/runs/<sessionID>/permission.json: the note the
// permission-note hook writes for the run whose LIGHTHOUSE_CONTROLLER_RUN is sessionID. No
// note is nil, nil; a note that does not parse, has another version or no time is an error.
func ReadPermission(stateDir, sessionID string) (*Permission, error) {
	if err := checkFileName(sessionID); err != nil {
		return nil, err
	}
	path := filepath.Join(stateDir, "runs", sessionID, "permission.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch: permission note: %w", err)
	}
	var note struct {
		Version int `json:"version"`
		Permission
	}
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("dispatch: permission note %s: %w", path, err)
	}
	if note.Version != permissionVersion {
		return nil, fmt.Errorf("dispatch: permission note %s: version %d, want %d", path, note.Version, permissionVersion)
	}
	if note.At.IsZero() {
		return nil, fmt.Errorf("dispatch: permission note %s: no time", path)
	}
	return &note.Permission, nil
}

// Parked says whether <resumeDir>/<sessionID>.json, the resume marker of the usage guard, is a
// regular file. A missing one, a symlink or a directory is no marker.
func Parked(resumeDir, sessionID string) (bool, error) {
	if err := checkFileName(sessionID); err != nil {
		return false, err
	}
	st, err := os.Lstat(filepath.Join(resumeDir, sessionID+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("dispatch: resume marker: %w", err)
	}
	return st.Mode().IsRegular(), nil
}

// checkFileName refuses a session id that would name another file than the run's own.
func checkFileName(sessionID string) error {
	if sessionID == "" || sessionID == "." || sessionID == ".." || filepath.Base(sessionID) != sessionID {
		return fmt.Errorf("dispatch: session id %q is no file name", sessionID)
	}
	return nil
}
