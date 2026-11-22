package hookcheck

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRoot is a directory with a hook directory but no init: the tests of step 3 call smoke on
// registrations of their own.
func fakeRoot(t *testing.T, name string) string {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not on the PATH")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Join(root, ".claude", "hooks", "lighthouse"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// fakeHook writes an executable sh script into the hook directory and returns the command a
// registration gives it, in the form init writes.
func fakeHook(t *testing.T, root, name, body string) string {
	t.Helper()
	p := filepath.Join(root, ".claude", "hooks", "lighthouse", name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/` + name
}

// smokeOut runs step 3 alone and returns its lines and the number of findings.
func smokeOut(env Env, regs []registration, bash string) (string, int) {
	var out bytes.Buffer
	r := &report{out: &out}
	smoke(env, regs, bash, r)
	return out.String(), r.findings
}

func TestSmokeOKAndExit2(t *testing.T) {
	root := fakeRoot(t, "repo")
	regs := []registration{
		{Event: "PreToolUse", Matcher: "Bash", Command: fakeHook(t, root, "pass.sh", "cat >/dev/null\nexit 0\n")},
		{Event: "PreToolUse", Matcher: "Bash", Command: fakeHook(t, root, "block.sh", "cat >/dev/null\necho 'Main-commit gate: blocked' >&2\nexit 2\n")},
	}
	out, findings := smokeOut(testEnv(t, root), regs, "")
	want := "ok    hook pass.sh (PreToolUse Bash)\nok    hook block.sh (PreToolUse Bash)\n"
	if findings != 0 || out != want {
		t.Fatalf("findings %d, out:\n%s\nwant 0 and:\n%s", findings, out, want)
	}
}

func TestSmokeCouldNotCheckOnExit0IsFail(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"on stdout, stderr empty",
			"echo '{\"systemMessage\": \"x: COULD NOT CHECK — jq is missing.\"}'\nexit 0\n",
			`FAIL  hook warn.sh (Stop): {"systemMessage": "x: COULD NOT CHECK — jq is missing."}`},
		{"first non-empty stderr line wins over stdout",
			"echo 'stdout line'\nprintf '\\n  \\nCOULD NOT CHECK: lighthouse not on PATH\\nsecond\\n' >&2\nexit 0\n",
			"FAIL  hook warn.sh (Stop): COULD NOT CHECK: lighthouse not on PATH"},
		{"on stderr with exit 2",
			"echo 'COULD NOT CHECK: no binary' >&2\nexit 2\n",
			"FAIL  hook warn.sh (Stop): COULD NOT CHECK: no binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := fakeRoot(t, "repo")
			regs := []registration{{Event: "Stop", Command: fakeHook(t, root, "warn.sh", c.body)}}
			out, findings := smokeOut(testEnv(t, root), regs, "")
			if findings != 1 || !strings.HasPrefix(out, c.want) {
				t.Fatalf("findings %d, out:\n%s\nwant 1 and a line starting %q", findings, out, c.want)
			}
		})
	}
}

func TestSmokeOtherExitAndTimeoutAreFail(t *testing.T) {
	t.Run("exit 1", func(t *testing.T) {
		root := fakeRoot(t, "repo")
		regs := []registration{{Event: "PreToolUse", Matcher: "Bash", Command: fakeHook(t, root, "one.sh", "echo broken >&2\nexit 1\n")}}
		out, findings := smokeOut(testEnv(t, root), regs, "")
		want := "FAIL  hook one.sh (PreToolUse Bash): exit 1: broken"
		if findings != 1 || !strings.HasPrefix(out, want) {
			t.Fatalf("findings %d, out:\n%s\nwant 1 and %q", findings, out, want)
		}
	})
	cases := []struct {
		name, body string
		limit      time.Duration // the killed group ends at once; the WaitDelay of 2s bounds an escaped grandchild
		needs      string
	}{
		// The sleep is a child of the script, not an exec: killing only the shell would leave it
		// holding the pipes until WaitDelay.
		{"timeout kills the group", "sleep 30\necho done\n", 2500 * time.Millisecond, ""},
		// A grandchild in a session of its own escapes the group kill; WaitDelay stops the wait.
		{"timeout with an escaped grandchild", "setsid sleep 30 &\nsleep 30\n", 5 * time.Second, "setsid"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.needs != "" {
				if _, err := exec.LookPath(c.needs); err != nil {
					t.Skipf("%s is not on the PATH", c.needs)
				}
			}
			root := fakeRoot(t, "repo")
			regs := []registration{{Event: "Stop", Timeout: time.Second, Command: fakeHook(t, root, "slow.sh", c.body)}}
			start := time.Now()
			out, findings := smokeOut(testEnv(t, root), regs, "")
			took := time.Since(start)
			want := "FAIL  hook slow.sh (Stop): timed out after 1s"
			if findings != 1 || !strings.HasPrefix(out, want) {
				t.Fatalf("findings %d, out:\n%s\nwant 1 and %q", findings, out, want)
			}
			if took < time.Second || took > c.limit {
				t.Fatalf("took %v, want close to the timeout of 1s (at most %v), not the sleep of 30s", took, c.limit)
			}
		})
	}
}

func TestSmokeSkipsForeignRegistrations(t *testing.T) {
	root := fakeRoot(t, "repo")
	marker := filepath.Join(t.TempDir(), "ran")
	regs := []registration{
		{Event: "PreToolUse", Matcher: "Bash", Command: "touch '" + marker + "'"},
		{Event: "Stop", Command: `"$CLAUDE_PROJECT_DIR"/.claude/hooks/entire/stop.sh; touch '` + marker + `'`},
	}
	out, findings := smokeOut(testEnv(t, root), regs, "")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a registration outside .claude/hooks/lighthouse/ ran")
	}
	if findings != 0 || strings.Contains(out, "hook ") {
		t.Fatalf("findings %d, out:\n%s\nwant no hook line", findings, out)
	}
}

func TestSmokeEnvAndCwd(t *testing.T) {
	for _, name := range []string{"repo", "a repo with spaces"} {
		t.Run(name, func(t *testing.T) {
			root := fakeRoot(t, name)
			dump := filepath.Join(t.TempDir(), "env.txt")
			t.Setenv("LIGHTHOUSE_CONTROLLER_RUN", "run-1")
			t.Setenv("LIGHTHOUSE_USAGE_REFRESH", "")
			realState := t.TempDir()
			t.Setenv("XDG_STATE_HOME", realState)
			body := `{
  printf '%s\n' "$CLAUDE_PROJECT_DIR"
  pwd
  printf '%s\n' "$LIGHTHOUSE_USAGE_REFRESH"
  printf '%s\n' "${LIGHTHOUSE_CONTROLLER_RUN-unset}"
  printf '%s\n' "$XDG_STATE_HOME"
} >'` + dump + "'\n"
			regs := []registration{{Event: "Stop", Command: fakeHook(t, root, "env.sh", body)}}
			if out, findings := smokeOut(testEnv(t, root), regs, ""); findings != 0 {
				t.Fatalf("findings %d:\n%s", findings, out)
			}
			got := strings.Split(strings.TrimSuffix(readFile(t, filepath.Dir(dump), "env.txt"), "\n"), "\n")
			if len(got) != 5 {
				t.Fatalf("dump %q, want 5 lines", got)
			}
			want := []string{root, root, "1", "unset"}
			for i, w := range want {
				if got[i] != w {
					t.Fatalf("line %d = %q, want %q (all: %q)", i, got[i], w, got)
				}
			}
			if got[4] == realState || got[4] == "" {
				t.Fatalf("XDG_STATE_HOME = %q, want a temporary directory, not the real %q", got[4], realState)
			}
		})
	}
}

