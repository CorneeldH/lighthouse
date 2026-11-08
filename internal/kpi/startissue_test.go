package kpi

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitIn runs git in dir with a fixed identity and the given commit date.
func gitIn(t *testing.T, dir, date string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE="+date, "GIT_COMMITTER_DATE="+date,
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func commitFile(t *testing.T, repo, rel, date string) {
	t.Helper()
	path := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, date, "add", rel)
	gitIn(t, repo, date, "commit", "-q", "-m", "add "+rel)
}

// newRepo is a git repository wired in as the repo root, with the given lighthouse.toml.
func newRepo(t *testing.T, toml string) string {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "2026-01-01T00:00:00Z", "init", "-q")
	tomlPath := filepath.Join(t.TempDir(), "lighthouse.toml")
	if err := os.WriteFile(tomlPath, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_REPO_ROOT", repo)
	t.Setenv("LIGHTHOUSE_TOML", tomlPath)
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join("..", "..", "tests", "pack", "fixtures", "kpi", "headings.toml"))
	return repo
}

const englishToml = "[project]\nlanguage = \"en\"\n"

func runStartIssue(opts Options) (int, string, string) {
	var out, errOut bytes.Buffer
	rc := RunStartIssue(opts, &out, &errOut)
	return rc, out.String(), errOut.String()
}

// Classification: these fail when the transcript directory is derived from the wrong root or
// with the wrong character replacement.
func TestTranscriptDirFromRepoRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "/srv/my_repo.d")
	got, err := defaultTranscriptDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".claude", "projects", "-srv-my-repo-d")
	if got != want {
		t.Fatalf("dir %q, want %q", got, want)
	}
	if err := os.MkdirAll(want, 0o750); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","message":{"content":"pak 610 op"}}` + "\n"
	if err := os.WriteFile(filepath.Join(want, "s.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if rc, _, errOut := runStartIssue(Options{Metric: "mistrigger"}); rc != 0 {
		t.Fatalf("rc %d, stderr %q", rc, errOut)
	}
}

// Classification: fails when --since stops reaching git log or the shares are computed wrong.
func TestPathsSince(t *testing.T) {
	repo := newRepo(t, englishToml)
	commitFile(t, repo, "docs/superpowers/plans/old.md", "2026-01-01T00:00:00Z")
	commitFile(t, repo, "docs/developer/planning/specs/new.md", "2026-09-01T00:00:00Z")
	empty := t.TempDir()
	rc, out, errOut := runStartIssue(Options{Metric: "paths", Dir: empty})
	want := "{\n  \"paths\": {\n    \"since\": null,\n    \"right_dir\": 1,\n    \"superpowers_default_dir\": 1,\n    \"share\": 0.5\n  }\n}\n"
	if rc != 0 || out != want {
		t.Fatalf("rc %d stderr %q\n%s\nwant\n%s", rc, errOut, out, want)
	}
	since := "2026-08-01"
	rc, out, errOut = runStartIssue(Options{Metric: "paths", Dir: empty, Since: &since})
	want = "{\n  \"paths\": {\n    \"since\": \"2026-08-01\",\n    \"right_dir\": 1,\n    \"superpowers_default_dir\": 0,\n    \"share\": 1.0\n  }\n}\n"
	if rc != 0 || out != want {
		t.Fatalf("rc %d stderr %q\n%s\nwant\n%s", rc, errOut, out, want)
	}
}

// Classification: fails when paths.specs/plans are not read from the config or get no trailing slash.
func TestPathsDirsFromConfig(t *testing.T) {
	repo := newRepo(t, englishToml+"[paths]\nspecs = \"s\"\nplans = \"p/\"\n")
	commitFile(t, repo, "s/a.md", "2026-01-01T00:00:00Z")
	commitFile(t, repo, "docs/developer/planning/specs/b.md", "2026-01-02T00:00:00Z")
	commitFile(t, repo, "sx/c.md", "2026-01-03T00:00:00Z")
	rc, out, errOut := runStartIssue(Options{Metric: "paths", Dir: t.TempDir()})
	if rc != 0 || !strings.Contains(out, "\"right_dir\": 1,") {
		t.Fatalf("rc %d stderr %q\n%s", rc, errOut, out)
	}
}

