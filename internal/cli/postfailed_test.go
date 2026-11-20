package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

var noteTime = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

func noteEnvOf(stateDir, run string) func(string) string {
	return func(k string) string {
		return map[string]string{"LIGHTHOUSE_CONTROLLER_RUN": run, "LIGHTHOUSE_STATE_DIR": stateDir}[k]
	}
}

func TestNotePostFailedOutsideControllerRun(t *testing.T) {
	for name, run := range map[string]string{"missing": "", "no uuid": "abc"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var errOut strings.Builder
			notePostFailed(noteEnvOf(dir, run), noteTime, &errOut, 7, "ask", "b", errors.New("x"))
			if ents, _ := os.ReadDir(dir); len(ents) != 0 || errOut.Len() != 0 {
				t.Errorf("entries %v, stderr %q; want nothing", ents, errOut.String())
			}
		})
	}
	t.Run("no state dir", func(t *testing.T) {
		var errOut strings.Builder
		notePostFailed(noteEnvOf("", askSession), noteTime, &errOut, 7, "ask", "b", errors.New("x"))
		if errOut.Len() != 0 {
			t.Errorf("stderr %q, want empty", errOut.String())
		}
	})
}

func TestNotePostFailedCouldNotCheck(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var errOut strings.Builder
	notePostFailed(noteEnvOf(file, askSession), noteTime, &errOut, 7, "ask", "b", errors.New("x"))
	got := errOut.String()
	if !strings.HasPrefix(got, "lighthouse: post-failed note: COULD NOT CHECK — ") || strings.Count(got, "\n") != 1 {
		t.Errorf("stderr = %q, want one COULD NOT CHECK line", got)
	}
}

// commentEnv is writeEnv in a controller run with state dir dir.
func commentEnv(f forge.Forge, dir string) *forgeEnv {
	e := writeEnv(f)
	e.getenv = noteEnvOf(dir, askSession)
	e.now = func() time.Time { return noteTime }
	return e
}

func TestForgeIssueCommentFailureWritesNote(t *testing.T) {
	dir := t.TempDir()
	body := "the handoff\n"
	args := []string{"issue", "comment", "121", "--body-file", bodyFile(t, body)}
	plain, _, plainErr := runForgeEnv(writeEnv(&writeForge{err: errors.New("rate limit")}), args...)
	code, _, errOut := runForgeEnv(commentEnv(&writeForge{err: errors.New("rate limit")}, dir), args...)
	if code != 1 || code != plain || errOut != plainErr {
		t.Errorf("code %d stderr %q; without a note %d %q", code, errOut, plain, plainErr)
	}
	n, err := dispatch.ReadPostFailed(dir, askSession)
	if err != nil || n == nil {
		t.Fatalf("note %v, err %v", n, err)
	}
	if n.Issue != 121 || n.Command != "forge issue comment" || n.Body != body || !strings.Contains(n.Error, "rate limit") || !n.At.Equal(noteTime) {
		t.Errorf("note = %+v", n)
	}
}

func TestForgeIssueCommentOpenFailureWritesNote(t *testing.T) {
	dir := t.TempDir()
	env := commentEnv(nil, dir)
	env.open = func(context.Context) (forge.Forge, error) { return nil, errors.New("no token") }
	code, _, errOut := runForgeEnv(env, "issue", "comment", "121", "--body-file", bodyFile(t, "hand\n"))
	if code != 1 || errOut != "lighthouse forge issue comment: no token\n" {
		t.Errorf("code %d stderr %q", code, errOut)
	}
	n, _ := dispatch.ReadPostFailed(dir, askSession)
	if n == nil || n.Body != "hand\n" || n.Error != "no token" {
		t.Errorf("note = %+v", n)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestForgeIssueCommentStdoutFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	env := commentEnv(&writeForge{}, dir)
	env.out = failWriter{}
	code, _, _ := runForgeEnv(env, "issue", "comment", "121", "--body-file", bodyFile(t, "x\n"))
	if code != 1 {
		t.Errorf("code %d, want 1", code)
	}
	if n, _ := dispatch.ReadPostFailed(dir, askSession); n != nil {
		t.Errorf("note = %+v, want none", n)
	}
}

func TestForgeIssueCommentUsageErrorWritesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"issue", "comment", "121", "--body-file", filepath.Join(dir, "missing")},
		{"issue", "comment", "x", "--body-file", bodyFile(t, "x\n")},
	} {
		if code, _, _ := runForgeEnv(commentEnv(&writeForge{err: errors.New("boom")}, dir), args...); code != 2 {
			t.Errorf("%v: code %d, want 2", args, code)
		}
		if n, _ := dispatch.ReadPostFailed(dir, askSession); n != nil {
			t.Errorf("%v: note = %+v, want none", args, n)
		}
	}
}