func TestPayloadPerEvent(t *testing.T) {
	tmp := t.TempDir()
	// 10:00:00.3 in UTC+1: 09:00:00.3 UTC, rounded up to 09:00:01.
	now := time.Date(2026, 11, 22, 10, 0, 0, 300_000_000, time.FixedZone("CET", 3600))
	decode := func(t *testing.T, event, matcher string) map[string]any {
		t.Helper()
		data, err := Payload(event, matcher, now, tmp)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("payload %s: %v", data, err)
		}
		if m["hook_event_name"] != event {
			t.Fatalf("hook_event_name = %v, want %s", m["hook_event_name"], event)
		}
		return m
	}

	for _, event := range []string{"PreToolUse", "PostToolUse", "PostToolUseFailure"} {
		m := decode(t, event, "Bash")
		input, _ := m["tool_input"].(map[string]any)
		if m["tool_name"] != "Bash" || input["command"] != "true" {
			t.Fatalf("%s Bash: tool_name %v, tool_input %v; want Bash and command \"true\"", event, m["tool_name"], input)
		}
	}

	m := decode(t, "PostToolUse", "Edit|Write")
	input, _ := m["tool_input"].(map[string]any)
	path, _ := input["file_path"].(string)
	if path == "" || strings.HasSuffix(path, ".md") || !filepath.IsAbs(path) {
		t.Fatalf("Edit|Write: file_path %q, want an absolute path without .md", path)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("Edit|Write: file_path %q exists", path)
	}

	for _, c := range []struct{ event, matcher string }{
		{"Stop", ""}, {"SubagentStop", ""}, {"UserPromptSubmit", ""}, {"PostToolUse", ""},
	} {
		m := decode(t, c.event, c.matcher)
		transcript, _ := m["transcript_path"].(string)
		data, err := os.ReadFile(transcript)
		if err != nil {
			t.Fatalf("%s: transcript_path %q: %v", c.event, transcript, err)
		}
		lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
		if len(lines) != 1 {
			t.Fatalf("%s: transcript has %d lines, want 1:\n%s", c.event, len(lines), data)
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
			t.Fatal(err)
		}
		msg, _ := entry["message"].(map[string]any)
		if entry["type"] != "user" || msg["content"] == nil || strings.Contains(lines[0], "tool_use") {
			t.Fatalf("%s: entry %s, want one prompt without tool use", c.event, lines[0])
		}
		if entry["timestamp"] != "2026-11-22T09:00:01Z" {
			t.Fatalf("%s: timestamp %v, want 2026-11-22T09:00:01Z", c.event, entry["timestamp"])
		}
	}

	m = decode(t, "Notification", "permission_prompt|worker_permission_prompt")
	if m["notification_type"] != "permission_prompt" {
		t.Fatalf("Notification: notification_type %v, want permission_prompt", m["notification_type"])
	}
}