// Classification: fails when a failing git log gives rc 0 or loses its message.
func TestPathsGitFailure(t *testing.T) {
	newRepo(t, englishToml)
	rc, out, errOut := runStartIssue(Options{Metric: "paths", Dir: t.TempDir()})
	if rc != 2 || out != "" || !strings.HasPrefix(errOut, "measure: git log failed: ") {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out, errOut)
	}
}

// Classification: fails when a table row moves or goes missing.
func TestFullTableOrder(t *testing.T) {
	newRepo(t, englishToml)
	repo := os.Getenv("LIGHTHOUSE_REPO_ROOT")
	commitFile(t, repo, "README.md", "2026-01-01T00:00:00Z")
	rc, out, errOut := runStartIssue(Options{Dir: t.TempDir()})
	if rc != 0 {
		t.Fatalf("rc %d stderr %q", rc, errOut)
	}
	var keys []string
	dec := json.NewDecoder(strings.NewReader(out))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not an object: %v", err)
	}
	for dec.More() {
		tok, _ := dec.Token()
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(keys, ",") != strings.Join(StartIssueMetrics, ",") {
		t.Fatalf("keys %v, want %v", keys, StartIssueMetrics)
	}
}

// Classification: fails when an unknown metric is accepted or the message changes.
func TestUnknownMetric(t *testing.T) {
	rc, out, errOut := runStartIssue(Options{Metric: "bogus"})
	want := "measure: unknown metric \"bogus\" (choose from leadin, worktree-first, mistrigger, paths, gate, scout)\n"
	if rc != 2 || out != "" || errOut != want {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out, errOut)
	}
}

// Classification: fails when a plain file counts as a transcript directory (Python's is_dir).
func TestDirThatIsAFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "plain")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rc, _, errOut := runStartIssue(Options{Metric: "leadin", Dir: file})
	if rc != 2 || errOut != "transcript directory does not exist: "+file+"\n" {
		t.Fatalf("rc %d stderr %q", rc, errOut)
	}
}

// Classification: fails when gate and scout need the headings file on a directory without sessions.
func TestHeadingsAreLoadedLazily(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(t.TempDir(), "absent.toml"))
	for _, metric := range []string{"gate", "scout"} {
		if rc, _, errOut := runStartIssue(Options{Metric: metric, Dir: t.TempDir()}); rc != 0 {
			t.Errorf("%s: rc %d stderr %q", metric, rc, errOut)
		}
	}
}

// Classification: fails when a headings error is swallowed instead of ending in rc 2.
func TestHeadingsErrorIsRC2(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(t.TempDir(), "absent.toml"))
	dir := t.TempDir()
	lines := `{"type":"user","message":{"content":"pak 610 op"}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Skill","input":{"skill":"lighthouse-start-issue"}}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	rc, out, errOut := runStartIssue(Options{Metric: "gate", Dir: dir})
	if rc != 2 || out != "" || !strings.HasPrefix(errOut, "measure: ") {
		t.Fatalf("rc %d stdout %q stderr %q", rc, out, errOut)
	}
}

// Classification: fails when a trailing slash on --dir hides the worktree sibling directories
// (Path("a/").parent is ".", filepath.Dir("a/") is "a").
func TestDirWithTrailingSlashFindsSiblings(t *testing.T) {
	parent := t.TempDir()
	line := `{"type":"user","message":{"content":"pak 610 op"}}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/x"}}]}}` + "\n"
	for _, d := range []string{"main", "main--claude-worktrees-a"} {
		if err := os.Mkdir(filepath.Join(parent, d), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(parent, d, "s.jsonl"), []byte(line), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rc, out, errOut := runStartIssue(Options{Metric: "worktree-first", Dir: filepath.Join(parent, "main") + "/"})
	if rc != 0 || !strings.Contains(out, "\"outside_worktree\": 2,") {
		t.Fatalf("rc %d stderr %q\n%s", rc, errOut, out)
	}
}
