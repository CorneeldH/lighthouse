package collect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Observation is one look at the installed versions, made by the SessionStart hook.
type Observation struct {
	At                time.Time `json:"at"`
	LighthouseVersion string    `json:"lighthouse_version"`
	PackVersion       string    `json:"pack_version"`
}

func sidecarPath(stateDir, sessionID string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) || sessionID == "." || sessionID == ".." {
		return "", fmt.Errorf("sidecar: session id %q is not a file name", sessionID)
	}
	return filepath.Join(stateDir, sessionID+".observations.json"), nil
}

// ReadSidecar returns the observations of a session in file order; a missing file is nil, nil.
func ReadSidecar(stateDir, sessionID string) ([]Observation, error) {
	path, err := sidecarPath(stateDir, sessionID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var obs []Observation
	if err := json.Unmarshal(data, &obs); err != nil {
		return nil, fmt.Errorf("sidecar %s: %w", path, err)
	}
	return obs, nil
}

// AppendSidecar adds o to the end of the session's list. The file is replaced atomically
// (temp file in the same directory, then rename). Concurrent writers for one session are not
// expected: the hook fires once per session start.
func AppendSidecar(stateDir, sessionID string, o Observation) error {
	path, err := sidecarPath(stateDir, sessionID)
	if err != nil {
		return err
	}
	obs, err := ReadSidecar(stateDir, sessionID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(append(obs, o))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(stateDir, sessionID+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// PickObservation returns the first observation at or after w.From and before w.To (an open
// window has no end), or nil.
func PickObservation(obs []Observation, w RunWindow) *Observation {
	for i := range obs {
		o := &obs[i]
		if o.At.Before(w.From) {
			continue
		}
		if !w.To.IsZero() && !o.At.Before(w.To) {
			continue
		}
		return o
	}
	return nil
}

// StateDir is LIGHTHOUSE_STATE_DIR when set, else DispatchStateDir.
func StateDir(getenv func(string) string) (string, error) {
	if d := getenv("LIGHTHOUSE_STATE_DIR"); d != "" {
		return d, nil
	}
	return DispatchStateDir(getenv)
}

// DispatchStateDir is lighthouse/dispatch under XDG_STATE_HOME, or under $HOME/.local/state
// when XDG_STATE_HOME is not an absolute path. tick uses it without the override variable.
func DispatchStateDir(getenv func(string) string) (string, error) {
	base := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("neither XDG_STATE_HOME nor HOME is an absolute path: no place for the run instructions")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "lighthouse", "dispatch"), nil
}
