package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

type histRig struct {
	t    *testing.T
	home string
	repo string
	proc string
}

// newHistRig is a HOME, a git repo and a /proc/version stand-in; the process HOME and
// CLAUDE_CONFIG_DIR point at the temp HOME, as agentaudit.DefaultEnv reads them.
func newHistRig(t *testing.T) *histRig {
	t.Helper()
	root := t.TempDir()
	r := &histRig{t: t, home: filepath.Join(root, "home"), repo: filepath.Join(root, "repo"), proc: filepath.Join(root, "version")}
	for _, d := range []string{r.home, r.repo} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(r.proc, []byte("Linux version 6.0 (gcc)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", r.home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(r.home, ".claude"))
	mrGit(t, r.repo, nil, "init", "-q", "-b", "main")
	return r
}

func (r *histRig) transcript(project, sid string, lines ...string) string {
	r.t.Helper()
	path := filepath.Join(r.home, ".claude", "projects", project, sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		r.t.Fatal(err)
	}
	return path
}

func (r *histRig) run(cwd string, args ...string) (int, string, string) {
	r.t.Helper()
	var out, errOut bytes.Buffer
	env := historyEnv{
		Getenv:      os.Getenv,
		Cwd:         func() (string, error) { return cwd, nil },
		Stdout:      &out,
		ProcVersion: r.proc,
	}
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newHistoryCommand(env))
	root.SetArgs(append([]string{"history"}, args...))
	code := execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func histBashLine(cwd, branch, ts, cmd string) string {
	return fmt.Sprintf(`{"type":"assistant","cwd":%q,"gitBranch":%q,"timestamp":%q,"message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":%q}}]}}`, cwd, branch, ts, cmd)
}

func histDecode(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, out)
	}
	return m
}

func histFinding(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	for _, f := range m["findings"].([]any) {
		if fm := f.(map[string]any); fm["key"] == key {
			return fm
		}
	}
	t.Fatalf("no finding %s", key)
	return nil
}

func TestHistory_NotGitRepo(t *testing.T) {
	r := newHistRig(t)
	dir := t.TempDir()
	code, _, errOut := r.run(dir)
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if want := "lighthouse history: " + dir + " is not a git repository"; !strings.Contains(errOut, want) {
		t.Errorf("stderr %q lacks %q", errOut, want)
	}
}

func TestHistory_NoTranscripts(t *testing.T) {
	r := newHistRig(t)
	code, out, errOut := r.run(r.repo)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	m := histDecode(t, out)
	if m["sessions"].(float64) != 0 {
		t.Errorf("sessions = %v", m["sessions"])
	}
	for _, f := range m["findings"].([]any) {
		if s := f.(map[string]any)["status"]; s != "no_data" {
			t.Errorf("%v status %v, want no_data", f.(map[string]any)["key"], s)
		}
	}
	lv := m["level"].(map[string]any)
	if lv["recommended"] != "light" || lv["reason"] != "no history" {
		t.Errorf("level = %v", lv)
	}
}

func TestHistory_ForeignProjectWithoutOrigin(t *testing.T) {
	r := newHistRig(t)
	other := filepath.Join(t.TempDir(), "elsewhere", "other-repo")
	r.transcript("-other", "s1", histBashLine(other, "main", "2026-11-01T10:00:00Z", "git push origin main"))
	code, out, errOut := r.run(r.repo)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := histDecode(t, out)["sessions"].(float64); got != 0 {
		t.Errorf("sessions = %v, want 0", got)
	}
}

func TestHistory_UnreadableProjectsDir(t *testing.T) {
	r := newHistRig(t)
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := r.run(r.repo, "--projects-dir", file)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
}

func TestHistory_CustomDefaultBranch(t *testing.T) {
	r := newHistRig(t)
	mrGit(t, r.repo, nil, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init")
	mrGit(t, r.repo, nil, "update-ref", "refs/remotes/origin/stable", "HEAD")
	mrGit(t, r.repo, nil, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/stable")
	r.transcript("-p", "s1",
		histBashLine(r.repo, "feature", "2026-11-01T10:00:00Z", "git push origin HEAD:stable"),
		histBashLine(r.repo, "feature", "2026-11-01T10:01:00Z", "git push origin HEAD:stable"))
	code, out, errOut := r.run(r.repo, "--projects-dir", filepath.Join(r.home, ".claude", "projects"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	m := histDecode(t, out)
	if m["sessions"].(float64) != 1 {
		t.Fatalf("sessions = %v", m["sessions"])
	}
	meas := histFinding(t, m, "trunk")["measured"].(map[string]any)
	if !strings.Contains(fmt.Sprint(meas["trunk_names"]), "stable") {
		t.Errorf("trunk_names = %v, want stable", meas["trunk_names"])
	}
}

func TestHistory_BadSince(t *testing.T) {
	r := newHistRig(t)
	if code, _, _ := r.run(r.repo, "--since", "morgen"); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}

func TestHistory_WSLHint(t *testing.T) {
	r := newHistRig(t)
	if err := os.WriteFile(r.proc, []byte("Linux version 5.15 microsoft-standard-WSL2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := r.run(r.repo)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if hint, _ := histDecode(t, out)["hint"].(string); !strings.Contains(hint, "/mnt/c/Users/<naam>/.claude/projects") {
		t.Errorf("hint = %q", hint)
	}
}

func TestHistory_SessionOnWSLPathAndSlug(t *testing.T) {
	r := newHistRig(t)
	mrGit(t, r.repo, nil, "remote", "add", "origin", "https://github.com/Acme/Widget.git")
	r.transcript("-win", "s1",
		histBashLine(`C:\Users\x\src\acme\widget`, "main", "2026-11-01T10:00:00Z", "git commit -m x"),
		histBashLine(`c:\Users\x\src\acme\widget`, "main", "2026-11-01T10:01:00Z", "git commit -m y"))
	code, out, errOut := r.run(r.repo)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := histDecode(t, out)["sessions"].(float64); got != 1 {
		t.Errorf("sessions = %v, want 1", got)
	}
}

func TestDefaultProjectsDir(t *testing.T) {
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got, err := defaultProjectsDir(get(map[string]string{"CLAUDE_CONFIG_DIR": "/cfg", "HOME": "/h"})); err != nil || got != "/cfg/projects" {
		t.Errorf("config dir: %q %v", got, err)
	}
	if got, err := defaultProjectsDir(get(map[string]string{"HOME": "/h"})); err != nil || got != "/h/.claude/projects" {
		t.Errorf("home: %q %v", got, err)
	}
	if _, err := defaultProjectsDir(get(map[string]string{"HOME": "rel"})); err == nil {
		t.Error("relative HOME: want error")
	}
}
