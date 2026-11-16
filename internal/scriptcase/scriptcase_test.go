package scriptcase

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/procrun"
)

// recorder is a testing.TB that records failures instead of failing the real test.
type recorder struct {
	testing.TB
	errors   []string
	cleanups []func()
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, sprintf(format, args...))
}
func (r *recorder) Cleanup(f func()) { r.cleanups = append(r.cleanups, f) }
func (r *recorder) finish() {
	for _, f := range r.cleanups {
		f()
	}
}

func TestReplayAnswersInOrder(t *testing.T) {
	in := "payload"
	calls := []Call{
		{Argv: []string{"gh", "a"}, Stdout: "A\n", Code: 0},
		{Argv: []string{"gh", "b"}, Stdin: &in, Stderr: "boom", Code: 4},
	}
	r := &recorder{TB: t}
	run := Replay(r, calls)
	got, err := run(context.Background(), "some/dir", nil, "gh", "a")
	if err != nil || got != (procrun.Result{Stdout: "A\n"}) {
		t.Errorf("call 1: got %+v, %v", got, err)
	}
	got, err = run(context.Background(), "", &in, "gh", "b")
	if err != nil || got != (procrun.Result{Stderr: "boom", Code: 4}) {
		t.Errorf("call 2: got %+v, %v", got, err)
	}
	r.finish()
	if len(r.errors) != 0 {
		t.Errorf("unexpected failures: %v", r.errors)
	}
}

func TestReplayFailsOnWrongOrderExtraAndMissingCalls(t *testing.T) {
	calls := []Call{{Argv: []string{"gh", "a"}}, {Argv: []string{"gh", "b"}}}

	r := &recorder{TB: t}
	run := Replay(r, calls)
	if _, err := run(context.Background(), "", nil, "gh", "b"); err == nil {
		t.Error("wrong order: want an error for the code under test")
	}
	if len(r.errors) != 1 || r.errors[0] != "replay: call 1: expected [gh a], got [gh b]" {
		t.Errorf("wrong order: failures = %q", r.errors)
	}

	r = &recorder{TB: t}
	run = Replay(r, calls[:1])
	_, _ = run(context.Background(), "", nil, "gh", "a")
	_, _ = run(context.Background(), "", nil, "gh", "extra")
	if len(r.errors) != 1 || r.errors[0] != "replay: unexpected call [gh extra]: all 1 scripted calls were used" {
		t.Errorf("extra call: failures = %q", r.errors)
	}

	r = &recorder{TB: t}
	_ = Replay(r, calls)
	r.finish()
	if len(r.errors) != 1 {
		t.Errorf("unused calls: failures = %q", r.errors)
	}
}

func TestReplayTimeoutAndRealGit(t *testing.T) {
	r := &recorder{TB: t}
	run := Replay(r, []Call{{Argv: []string{"gh", "slow"}, Timeout: true}})
	_, err := run(context.Background(), "", nil, "gh", "slow")
	var timeout *procrun.Timeout
	if !errors.As(err, &timeout) {
		t.Errorf("got %v, want *procrun.Timeout", err)
	}
	res, err := run(context.Background(), t.TempDir(), nil, "git", "--version")
	if err != nil || res.Code != 0 {
		t.Errorf("git runs for real: got %+v, %v", res, err)
	}
	r.finish()
	if len(r.errors) != 0 {
		t.Errorf("unexpected failures: %v", r.errors)
	}
}

func TestSelfCasesLoad(t *testing.T) {
	cases := Load(t, "../../tests/pack/fixtures/scripts/_self/cases.json")
	if len(cases) != 1 || cases[0].Name != "test_fake_gh_replays_in_order" || len(cases[0].Calls) != 2 {
		t.Errorf("cases = %+v", cases)
	}
}

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }
