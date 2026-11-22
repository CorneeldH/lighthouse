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
	// TranscriptUnmeasurable is true when the workplace should hold the transcript but could not
	// show it (a container run whose transcript is not on this host, #514). A forge stand done
	// then proves nothing: claude may have ended its turn for a background task.
	TranscriptUnmeasurable bool        `json:"transcript_unmeasurable,omitempty"`
	Permission             *Permission `json:"permission,omitempty"`
	PostFailed             *PostFailed `json:"post_failed,omitempty"` // the note of a failed post
}

// Permission is the permission prompt the session of a run last showed, as the
// permission-check hook wrote it.
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

// permissionVersion is the version of the note the permission-check hook writes.
const permissionVersion = 1

// PermissionPath is <stateDir>/runs/<sessionID>/permission.json, the note ReadPermission
// reads. It checks nothing.
func PermissionPath(stateDir, sessionID string) string {
	return filepath.Join(stateDir, "runs", sessionID, "permission.json")
}

// ReadPermission reads <stateDir>/runs/<sessionID>/permission.json: the note the
// permission-check hook writes for the run whose LIGHTHOUSE_CONTROLLER_RUN is sessionID. No
// note is nil, nil; a note that does not parse, has another version or no time is an error.
func ReadPermission(stateDir, sessionID string) (*Permission, error) {
	if err := checkFileName(sessionID); err != nil {
		return nil, err
	}
	path := PermissionPath(stateDir, sessionID)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch: permission note: %w", err)
	}
	p, err := ParsePermission(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// ParsePermission reads the bytes of a permission note, as the permission-check hook writes
// it, on this host or on a node. A note that does not parse, has another version or no time is
// an error.
func ParsePermission(data []byte) (*Permission, error) {
	var note struct {
		Version int `json:"version"`
		Permission
	}
	if err := json.Unmarshal(data, &note); err != nil {
		return nil, fmt.Errorf("dispatch: permission note: %w", err)
	}
	if note.Version != permissionVersion {
		return nil, fmt.Errorf("dispatch: permission note: version %d, want %d", note.Version, permissionVersion)
	}
	if note.At.IsZero() {
		return nil, errors.New("dispatch: permission note: no time")
	}
	return &note.Permission, nil
}

// checkFileName refuses a session id that would name another file than the run's own.
func checkFileName(sessionID string) error {
	if sessionID == "" || sessionID == "." || sessionID == ".." || filepath.Base(sessionID) != sessionID {
		return fmt.Errorf("dispatch: session id %q is no file name", sessionID)
	}
	return nil
}
