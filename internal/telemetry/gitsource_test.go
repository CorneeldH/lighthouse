package telemetry

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func runGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commitFile writes file and commits it with a fixed author and committer date.
func commitFile(t *testing.T, dir, file, subject string, at time.Time) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(subject+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := at.Format(time.RFC3339)
	env := []string{"GIT_AUTHOR_DATE=" + d, "GIT_COMMITTER_DATE=" + d}
	runGit(t, dir, env, "add", file)
	runGit(t, dir, env, "-c", "user.name=T", "-c", "user.email=t@example.com", "commit", "-q", "-m", subject)
	return runGit(t, dir, nil, "rev-parse", "HEAD")
}

type fakeRepo struct {
	dir     string
	a, b, c string
	aAt     time.Time
	cAt     time.Time
}

func newFakeRepo(t *testing.T) fakeRepo {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	aAt := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	r := fakeRepo{dir: dir, aAt: aAt, cAt: aAt.Add(8 * 24 * time.Hour)}
	r.a = commitFile(t, dir, "a.txt", "feat(a): maak a (#10)", aAt)
	r.b = commitFile(t, dir, "a.txt", "fix(a): herstel a", aAt.Add(48*time.Hour))
	r.c = commitFile(t, dir, "b.txt", "feat(b): maak b (#11)", r.cAt)
	return r
}

func TestGitSource(t *testing.T) {
	r := newFakeRepo(t)
	src := GitSource{Dir: r.dir, Branch: "main"}
	ctx := context.Background()

	t.Run("MergeCommit finds the squash commit with its files", func(t *testing.T) {
		got, err := src.MergeCommit(ctx, 10)
		if err != nil {
			t.Fatal(err)
		}
		if got.SHA != r.a || !reflect.DeepEqual(got.Files, []string{"a.txt"}) || !got.At.Equal(r.aAt) {
			t.Fatalf("got %+v", got)
		}
	})
	t.Run("MergeCommit of an unknown PR", func(t *testing.T) {
		if _, err := src.MergeCommit(ctx, 99); !errors.Is(err, ErrNoMergeCommit) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("MergeCommit matches only the whole number at the end", func(t *testing.T) {
		if _, err := src.MergeCommit(ctx, 1); !errors.Is(err, ErrNoMergeCommit) {
			t.Fatalf("(#1) must not match (#10): %v", err)
		}
	})
	t.Run("CommitsAfter is a half-open window, oldest first", func(t *testing.T) {
		got, err := src.CommitsAfter(ctx, r.aAt, r.aAt.Add(RecoveryWindow))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].SHA != r.b || !reflect.DeepEqual(got[0].Files, []string{"a.txt"}) {
			t.Fatalf("got %+v", got)
		}
		all, err := src.CommitsAfter(ctx, r.aAt.Add(-time.Hour), r.cAt)
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 || all[0].SHA != r.a || all[1].SHA != r.b || all[2].SHA != r.c {
			t.Fatalf("order: %+v", all)
		}
	})
	t.Run("IssuesMentioning delegates, and is empty without a hook", func(t *testing.T) {
		if got, err := src.IssuesMentioning(ctx, 10); err != nil || got != nil {
			t.Fatalf("got %v, %v", got, err)
		}
		withHook := src
		withHook.Issues = func(_ context.Context, pr int) ([]BugIssue, error) {
			return []BugIssue{{Number: pr}}, nil
		}
		if got, _ := withHook.IssuesMentioning(ctx, 7); len(got) != 1 || got[0].Number != 7 {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("a failing git is an error", func(t *testing.T) {
		bad := GitSource{Dir: r.dir, Branch: "no-such-branch"}
		if _, err := bad.CommitsAfter(ctx, r.aAt, r.cAt); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestGitSourceMidLineNumberDoesNotMatch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	commitFile(t, dir, "x.txt", "feat: iets (#10) en meer", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	commitFile(t, dir, "y.txt", "feat: zonder haakje #10)", time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	src := GitSource{Dir: dir, Branch: "main"}
	if _, err := src.MergeCommit(context.Background(), 10); !errors.Is(err, ErrNoMergeCommit) {
		t.Fatalf("err = %v", err)
	}
}

func TestGitSourceOddFileNames(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, nil, "init", "-q", "-b", "main")
	commitFile(t, dir, "één bestand.txt", "feat: spatie en accent (#5)", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	got, err := GitSource{Dir: dir, Branch: "main"}.MergeCommit(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Files, []string{"één bestand.txt"}) {
		t.Fatalf("files = %q", got.Files)
	}
}

func TestMeasureRecoveryOnFakeRepo(t *testing.T) {
	r := newFakeRepo(t)
	src := GitSource{Dir: r.dir, Branch: "main"}
	ctx := context.Background()

	rec, err := MeasureRecovery(ctx, src, 10, r.aAt, r.aAt.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != RecoveryRecovered || len(rec.Items) != 1 || rec.Items[0].Ref != r.b || rec.Items[0].Kind != "fix" {
		t.Fatalf("PR 10: %+v", rec)
	}

	rec, err = MeasureRecovery(ctx, src, 11, r.cAt, r.cAt.Add(10*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Status != RecoveryNone || len(rec.Items) != 0 {
		t.Fatalf("PR 11: %+v", rec)
	}
}
