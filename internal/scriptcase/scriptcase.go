// Package scriptcase replays the `gh` calls of a skill-command fixture
// (tests/pack/fixtures/scripts/<command>/cases.json). The Python runner
// (tests/pack/test_script_cases.py) reads the same file with a fake `gh` on the PATH, so one
// fixture proves the Python script and the Go command give the same output.
//
// It is for tests only: it takes a testing.TB and fails it on any error.
package scriptcase

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// Call is one scripted `gh` call: the argv it must have, and what it answers.
type Call struct {
	Argv   []string `json:"argv"`
	Stdin  *string  `json:"stdin"`
	Stdout string   `json:"stdout"`
	Stderr string   `json:"stderr"`
	Code   int      `json:"code"`
	// Timeout makes Replay answer with a *procrun.Timeout instead of the output.
	Timeout bool `json:"timeout,omitempty"`
}

// Expect is what a case demands of the command. StdoutContains and StderrContains are for help
// and usage cases, where the text of argparse and cobra differ.
type Expect struct {
	Stdout         string            `json:"stdout"`
	Stderr         string            `json:"stderr"`
	StdoutContains []string          `json:"stdout_contains,omitempty"`
	StderrContains []string          `json:"stderr_contains,omitempty"`
	Code           int               `json:"code"`
	Files          map[string]string `json:"files,omitempty"`
}

// Case is one fixture case.
type Case struct {
	Name   string            `json:"name"`
	Argv   []string          `json:"argv"`
	Config string            `json:"config"`
	Env    map[string]string `json:"env"`
	Setup  []map[string]any  `json:"setup,omitempty"`
	Calls  []Call            `json:"calls"`
	// ForgeCalls are the requests to the forge API, served by ReplayForge; a case that has
	// them scripts no `gh`.
	ForgeCalls []ForgeCall `json:"forge_calls,omitempty"`
	Expect     Expect      `json:"expect"`
	// Only restricts a case to one implementation ("go"); empty runs on both.
	Only string `json:"only,omitempty"`
}

// Load reads the cases of path and fails t when the file is missing or malformed.
func Load(t testing.TB, path string) []Case {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return cases
}

// gitTimeout bounds a real `git` call.
const gitTimeout = 60 * time.Second

// Replay answers the `gh` calls in order. Any other call than the next one, and any call after
// the list ran out, fails t with the expected and the actual argv; calls left at the end of the
// test fail it too. `git` is never scripted: it runs for real through procrun.Exec. The dir of a
// call does not count in the comparison.
func Replay(t testing.TB, calls []Call) procrun.Func {
	t.Helper()
	var (
		mu   sync.Mutex
		next int
	)
	realExec := procrun.Exec(gitTimeout)
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		if next < len(calls) {
			t.Errorf("replay: %d of %d scripted calls were not made; next expected: %v", len(calls)-next, len(calls), calls[next].Argv)
		}
	})
	return func(ctx context.Context, dir string, stdin *string, name string, args ...string) (procrun.Result, error) {
		if name == "git" {
			return realExec(ctx, dir, stdin, name, args...)
		}
		argv := append([]string{name}, args...)
		mu.Lock()
		defer mu.Unlock()
		if next >= len(calls) {
			t.Errorf("replay: unexpected call %v: all %d scripted calls were used", argv, len(calls))
			return procrun.Result{}, fmt.Errorf("unexpected call %v", argv)
		}
		want := calls[next]
		if !reflect.DeepEqual(want.Argv, argv) {
			t.Errorf("replay: call %d: expected %v, got %v", next+1, want.Argv, argv)
			return procrun.Result{}, fmt.Errorf("unexpected call %v", argv)
		}
		if want.Stdin != nil && (stdin == nil || *stdin != *want.Stdin) {
			t.Errorf("replay: call %d %v: expected stdin %q, got %v", next+1, argv, *want.Stdin, stdin)
			return procrun.Result{}, fmt.Errorf("unexpected stdin for %v", argv)
		}
		next++
		if want.Timeout {
			return procrun.Result{}, &procrun.Timeout{}
		}
		return procrun.Result{Stdout: want.Stdout, Stderr: want.Stderr, Code: want.Code}, nil
	}
}
