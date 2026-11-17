package roborev

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// pushGateRepo is a clone of a bare origin: main pushed, branch feat with one pushed commit and
// one unpushed commit of three code lines. roborev and codex are fake commands on PATH and the
// Codex budget says codex.
type pushGateRepo struct {
	t    *testing.T
	root string
}

func newPushGateRepo(t *testing.T) *pushGateRepo {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{
		"HOME": dir, "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
		"LIGHTHOUSE_REPO_ROOT": "", "LIGHTHOUSE_TOML": "",
	} {
		t.Setenv(k, v)
	}
	r := &pushGateRepo{t: t, root: filepath.Join(dir, "repo")}
	bare := filepath.Join(dir, "origin.git")
	r.git(dir, "init", "-q", "--bare", "-b", "main", bare)
	r.git(dir, "clone", "-q", bare, r.root)
	r.git(r.root, "checkout", "-q", "-b", "main")
	r.write("lighthouse.toml", "[project]\nlanguage = \"nl\"\n[review]\nroborev = \"local\"\n")
	r.write(".roborev.toml", "")
	r.git(r.root, "add", ".")
	r.git(r.root, "commit", "-q", "-m", "base")
	r.git(r.root, "push", "-q", "origin", "main")
	r.git(r.root, "checkout", "-q", "-b", "feat")
	r.commitLines("pushed.go", 200)
	r.git(r.root, "push", "-q", "-u", "origin", "feat")
	r.commitLines("fix.go", 3)
	bin := filepath.Join(dir, "bin")
	for _, tool := range []string{"roborev", "codex"} {
		r.writeAbs(filepath.Join(bin, tool), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return r
}

func (r *pushGateRepo) git(dir string, args ...string) string {
	r.t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...).CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *pushGateRepo) write(name, text string) {
	r.t.Helper()
	r.writeAbs(filepath.Join(r.root, name), text)
}

func (r *pushGateRepo) writeAbs(path, text string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *pushGateRepo) commitLines(name string, n int) {
	r.t.Helper()
	var b strings.Builder
	for i := range n {
		b.WriteString("var x" + strings.Repeat("y", i%5) + " = 1\n")
	}
	r.write(name, b.String())
	r.git(r.root, "add", name)
	r.git(r.root, "commit", "-q", "-m", "add "+name)
}

func (r *pushGateRepo) stateDir() string {
	return filepath.Join(r.git(r.root, "rev-parse", "--absolute-git-dir"), "lighthouse", "roborev")
}

func codexBudget() (int, string) { return 0, `{"decision": "codex", "reason": "ok"}` }

func gateField(t *testing.T, o *orderedjson.Object, key string) any {
	t.Helper()
	v, ok := o.Get(key)
	if !ok {
		t.Fatalf("gate output lacks %q", key)
	}
	return v
}

func TestRoborevGatePushSkipsThreshold(t *testing.T) {
	r := newPushGateRepo(t)
	head := r.git(r.root, "rev-parse", "HEAD")
	pushed := r.git(r.root, "rev-parse", "origin/feat")

	out, err := GatePush(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "verdict"); v != "run" {
		t.Fatalf("verdict %v, want run (3 code lines, no threshold): %v", v, out)
	}
	if v := gateField(t, out, "range"); v != pushed+".."+head {
		t.Errorf("range %v, want origin/feat..HEAD %s..%s", v, pushed, head)
	}
	state := filepath.Join(r.stateDir(), "push-"+head+".json")
	if v := gateField(t, out, "state"); v != state {
		t.Errorf("state %v, want %s", v, state)
	}
	if v := gateField(t, out, "raw"); v != filepath.Join(r.stateDir(), "push-"+head+".md") {
		t.Errorf("raw %v", v)
	}
	if _, err := os.Stat(filepath.Join(r.stateDir(), "feat.json")); !os.IsNotExist(err) {
		t.Errorf("push gate touched the branch record: %v", err)
	}
}

// A skip of the push gate stays a skip, in the push record: the pusher counts it red.
func TestRoborevGatePushSkipIsRecorded(t *testing.T) {
	r := newPushGateRepo(t)
	head := r.git(r.root, "rev-parse", "HEAD")
	if err := os.Remove(filepath.Join(r.root, ".roborev.toml")); err != nil {
		t.Fatal(err)
	}

	out, err := GatePush(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "verdict"); v != "skip" {
		t.Fatalf("verdict %v, want skip", v)
	}
	text, err := os.ReadFile(filepath.Join(r.stateDir(), "push-"+head+".json"))
	if err != nil {
		t.Fatalf("no push record: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(text, &rec); err != nil {
		t.Fatal(err)
	}
	if rec["uitkomst"] != "skip" {
		t.Errorf("push record %v, want uitkomst skip", rec)
	}
}

func TestRoborevGatePushIgnoresBranchRecord(t *testing.T) {
	r := newPushGateRepo(t)
	head := r.git(r.root, "rev-parse", "HEAD")
	branchRecord := filepath.Join(r.stateDir(), "feat.json")
	r.writeAbs(branchRecord, `{"range": "a..b", "uitkomst": "done", "sha": "`+head+`", "bevindingen": []}`)

	out, err := GatePush(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "verdict"); v != "run" {
		t.Fatalf("verdict %v, want run: the branch record is not the push record", v)
	}
	if v := gateField(t, out, "state"); v != filepath.Join(r.stateDir(), "push-"+head+".json") {
		t.Errorf("state %v", v)
	}
}

func TestRoborevGatePushReturnsItsOwnRecord(t *testing.T) {
	r := newPushGateRepo(t)
	head := r.git(r.root, "rev-parse", "HEAD")
	r.writeAbs(filepath.Join(r.stateDir(), "push-"+head+".json"),
		`{"range": "a..b", "uitkomst": "done", "sha": "`+head+`", "bevindingen": []}`)

	out, err := GatePush(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "verdict"); v != "done" {
		t.Fatalf("verdict %v, want done", v)
	}
	if v := gateField(t, out, "sha"); v != head {
		t.Errorf("sha %v, want %s", v, head)
	}
}

// Without the branch on origin the range starts at the merge-base, as the hook measures it.
func TestRoborevGatePushRangeFromMergeBase(t *testing.T) {
	r := newPushGateRepo(t)
	r.git(r.root, "push", "-q", "origin", "--delete", "feat")
	r.git(r.root, "update-ref", "-d", "refs/remotes/origin/feat")
	head := r.git(r.root, "rev-parse", "HEAD")
	base := r.git(r.root, "rev-parse", "origin/main")

	out, err := GatePush(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "range"); v != base+".."+head {
		t.Errorf("range %v, want %s..%s", v, base, head)
	}
}

// The plain gate keeps its threshold: three code lines on top of origin/main are a skip.
func TestRoborevGateKeepsThreshold(t *testing.T) {
	r := newPushGateRepo(t)
	r.git(r.root, "reset", "-q", "--hard", "origin/main")
	r.commitLines("fix.go", 3)

	out, err := Gate(r.root, codexBudget)
	if err != nil {
		t.Fatal(err)
	}

	if v := gateField(t, out, "verdict"); v != "skip" {
		t.Fatalf("verdict %v, want skip", v)
	}
}

func doneRecord(sha string, fixed ...string) string {
	decisions := []string{`{"path": "a", "line": 1, "severity": "low", "uitkomst": "refuted", "commit": null, "reden": "no"}`}
	for _, c := range fixed {
		decisions = append(decisions, `{"path": "a", "line": 2, "severity": "high", "uitkomst": "fixed", "commit": "`+c+`", "reden": null}`)
	}
	return `{"range": "a..b", "uitkomst": "done", "sha": "` + sha + `", "bevindingen": [` + strings.Join(decisions, ", ") + `]}`
}

func TestRoborevBodyLinePush(t *testing.T) {
	const skipped = "Roborev overgeslagen: geen reviewuitkomst vastgelegd"
	cases := []struct {
		name  string
		setup func(r *pushGateRepo) (want string)
	}{
		{"no push record", func(*pushGateRepo) string { return skipped }},
		{"only a branch record", func(r *pushGateRepo) string {
			r.writeAbs(filepath.Join(r.stateDir(), "feat.json"), doneRecord(r.git(r.root, "rev-parse", "HEAD")))
			return skipped
		}},
		{"push record of HEAD", func(r *pushGateRepo) string {
			head := r.git(r.root, "rev-parse", "HEAD")
			r.writeAbs(filepath.Join(r.stateDir(), "push-"+head+".json"), doneRecord(head))
			return "<!-- roborev-lokaal: " + head + " -->"
		}},
		{"skip in the push record of HEAD", func(r *pushGateRepo) string {
			head := r.git(r.root, "rev-parse", "HEAD")
			r.writeAbs(filepath.Join(r.stateDir(), "push-"+head+".json"), `{"range": "a..b", "uitkomst": "skip", "reden": "geen .roborev.toml"}`)
			return "Roborev overgeslagen: geen .roborev.toml"
		}},
		{"fix commit listed in an older push record", func(r *pushGateRepo) string {
			old := r.git(r.root, "rev-parse", "HEAD")
			r.commitLines("fix2.go", 2)
			fix := r.git(r.root, "rev-parse", "HEAD")
			r.writeAbs(filepath.Join(r.stateDir(), "push-"+old+".json"), doneRecord(old, fix[:10]))
			return "<!-- roborev-lokaal: " + old + " -->"
		}},
		{"unlisted commit after an older push record", func(r *pushGateRepo) string {
			old := r.git(r.root, "rev-parse", "HEAD")
			r.commitLines("more.go", 2)
			r.writeAbs(filepath.Join(r.stateDir(), "push-"+old+".json"), doneRecord(old))
			return skipped
		}},
		{"record off the branch, every later commit listed", func(r *pushGateRepo) string {
			r.git(r.root, "checkout", "-q", "-b", "side", "main")
			r.commitLines("side.go", 2)
			side := r.git(r.root, "rev-parse", "HEAD")
			r.git(r.root, "checkout", "-q", "feat")
			listed := strings.Fields(r.git(r.root, "rev-list", side+"..HEAD"))
			r.writeAbs(filepath.Join(r.stateDir(), "push-"+side+".json"), doneRecord(side, listed...))
			return skipped
		}},
		{"push record of another history", func(r *pushGateRepo) string {
			r.writeAbs(filepath.Join(r.stateDir(), "push-0123456789abcdef0123456789abcdef01234567.json"),
				doneRecord("0123456789abcdef0123456789abcdef01234567"))
			return skipped
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newPushGateRepo(t)
			want := c.setup(r)
			got, err := BodyLinePush(r.root)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("BodyLinePush = %q, want %q", got, want)
			}
		})
	}
}

func TestRoborevBodyLinePushOff(t *testing.T) {
	r := newPushGateRepo(t)
	r.write("lighthouse.toml", "[project]\nlanguage = \"nl\"\n")
	if got, err := BodyLinePush(r.root); err != nil || got != "" {
		t.Errorf("BodyLinePush = %q, %v; want empty when roborev is off", got, err)
	}
}