func TestPayloadTimestampOnAWholeSecond(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	if _, err := Payload("Stop", "", now, tmp); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, tmp, "transcript.jsonl")
	if !strings.Contains(data, `"timestamp":"2026-11-22T09:00:00Z"`) {
		t.Fatalf("transcript %s, want timestamp 2026-11-22T09:00:00Z", data)
	}
}

func TestProjectDirNative(t *testing.T) {
	cases := []struct{ goos, root, want string }{
		{"linux", "/home/a b/repo", "/home/a b/repo"},
		{"darwin", "/Users/a/repo", "/Users/a/repo"},
		{"windows", "C:/Users/A B/repo", `C:\Users\A B\repo`},
		{"windows", `d:\work\repo`, `d:\work\repo`},
	}
	for _, c := range cases {
		if got := projectDir(c.goos, c.root); got != c.want {
			t.Errorf("projectDir(%q, %q) = %q, want %q", c.goos, c.root, got, c.want)
		}
	}
}

func TestSmokeNoBashIsFail(t *testing.T) {
	// Claude Code runs a shell-form hook with `sh -c` on Linux and macOS (hooks docs); without a
	// sh on the PATH no hook runs.
	root := fakeRoot(t, "repo")
	marker := filepath.Join(t.TempDir(), "ran")
	regs := []registration{{Event: "Stop", Command: fakeHook(t, root, "x.sh", "touch '"+marker+"'\n")}}
	t.Setenv("PATH", "")
	out, findings := smokeOut(testEnv(t, root), regs, "")
	want := "FAIL  no sh on the PATH"
	if findings != 1 || !strings.HasPrefix(out, want) || strings.Contains(out, "hook x.sh") {
		t.Fatalf("findings %d, out:\n%s\nwant one %q and no hook line", findings, out, want)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the hook ran without a sh")
	}
}

