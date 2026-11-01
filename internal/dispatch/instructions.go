package dispatch

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed instructions/*.md
var instructionsFS embed.FS

// roundLimitQuestion is the at-round-limit setting of the repo's controller config that adds
// the round-limit appendix to the run instructions.
const roundLimitQuestion = "vraag"

// WriteInstructions writes the run instructions of a session to
// <stateDir>/runs/<sessionID>/instructions.md (file 0600, directory 0700) and returns the
// path. With atRoundLimit roundLimitQuestion the round-limit appendix follows the instructions.
func WriteInstructions(stateDir, sessionID, atRoundLimit string) (string, error) {
	if sessionID == "" || sessionID == "." || sessionID == ".." || filepath.Base(sessionID) != sessionID {
		return "", fmt.Errorf("session id %q is no single path element", sessionID)
	}
	text, err := instructionsFS.ReadFile("instructions/run.md")
	if err != nil {
		return "", err
	}
	if atRoundLimit == roundLimitQuestion {
		appendix, err := instructionsFS.ReadFile("instructions/round_limit_question.md")
		if err != nil {
			return "", err
		}
		text = append(text, appendix...)
	}
	dir := filepath.Join(stateDir, "runs", sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "instructions.md")
	if err := writePrivate(path, text); err != nil {
		return "", err
	}
	return path, nil
}

// writePrivate writes data to path with mode 0600, also when path already exists with a
// wider mode.
func writePrivate(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	return errors.Join(werr, f.Chmod(0o600), f.Close())
}
