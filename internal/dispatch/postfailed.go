package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// postFailedVersion is the version of the post-failed.json note.
const postFailedVersion = 1

// postFailedTime is the format of the at field: milliseconds, UTC.
const postFailedTime = "2006-01-02T15:04:05.000Z"

// PostFailed is the note a failed handoff or question post of a controller run leaves, for the
// controller to repost.
type PostFailed struct {
	At         time.Time `json:"at"`
	Issue      int       `json:"issue"`
	Command    string    `json:"command"`
	Error      string    `json:"error"`
	Body       string    `json:"body"`
	Unreadable bool      `json:"unreadable,omitempty"` // the note did not parse; the other fields are zero
}

// postFailedFile is the note on disk; the field order is the contract.
type postFailedFile struct {
	Version int    `json:"version"`
	At      string `json:"at"`
	Issue   int    `json:"issue"`
	Command string `json:"command"`
	Error   string `json:"error"`
	Body    string `json:"body"`
}

// PostFailedPath is <stateDir>/runs/<sessionID>/post-failed.json. It checks nothing.
func PostFailedPath(stateDir, sessionID string) string {
	return filepath.Join(stateDir, "runs", sessionID, "post-failed.json")
}

// WritePostFailed writes the note of a failed post, replacing an earlier one. The error is
// flattened to one line and the time is written in milliseconds UTC.
func WritePostFailed(stateDir, sessionID string, n PostFailed) error {
	if err := checkFileName(sessionID); err != nil {
		return err
	}
	data, err := json.Marshal(postFailedFile{
		Version: postFailedVersion,
		At:      n.At.UTC().Format(postFailedTime),
		Issue:   n.Issue,
		Command: n.Command,
		Error:   oneLine(n.Error),
		Body:    n.Body,
	})
	if err != nil {
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	path := PostFailedPath(stateDir, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "post-failed-*.tmp")
	if err != nil {
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	return nil
}

// ReadPostFailed reads the note of a run. No note is nil, nil; a note that does not parse is
// &PostFailed{Unreadable: true}. Only a real I/O error or an unsafe session id is an error.
func ReadPostFailed(stateDir, sessionID string) (*PostFailed, error) {
	if err := checkFileName(sessionID); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(PostFailedPath(stateDir, sessionID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dispatch: post-failed note: %w", err)
	}
	return ParsePostFailed(data), nil
}

// ParsePostFailed reads the bytes of a post-failed note. It never returns nil: a note that
// does not parse, has another version or no time is &PostFailed{Unreadable: true}.
func ParsePostFailed(data []byte) *PostFailed {
	var f postFailedFile
	if err := json.Unmarshal(data, &f); err != nil || f.Version != postFailedVersion {
		return &PostFailed{Unreadable: true}
	}
	at, err := time.Parse(time.RFC3339Nano, f.At)
	if err != nil || at.IsZero() {
		return &PostFailed{Unreadable: true}
	}
	return &PostFailed{At: at, Issue: f.Issue, Command: f.Command, Error: f.Error, Body: f.Body}
}