func TestForgePRCommentFailureWritesNothing(t *testing.T) {
	dir := t.TempDir()
	env := commentEnv(&writeForge{err: errors.New("boom")}, dir)
	if code, _, _ := runForgeEnv(env, "pr", "comment", "12", "--body-file", bodyFile(t, "x\n")); code != 1 {
		t.Errorf("code %d, want 1", code)
	}
	if n, _ := dispatch.ReadPostFailed(dir, askSession); n != nil {
		t.Errorf("note = %+v, want none", n)
	}
}

// askNoteEnv is askFixture in a controller run with a state dir.
func askNoteEnv(t *testing.T) (askNoteFixture, string) {
	t.Helper()
	srv, env, _ := askFixture(t)
	dir := t.TempDir()
	base := env.Getenv
	env.Getenv = func(k string) string {
		if k == "LIGHTHOUSE_STATE_DIR" {
			return dir
		}
		return base(k)
	}
	env.Now = func() time.Time { return noteTime }
	return askNoteFixture{srv: srv, env: env}, dir
}

func TestAskIssueFailureWritesEmptyBody(t *testing.T) {
	fx, dir := askNoteEnv(t)
	fx.srv.Fail = map[string]int{"GET /repos/o/r/issues/42": 500}
	code, _, errOut := runAsk(fx.env, askArgs(oneQuestion...)...)
	if code != 1 || !strings.HasPrefix(errOut, "lighthouse ask: post failed: ") || strings.Count(errOut, "\n") != 1 {
		t.Errorf("code %d stderr %q", code, errOut)
	}
	n, _ := dispatch.ReadPostFailed(dir, askSession)
	if n == nil || n.Command != "ask" || n.Body != "" || n.Issue != 42 || n.Error == "" {
		t.Errorf("note = %+v", n)
	}
}

func TestAskPostFailureWritesBody(t *testing.T) {
	fx, dir := askNoteEnv(t)
	fx.srv.Fail = map[string]int{"POST /repos/o/r/issues/42/comments": 500}
	if code, _, _ := runAsk(fx.env, askArgs(oneQuestion...)...); code != 1 {
		t.Fatalf("code %d", code)
	}
	n, _ := dispatch.ReadPostFailed(dir, askSession)
	if n == nil || n.Command != "ask" || n.Issue != 42 {
		t.Fatalf("note = %+v", n)
	}
	if _, ok := protocol.ParseQuestion(n.Body); !ok {
		t.Errorf("body does not parse as a question:\n%s", n.Body)
	}
}

func TestAskInvalidInputWritesNothing(t *testing.T) {
	fx, dir := askNoteEnv(t)
	fx.srv.Fail = map[string]int{"GET /repos/o/r/issues/42": 500}
	for _, args := range [][]string{
		{"--issue", "42", "--phase", "execution", "--where", "w", "--what", "x"},
		append([]string{"--issue", "0"}, append(askBase[2:], oneQuestion...)...),
	} {
		if code, _, _ := runAsk(fx.env, args...); code == 0 {
			t.Errorf("%v: code 0", args)
		}
		if n, _ := dispatch.ReadPostFailed(dir, askSession); n != nil {
			t.Errorf("%v: note = %+v, want none", args, n)
		}
	}
}

func TestAskDryRunWritesNothing(t *testing.T) {
	fx, dir := askNoteEnv(t)
	fx.srv.Fail = map[string]int{"GET /repos/o/r/issues/42": 500}
	if code, _, _ := runAsk(fx.env, askArgs(append([]string{"--dry-run"}, oneQuestion...)...)...); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if n, _ := dispatch.ReadPostFailed(dir, askSession); n != nil {
		t.Errorf("note = %+v, want none", n)
	}
}

type askNoteFixture struct {
	srv *forgetest.Server
	env askEnv
}
