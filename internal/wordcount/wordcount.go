// Package wordcount counts words the way check 10 of check-instructions.sh does: with `wc -w`.
//
// There is no Go fallback on purpose: strings.Fields and wc disagree on some whitespace, and
// the budget must match the gate.
package wordcount

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ErrNoWC is returned when `wc` is not on PATH.
var ErrNoWC = errors.New("'wc' is missing")

// Count returns the number of words in b according to `wc -w`.
func Count(b []byte) (int, error) {
	path, err := exec.LookPath("wc")
	if err != nil {
		return 0, ErrNoWC
	}
	cmd := exec.Command(path, "-w")
	cmd.Stdin = bytes.NewReader(b)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("wc -w failed: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("wc -w gave %q, not a number", out)
	}
	return n, nil
}