func TestSmokeWindowsWithoutGitBashSkips(t *testing.T) {
	root := newFixture(t, "v1.0.0")
	marker := filepath.Join(t.TempDir(), "ran")
	writeFile(t, root, ".claude/hooks/lighthouse/run.sh", "#!/bin/sh\ntouch '"+marker+"'\n")

	env := testEnv(t, root)
	fake := windowsEnv(nil, nil, "")
	env.GOOS, env.Getenv, env.LookPath, env.Exists = fake.GOOS, fake.Getenv, fake.LookPath, fake.Exists
	code, out, _ := run(env)
	if code != 1 || !strings.Contains(out, "FAIL  Git Bash not found") {
		t.Fatalf("exit %d, want 1 with the Git Bash FAIL:\n%s", code, out)
	}
	if !strings.Contains(out, "SKIPPED  hooks not run: no Git Bash (see the Git Bash line above)\n") || strings.Contains(out, "hook main-commit-gate") {
		t.Fatalf("want one SKIPPED line for step 3 and no hook line:\n%s", out)
	}
	if strings.Count(out, "FAIL") != 1 {
		t.Fatalf("want only the Git Bash FAIL:\n%s", out)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a hook ran without Git Bash")
	}
}

var (
	buildOnce sync.Once
	binDir    string
	buildErr  error
	buildOut  []byte
)

// lighthouseOnPath builds the binary of this tree once and puts its directory first on the PATH.
func lighthouseOnPath(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"bash", "git", "jq", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on the PATH", tool)
		}
	}
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "hookcheck-bin-")
		if err != nil {
			buildErr = err
			return
		}
		binDir = dir
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "lighthouse"), "./cmd/lighthouse")
		cmd.Dir = filepath.Join("..", "..")
		buildOut, buildErr = cmd.CombinedOutput()
	})
	if buildErr != nil {
		t.Fatalf("go build: %v\n%s", buildErr, buildOut)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestMain(m *testing.M) {
	code := m.Run()
	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}
	os.Exit(code)
}

// realFixture is a repository after `init` of every package that registers hooks, with the
// init committed, a .golangci-version that no golangci-lint matches, and a dirty .go file: the
// Go gates would report COULD NOT CHECK if they ran, and they run only when the transcript's
// timestamp lies before that file's mtime.
func realFixture(t *testing.T, version string) string {
	t.Helper()
	root := newFixture(t, version, "go", "node", "python", "shell")
	writeFile(t, root, "go.mod", "module example.invalid/x\n\ngo 1.22\n")
	writeFile(t, root, ".golangci-version", "v0.0.1\n")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-q", "-m", "lighthouse init")
	writeFile(t, root, "x.go", "package x\nfunc  X( ) {}\n")
	if _, err := os.Stat(filepath.Join(root, "cmd", "lighthouse", "main.go")); err == nil {
		t.Fatal("the fixture must not look like lighthouse's own checkout")
	}
	return root
}

