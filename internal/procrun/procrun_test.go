package procrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"
)

func TestExecReportsCodeTimeoutAndMissing(t *testing.T) {
	ctx := context.Background()
	exec := Exec(time.Second)

	res, err := exec(ctx, "", nil, "sh", "-c", "echo out; echo err >&2; exit 3")
	if err != nil || res != (Result{"out\n", "err\n", 3}) {
		t.Errorf("exit 3: got %+v, %v", res, err)
	}

	_, err = Exec(200*time.Millisecond)(ctx, "", nil, "sleep", "5")
	var timeout *Timeout
	if !errors.As(err, &timeout) {
		t.Errorf("sleep 5: got %v, want *Timeout", err)
	}

	_, err = exec(ctx, "", nil, "no-such-binary-xyz")
	var missing *NotFound
	if !errors.As(err, &missing) || missing.Name != "no-such-binary-xyz" {
		t.Errorf("missing: got %v, want *NotFound", err)
	}

	in := "abc"
	res, err = exec(ctx, "", &in, "cat")
	if err != nil || res.Stdout != "abc" {
		t.Errorf("cat: got %+v, %v", res, err)
	}

	dir := t.TempDir()
	res, err = exec(ctx, dir, nil, "pwd", "-P")
	if err != nil || res.Code != 0 || len(res.Stdout) == 0 {
		t.Errorf("pwd in dir: got %+v, %v", res, err)
	}
}

func TestExecTimeoutKillsGrandchildren(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX process groups to kill a grandchild with; see host-assumptions.md")
	}
	start := time.Now()
	_, err := Exec(200*time.Millisecond)(context.Background(), "", nil, "sh", "-c", "sleep 30 & wait")
	var timeout *Timeout
	if !errors.As(err, &timeout) {
		t.Fatalf("got %v, want *Timeout", err)
	}
	if d := time.Since(start); d > time.Second+500*time.Millisecond {
		t.Errorf("returned after %v, want before the 2 s pipe wait: the grandchild must be killed with its group", d)
	}
}

func TestPyNameTable(t *testing.T) {
	var syntaxErr, typeErr error
	syntaxErr = json.Unmarshal([]byte("{"), new(any))
	typeErr = json.Unmarshal([]byte(`{"a": 1}`), &struct{ A string }{})
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"exit code", &CalledProcessError{Code: 1}, "CalledProcessError"},
		{"timeout", &Timeout{After: time.Second}, "TimeoutExpired"},
		{"not found", &NotFound{Name: "gh"}, "FileNotFoundError"},
		{"invalid json", syntaxErr, "JSONDecodeError"},
		{"missing field", &KeyError{Key: "id"}, "KeyError"},
		{"wrong type", typeErr, "TypeError"},
		{"config", &ConfigError{Msg: "/x/lighthouse.toml:\n  [forge.board]   needs forge.repo"}, "ConfigError: /x/lighthouse.toml: [forge.board] needs forge.repo"},
		{"wrapped", fmt.Errorf("ctx: %w", &Timeout{}), "TimeoutExpired"},
	}
	for _, tt := range tests {
		if got := PyName(tt.err); got != tt.want {
			t.Errorf("%s: PyName = %q, want %q", tt.name, got, tt.want)
		}
	}
}
