//go:build canary

// The canaries of the three imports gates (#383): the real hook, the real binary, the real
// tool (deptry, knip, go mod tidy) against a small repository per rule ID. They need `uv`, `npx`
// and the network for the pinned tools, so they run in the `canary` job of CI, not in go-test.
package importcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var (
	binDir   string // the directory of the built lighthouse
	repoRoot string // this checkout
)

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	var err error
	if repoRoot, err = filepath.Abs("../.."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if binDir, err = os.MkdirTemp("", "canary-bin-"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(binDir) }()
	build := exec.Command("go", "build", "-o", filepath.Join(binDir, "lighthouse"), "./cmd/lighthouse")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		return 1
	}
	return m.Run()
}

// hooks are the three gates; the Python fixtures live with the Python tests, the only place.
var hooks = map[string]string{
	"python": "tests/pack/python/testdata/python-imports-gate",
	"go":     "internal/importcheck/testdata/go-imports-gate",
	"node":   "internal/importcheck/testdata/node-imports-gate",
}

var blockingRulesRe = regexp.MustCompile(`(?m)^BLOCKING_RULES="([^"]*)"$`)

func blockingRules(t *testing.T, hook string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, "pack", hook, "hooks", hook+"-imports-gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := blockingRulesRe.FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s-imports-gate.sh has no BLOCKING_RULES line", hook)
	}
	ids := strings.Split(string(m[1]), ",")
	sort.Strings(ids)
	return ids
}

func fixtureDirs(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, dir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// TestCanaryBijection fails on a blocking rule without a fixture, and on a fixture of a rule
// the hook does not block on.
func TestCanaryBijection(t *testing.T) {
	for hook, dir := range hooks {
		got, want := fixtureDirs(t, dir), blockingRules(t, hook)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: fixture dirs %v, BLOCKING_RULES %v: one fixture per ID, no other", hook, got, want)
		}
	}
}

// TestCanaryPerRule runs each fixture of each blocking rule through the hook: exit 2 and the ID
// on stderr.
func TestCanaryPerRule(t *testing.T) {
	for hook, dir := range hooks {
		for _, id := range fixtureDirs(t, dir) {
			t.Run(hook+"/"+id, func(t *testing.T) {
				c := loadCanaryCase(t, filepath.Join(repoRoot, dir, id))
				if c.Exit != 2 || c.ID != id || c.Hook != hook {
					t.Fatalf("canary.json of %s/%s must say hook %s, exit 2, id %s: %+v", hook, id, hook, id, c)
				}
				runCanary(t, filepath.Join(repoRoot, dir, id), c)
			})
		}
	}
}

// TestCanaryExtra runs the fixtures that are no rule: the green counterpart of each pack, the
// extras case, the Node workspace and the Expo repository.
func TestCanaryExtra(t *testing.T) {
	roots := map[string]string{
		"python": "tests/pack/python/testdata/extra",
		"go":     "internal/importcheck/testdata/extra",
		"node":   "internal/importcheck/testdata/extra",
	}
	seen := map[string]bool{}
	for _, dir := range roots {
		if seen[dir] {
			continue
		}
		seen[dir] = true
		for _, name := range fixtureDirs(t, dir) {
			t.Run(name, func(t *testing.T) {
				c := loadCanaryCase(t, filepath.Join(repoRoot, dir, name))
				runCanary(t, filepath.Join(repoRoot, dir, name), c)
			})
		}
	}
}

// runCanary copies the fixture to a temp repository, builds the fake environment there and runs
// the hook for a turn that wrote c.Touch. In the repository itself `turn-roots` and Discover
// would not see the fixture: testdata is skipped.
func runCanary(t *testing.T, fixture string, c canaryCase) {
	t.Helper()
	needTool(t, "git")
	needTool(t, "jq")
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	copyFixture(t, fixture, repo)
	writeFile(t, filepath.Join(repo, ".gitignore"), ".venv/\nnode_modules/\n")
	writeFile(t, filepath.Join(repo, "lighthouse.toml"), "[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n")
	fakeGoDeps(t, repo, c.GoDeps)
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.name=canary", "-c", "user.email=canary@example.test", "commit", "-q", "--allow-empty", "-m", "empty"},
	} {
		mustRun(t, repo, nil, "git", args...)
	}
	mustRun(t, repo, nil, "git", "add", "-A")
	mustRun(t, repo, nil, "git", "-c", "user.name=canary", "-c", "user.email=canary@example.test", "commit", "-q", "-m", "fixture")
	if c.Hook == "python" {
		fakePython(t, repo, c.Dists)
	}
	if c.Hook == "node" {
		fakeNodeModules(t, repo, c.NodeModules)
	}

	stdout, stderr, code := runHook(t, repo, c)
	if code != c.Exit {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", code, c.Exit, stdout, stderr)
	}
	// A finding line, not the ID alone: the hook's own header names every blocking ID.
	if finding := regexp.MustCompile(`(?m)^\S+:\d+: ` + regexp.QuoteMeta(c.ID) + ` `); c.Exit == 2 && !finding.MatchString(stderr) {
		t.Errorf("stderr has no finding line with the rule ID %s:\n%s", c.ID, stderr)
	}
	if c.Exit == 0 && strings.TrimSpace(stdout) != "" {
		t.Errorf("a clean check says nothing; the hook said %q (not checked, or could not check)", stdout)
	}
}

func runHook(t *testing.T, repo string, c canaryCase) (stdout, stderr string, code int) {
	t.Helper()
	// The prompt lies in the future: the commit that built the fixture is then before the turn,
	// and only the Write below touches a path.
	prompt := time.Now().Add(time.Hour).UTC()
	entries := []map[string]any{
		{"type": "user", "timestamp": prompt.Format("2006-01-02T15:04:05.000Z"), "cwd": repo,
			"message": map[string]any{"role": "user", "content": "go"}},
		{"type": "assistant", "timestamp": prompt.Add(time.Second).Format("2006-01-02T15:04:05.000Z"), "cwd": repo,
			"message": map[string]any{"role": "assistant", "content": []map[string]any{{
				"type": "tool_use", "id": "toolu_1", "name": "Write",
				"input": map[string]any{"file_path": filepath.Join(repo, c.Touch)},
			}}}},
	}
	var lines strings.Builder
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		lines.Write(b)
		lines.WriteByte('\n')
	}
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	writeFile(t, transcript, lines.String())
	payload, err := json.Marshal(map[string]any{
		"session_id": "canary", "transcript_path": transcript, "cwd": repo,
		"hook_event_name": "Stop", "stop_hook_active": false,
	})
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(repoRoot, "pack", c.Hook, "hooks", c.Hook+"-imports-gate.sh"))
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader(string(payload))
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"CLAUDE_PROJECT_DIR="+repo,
		"LIGHTHOUSE_REPO_ROOT="+repo,
		"XDG_CACHE_HOME="+t.TempDir(), // the usage state: no alarm of this machine
		"UV_CACHE_DIR="+uvCacheDir(t),
		"GOPROXY=off", "GOFLAGS=",
	)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err = cmd.Run()
	code = 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errOut.String(), code
}

// uvCacheDir keeps uv's own cache while XDG_CACHE_HOME points elsewhere, so deptry is
// downloaded once, not per canary.
func uvCacheDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("UV_CACHE_DIR"); d != "" {
		return d
	}
	if _, err := exec.LookPath("uv"); err != nil {
		return ""
	}
	out, err := exec.Command("uv", "cache", "dir").Output()
	if err != nil {
		t.Fatalf("uv cache dir: %v", err)
	}
	return strings.TrimSpace(string(out))
}