// toolOf is the external tool a hook needs beyond bash, git, jq and lighthouse; a host without
// it skips that hook.
var toolOf = map[string][]string{
	"ruff-format-check.sh": {"uvx", "ruff"},
	"ty-gate.sh":           {"uvx"},
	"shell-lint-check.sh":  {"shellcheck"},
	"node-imports-gate.sh": {"node"},
}

var hookLine = regexp.MustCompile(`(?m)^(ok|FAIL)  +hook (\S+(?: [a-z-]+)*) \(([^)]*)\)(.*)$`)

func TestSmokeRealPackAllOKOnLinux(t *testing.T) {
	lighthouseOnPath(t)
	root := realFixture(t, "v1.0.0")
	regs := readRegistrations(t, root)
	out, _ := smokeOut(testEnv(t, root), regs, "")

	lines := hookLine.FindAllStringSubmatch(out, -1)
	want := 0
	for _, reg := range regs {
		if strings.Contains(reg.Command, ".claude/hooks/lighthouse/") {
			want++
		}
	}
	if want < 20 || len(lines) != want {
		t.Fatalf("%d hook lines for %d registrations (want at least 20):\n%s", len(lines), want, out)
	}
	for _, l := range lines {
		verdict, name, where := l[1], l[2], l[3]
		t.Run(name+" "+where, func(t *testing.T) {
			if tools, ok := toolOf[name]; ok && verdict == "FAIL" {
				found := false
				for _, tool := range tools {
					if _, err := exec.LookPath(tool); err == nil {
						found = true
					}
				}
				if !found {
					t.Skipf("%v is not on this host", tools)
				}
			}
			if verdict != "ok" {
				t.Fatalf("%s", l[0])
			}
		})
	}
}

// readRegistrations reads the command hooks of root's .claude/settings.json.
func readRegistrations(t *testing.T, root string) []registration {
	t.Helper()
	var regs []registration
	smokeStep = func(_ Env, all []registration, _ string, _ *report) { regs = all }
	defer func() { smokeStep = smoke }()
	var out, errOut bytes.Buffer
	Run(testEnv(t, root), &out, &errOut)
	if regs == nil {
		t.Fatalf("no registrations read:\n%s%s", out.String(), errOut.String())
	}
	return regs
}

// tree is every path under root with its mtime and size; a missing root is empty.
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return filepath.SkipDir
			}
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		got[path] = info.ModTime().String() + " " + info.Mode().String()
		if !d.IsDir() {
			got[path] += fmt.Sprintf(" %d", info.Size())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSmokeLeavesStateUntouched(t *testing.T) {
	lighthouseOnPath(t)
	// v9.9.9 against the binary's dev: the version notice would land in the real XDG_STATE_HOME.
	root := realFixture(t, "v9.9.9")
	dirs := map[string]string{}
	for _, name := range []string{"LIGHTHOUSE_STATE_DIR", "XDG_RUNTIME_DIR", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		dirs[name] = t.TempDir()
		t.Setenv(name, dirs[name])
	}
	regs := readRegistrations(t, root)
	before := map[string]map[string]string{"repo": tree(t, root)}
	for name, dir := range dirs {
		before[name] = tree(t, dir)
	}
	time.Sleep(10 * time.Millisecond)

	out, _ := smokeOut(testEnv(t, root), regs, "")
	if !strings.Contains(out, "hook main-commit-gate") {
		t.Fatalf("the smoke test ran no hook:\n%s", out)
	}

	after := map[string]map[string]string{"repo": tree(t, root)}
	for name, dir := range dirs {
		after[name] = tree(t, dir)
	}
	for name, b := range before {
		a := after[name]
		for p, v := range a {
			if b[p] != v {
				t.Errorf("%s: %s changed or appeared (%q -> %q)", name, p, b[p], v)
			}
		}
		for p := range b {
			if _, ok := a[p]; !ok {
				t.Errorf("%s: %s disappeared", name, p)
			}
		}
	}
}
