package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/roborev"
)

// The roborev-gate cases: one JSON file per case in
// tests/pack/fixtures/roborev_gate/. A case builds a repo on branch `feat` with 100 added code
// lines on top of origin/main and fake roborev/codex binaries on PATH, then runs its steps.
const (
	gateCaseDir    = "../../tests/pack/fixtures/roborev_gate"
	gateFixtureDir = "../../tests/pack/fixtures/roborev"
)

type gateCase struct {
	Level string           `json:"level"`
	Repo  *bool            `json:"repo"`
	Setup []map[string]any `json:"setup"`
	Steps []map[string]any `json:"steps"`
}

type gateRun struct {
	t       *testing.T
	repo    string
	tmp     string
	vars    map[string]string
	hasRepo bool
}

func gateGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gateCommitLines(t *testing.T, repo string, count int, name string) {
	t.Helper()
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("line " + gateItoa(i) + "\n")
	}
	gateWrite(t, filepath.Join(repo, name), b.String())
	gateGit(t, repo, "add", name)
	gateGit(t, repo, "commit", "-m", "add "+name)
}

func gateItoa(i int) string { b, _ := json.Marshal(i); return string(b) }

func gateWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newGateRun(t *testing.T, c gateCase) *gateRun {
	t.Helper()
	dir := t.TempDir()
	r := &gateRun{t: t, tmp: filepath.Join(dir, "tmp"), repo: filepath.Join(dir, "repo"), vars: map[string]string{}}
	if err := os.Mkdir(r.tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	r.hasRepo = c.Repo == nil || *c.Repo
	abs, _ := filepath.Abs(gateFixtureDir)
	r.vars["tmp"], r.vars["fixtures"], r.vars["repo"] = r.tmp, abs, r.repo
	for k, v := range map[string]string{
		"GIT_AUTHOR_NAME": "t", "GIT_AUTHOR_EMAIL": "t@example.com",
		"GIT_COMMITTER_NAME": "t", "GIT_COMMITTER_EMAIL": "t@example.com",
		"LIGHTHOUSE_REPO_ROOT": "", "LIGHTHOUSE_TOML": "",
	} {
		t.Setenv(k, v)
	}
	if err := os.Mkdir(r.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if !r.hasRepo {
		return r
	}
	gateGit(t, r.repo, "init", "-b", "main")
	gateWrite(t, filepath.Join(r.repo, "lighthouse.toml"), "[project]\nlanguage = \"nl\"\n[review]\nroborev = \"local\"\n")
	gateWrite(t, filepath.Join(r.repo, ".roborev.toml"), "")
	gateGit(t, r.repo, "add", ".")
	gateGit(t, r.repo, "commit", "-m", "base")
	gateGit(t, r.repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	gateGit(t, r.repo, "checkout", "-b", "feat")
	gateCommitLines(t, r.repo, 100, "x.go")
	bin := filepath.Join(dir, "bin")
	for _, tool := range []string{"roborev", "codex"} {
		gateWrite(t, filepath.Join(bin, tool), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	r.vars["base"] = gateGit(t, r.repo, "rev-parse", "origin/main")
	return r
}

func (r *gateRun) stateDir() string { return filepath.Join(r.repo, ".git", "lighthouse", "roborev") }

// sub replaces the $placeholders in every string of v.
func (r *gateRun) sub(v any) any {
	switch x := v.(type) {
	case string:
		if r.hasRepo {
			r.vars["head"] = gateGit(r.t, r.repo, "rev-parse", "HEAD")
			r.vars["state"] = r.stateDir()
		}
		keys := make([]string, 0, len(r.vars))
		for k := range r.vars {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		for _, k := range keys {
			x = strings.ReplaceAll(x, "$"+k, r.vars[k])
		}
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = r.sub(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = r.sub(e)
		}
		return out
	}
	return v
}

func gateStr(m map[string]any, key string) string { s, _ := m[key].(string); return s }

func (r *gateRun) setup(actions []map[string]any) {
	t := r.t
	t.Helper()
	for _, raw := range actions {
		act := r.sub(raw).(map[string]any)
		switch do := gateStr(act, "do"); do {
		case "toml":
			gateWrite(t, filepath.Join(r.repo, "lighthouse.toml"), gateStr(act, "text"))
		case "remove":
			if err := os.Remove(filepath.Join(r.repo, gateStr(act, "path"))); err != nil {
				t.Fatal(err)
			}
		case "path":
			onlyGit := filepath.Join(r.tmp, "only-git")
			if err := os.Mkdir(onlyGit, 0o755); err != nil {
				t.Fatal(err)
			}
			git, err := exec.LookPath("git")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(git, filepath.Join(onlyGit, "git")); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", onlyGit)
		case "git":
			var args []string
			for _, a := range act["args"].([]any) {
				args = append(args, a.(string))
			}
			gateGit(t, r.repo, args...)
		case "commit_lines":
			gateCommitLines(t, r.repo, int(act["count"].(float64)), gateStr(act, "name"))
		case "commit_files":
			for name, spec := range act["files"].(map[string]any) {
				gateWrite(t, filepath.Join(r.repo, name), strings.Repeat("x\n", int(spec.(map[string]any)["lines"].(float64))))
			}
			gateGit(t, r.repo, "add", ".")
			gateGit(t, r.repo, "commit", "-m", gateStr(act, "message"))
		case "fix_commit":
			gateCommitLines(t, r.repo, 1, gateStr(act, "name"))
			gateGit(t, r.repo, "commit", "--amend", "-m", gateStr(act, "subject"))
			r.vars[gateStr(act, "as")] = gateGit(t, r.repo, "rev-parse", "HEAD")
		case "raw":
			text, ok := act["text"].(string)
			if !ok {
				data, err := os.ReadFile(filepath.Join(gateFixtureDir, gateStr(act, "fixture")))
				if err != nil {
					t.Fatal(err)
				}
				text = string(data)
			}
			gateWrite(t, filepath.Join(r.stateDir(), gateStr(act, "name")+".md"), text)
		case "state":
			data, _ := json.Marshal(act["json"])
			gateWrite(t, filepath.Join(r.stateDir(), gateStr(act, "name")+".json"), string(data))
		case "remove_state":
			if err := os.Remove(filepath.Join(r.stateDir(), gateStr(act, "name")+".json")); err != nil {
				t.Fatal(err)
			}
		case "file":
			text, ok := act["text"].(string)
			if !ok {
				data, _ := json.Marshal(act["json"])
				text = string(data)
			}
			gateWrite(t, gateStr(act, "path"), text)
		case "worktree":
			wt := filepath.Join(r.tmp, gateStr(act, "name"))
			gateGit(t, r.repo, "worktree", "add", wt, "-b", gateStr(act, "branch"))
			r.vars["wt"] = wt
		default:
			t.Fatalf("unknown setup action %q", do)
		}
	}
}

// gatePlain turns ordered JSON values into the shapes encoding/json decodes to.
func gatePlain(v any) any {
	switch x := v.(type) {
	case *orderedjson.Object:
		m := map[string]any{}
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			m[k] = gatePlain(e)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = gatePlain(e)
		}
		return out
	case int:
		return float64(x)
	case json.Number:
		f, _ := x.Float64()
		return f
	}
	return v
}

func (r *gateRun) subset(got map[string]any, want any) {
	r.t.Helper()
	for k, w := range r.sub(want).(map[string]any) {
		if !reflect.DeepEqual(got[k], w) {
			r.t.Errorf("%s = %#v, want %#v", k, got[k], w)
		}
	}
}

func (r *gateRun) checkStateFile(spec map[string]any) {
	r.t.Helper()
	path := filepath.Join(r.stateDir(), gateStr(spec, "name")+".json")
	if p := gateStr(spec, "path"); p != "" {
		path = r.sub(p).(string)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	var got any
	if err := json.Unmarshal(data, &got); err != nil {
		r.t.Fatal(err)
	}
	if want, ok := spec["equals"]; ok && !reflect.DeepEqual(got, r.sub(want)) {
		r.t.Errorf("%s = %#v, want %#v", path, got, r.sub(want))
	}
	if want, ok := spec["subset"]; ok {
		r.subset(got.(map[string]any), want)
	}
}

func (r *gateRun) checkAbsent(step map[string]any) {
	r.t.Helper()
	for _, name := range gateStrings(step["absent"]) {
		if _, err := os.Stat(filepath.Join(r.stateDir(), name+".json")); err == nil {
			r.t.Errorf("state file %s exists, want none", name)
		}
	}
	for _, p := range gateStrings(r.sub(step["absent_path"])) {
		if _, err := os.Stat(p); err == nil {
			r.t.Errorf("%s exists, want none", p)
		}
	}
}

func gateStrings(v any) []string {
	var out []string
	list, _ := v.([]any)
	for _, e := range list {
		out = append(out, e.(string))
	}
	return out
}

func gateBudget(t *testing.T, spec any) roborev.Budget {
	if s, ok := spec.(string); ok && s == "forbidden" {
		return func() (int, string) { t.Helper(); t.Fatal("budget must not be consulted"); return 0, "" }
	}
	pair := spec.([]any)
	return func() (int, string) { return int(pair[0].(float64)), pair[1].(string) }
}

func decisionsOf(t *testing.T, rows any) []any {
	t.Helper()
	objs := []map[string]any{}
	for _, row := range rows.([]any) {
		c := row.([]any)
		objs = append(objs, map[string]any{"path": c[0], "line": c[1], "severity": c[2], "uitkomst": c[3], "commit": c[4], "reden": c[5]})
	}
	data, _ := json.Marshal(objs)
	parsed, err := orderedjson.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.([]any)
}

func (r *gateRun) step(step map[string]any) {
	t := r.t
	t.Helper()
	root := r.repo
	if s := gateStr(step, "root"); s != "" {
		root = r.sub(s).(string)
	}
	switch op := gateStr(step, "op"); op {
	case "prepare":
		r.setup(gateMapList(step["setup"]))
	case "count_lines":
		if got := roborev.CountCodeLines(gateStr(step, "numstat")); float64(got) != step["expect"].(float64) {
			t.Errorf("CountCodeLines = %d, want %v", got, step["expect"])
		}
	case "gate":
		out, err := roborev.Gate(root, gateBudget(t, step["budget"]))
		if err != nil {
			t.Fatal(err)
		}
		got := gatePlain(out).(map[string]any)
		if want, ok := step["expect_exact"]; ok && !reflect.DeepEqual(got, want) {
			t.Errorf("gate = %#v, want %#v", got, want)
		}
		r.subset(got, gateOrEmpty(step["expect"]))
		for _, k := range gateStrings(step["absolute"]) {
			if !filepath.IsAbs(got[k].(string)) {
				t.Errorf("%s = %q is not absolute", k, got[k])
			}
		}
		if step["raw_writable"] == true {
			gateWrite(t, got["raw"].(string), "review")
		}
		if save, ok := step["save"].(map[string]any); ok {
			for key, name := range save {
				r.vars[name.(string)] = got[key].(string)
			}
		}
		if spec, ok := step["state_file"].(map[string]any); ok {
			r.checkStateFile(spec)
		}
	case "classify":
		text, ok := step["text"].(string)
		if !ok {
			data, err := os.ReadFile(filepath.Join(gateFixtureDir, gateStr(step, "fixture")))
			if err != nil {
				t.Fatal(err)
			}
			text = string(data)
		}
		if rep, ok := step["replace"].([]any); ok {
			text = strings.ReplaceAll(text, rep[0].(string), rep[1].(string))
		}
		got := gatePlain(roborev.Classify(text).JSON()).(map[string]any)
		if want, ok := step["expect_exact"]; ok && !reflect.DeepEqual(got, want) {
			t.Errorf("classify = %#v, want %#v", got, want)
		}
		r.subset(got, gateOrEmpty(step["expect"]))
		findings, _ := got["findings"].([]any)
		if want, ok := step["triples"].([]any); ok {
			var triples []any
			for _, f := range findings {
				m := f.(map[string]any)
				triples = append(triples, []any{m["path"], m["line"], m["severity"]})
			}
			if !reflect.DeepEqual(triples, want) {
				t.Errorf("findings = %#v, want %#v", triples, want)
			}
		}
		if p, ok := step["text_prefix"].([]any); ok {
			if text := findings[int(p[0].(float64))].(map[string]any)["text"].(string); !strings.HasPrefix(text, p[1].(string)) {
				t.Errorf("finding text %q lacks prefix %q", text, p[1])
			}
		}
	case "fixed":
		got, err := roborev.FixedFindings(root)
		if err != nil {
			t.Fatal(err)
		}
		var rows []any
		for _, f := range got {
			rows = append(rows, []any{f.Path, float64(f.Line), f.Severity, f.Commit})
		}
		if want := r.sub(step["expect"]); !reflect.DeepEqual(rows, want) {
			t.Errorf("fixed = %#v, want %#v", rows, want)
		}
	case "body_line":
		got, err := roborev.BodyLine(root)
		if err != nil || got != step["expect"] {
			t.Errorf("BodyLine = %q, %v; want %q", got, err, step["expect"])
		}
	case "record":
		r.record(root, step)
	case "cli":
		r.cli(step)
	default:
		t.Fatalf("unknown op %q", op)
	}
}

func gateOrEmpty(v any) any {
	if v == nil {
		return map[string]any{}
	}
	return v
}

func gateMapList(v any) []map[string]any {
	var out []map[string]any
	for _, e := range v.([]any) {
		out = append(out, e.(map[string]any))
	}
	return out
}

func (r *gateRun) record(root string, step map[string]any) {
	t := r.t
	t.Helper()
	var o roborev.RecordOptions
	if s, ok := step["skip"].(string); ok {
		o.Skip = &s
	}
	if s, ok := step["sha"].(string); ok {
		o.Sha = &s
	}
	if rows, ok := step["decisions"]; ok {
		o.Decisions = decisionsOf(t, rows)
	}
	if s, ok := step["out"].(string); ok {
		o.Out = r.sub(s).(string)
	}
	if s, ok := step["raw"].(string); ok {
		o.Raw = r.sub(s).(string)
	}
	if s, ok := step["range"].(string); ok {
		s = r.sub(s).(string)
		o.Range = &s
	}
	written, err := roborev.Record(root, o)
	if want, ok := step["error"].(string); ok {
		var value *roborev.ValueError
		if !errors.As(err, &value) || !strings.Contains(err.Error(), want) {
			t.Errorf("Record error = %v, want a ValueError containing %q", err, want)
		}
	} else {
		if err != nil {
			t.Fatal(err)
		}
		if want, ok := step["returns"].(string); ok && written != r.sub(want) {
			t.Errorf("Record = %q, want %q", written, r.sub(want))
		}
	}
	r.checkAbsent(step)
	if spec, ok := step["state_file"].(map[string]any); ok {
		r.checkStateFile(spec)
	}
}

func (r *gateRun) cli(step map[string]any) {
	t := r.t
	t.Helper()
	cwd := r.repo
	if gateStr(step, "cwd") == "tmp" {
		cwd = r.tmp
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(r.tmp))
	}
	t.Chdir(cwd)
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newRoborevGateCommand(func() (int, string) {
		t.Helper()
		t.Fatal("budget must not be consulted")
		return 0, ""
	}))
	args := []string{"roborev-gate"}
	for _, a := range r.sub(step["args"]).([]any) {
		args = append(args, a.(string))
	}
	root.SetArgs(args)
	code := execute(root, &errOut)
	if want := int(step["exit"].(float64)); code != want {
		t.Fatalf("exit %d, want %d; stderr %q", code, want, errOut.String())
	}
	if want, ok := step["stdout_json"]; ok {
		var got any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("stdout %q is not JSON: %v", out.String(), err)
		}
		if step["subset"] == true {
			r.subset(got.(map[string]any), want)
		} else if !reflect.DeepEqual(got, r.sub(want)) {
			t.Errorf("stdout = %#v, want %#v", got, r.sub(want))
		}
	}
	if want, ok := step["stdout"].(string); ok && strings.TrimSpace(out.String()) != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if want, ok := step["stderr_starts"].(string); ok && !strings.HasPrefix(errOut.String(), want) {
		t.Errorf("stderr = %q, want prefix %q", errOut.String(), want)
	}
	if step["stderr_nonempty"] == true && strings.TrimSpace(errOut.String()) == "" {
		t.Error("stderr is empty")
	}
	if r.hasRepo {
		r.checkAbsent(step)
		if spec, ok := step["state_file"].(map[string]any); ok {
			r.checkStateFile(spec)
		}
	}
}

func TestRoborevGateFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(gateCaseDir, "*.json"))
	if err != nil || len(files) != 76 {
		t.Fatalf("fixtures: %d files, %v; want 76", len(files), err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var c gateCase
			if err := json.Unmarshal(data, &c); err != nil {
				t.Fatal(err)
			}
			r := newGateRun(t, c)
			r.setup(c.Setup)
			for _, step := range c.Steps {
				r.step(step)
			}
		})
	}
}

func TestRoborevGateUsageAndRegistration(t *testing.T) {
	root := NewRoot(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{})
	for _, sub := range []string{"gate", "fixed", "body-line", "classify", "record"} {
		if cmd, _, err := root.Find([]string{"roborev-gate", sub}); err != nil || cmd.Name() != sub {
			t.Errorf("Find(roborev-gate %s) = %v, %v", sub, cmd, err)
		}
	}
	for name, args := range map[string][]string{
		"no subcommand": {},
		"unknown flag":  {"gate", "--x"},
		"extra args":    {"fixed", "a"},
		"two paths":     {"classify", "a", "b"},
	} {
		var out, errOut bytes.Buffer
		cmd := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
		cmd.SetOut(&out)
		cmd.SetErr(&errOut)
		cmd.AddCommand(newRoborevGateCommand(func() (int, string) { return 0, "" }))
		cmd.SetArgs(append([]string{"roborev-gate"}, args...))
		if code := execute(cmd, &errOut); code != 2 || out.String() != "" {
			t.Errorf("%s: code %d, stdout %q; want 2 and nothing", name, code, out.String())
		}
	}
}

// The gate output keeps the key order of the script: verdict first, then its own keys, then the
// paths, on one line.
func TestRoborevGateStdoutIsOneOrderedLine(t *testing.T) {
	r := newGateRun(t, gateCase{})
	r.setup([]map[string]any{{"do": "remove", "path": ".roborev.toml"}})
	t.Chdir(r.repo)
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.AddCommand(newRoborevGateCommand(func() (int, string) { return 0, "" }))
	cmd.SetArgs([]string{"roborev-gate", "gate"})
	if code := execute(cmd, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	want := `{"verdict": "skip", "reason": "geen .roborev.toml", "state": "` + r.stateDir() + `/feat.json", "raw": "` + r.stateDir() + `/feat.md"}` + "\n"
	if out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
}

// The production budget route, without the Budget seam: the fake codex exits at once, so the
// strict Codex read fails and the gate skips as not measurable.
func TestRoborevGateProductionBudgetNotMeasurable(t *testing.T) {
	r := newGateRun(t, gateCase{})
	t.Setenv("HOME", filepath.Join(r.tmp, "home"))
	t.Setenv("CODEX_HOME", filepath.Join(r.tmp, "codex-home"))
	t.Chdir(r.repo)
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.AddCommand(newRoborevGateCommand(roborev.DefaultBudget()))
	cmd.SetArgs([]string{"roborev-gate", "gate"})
	if code := execute(cmd, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("stdout %q: %v", out.String(), err)
	}
	reason, _ := got["reason"].(string)
	if got["verdict"] != "skip" || !strings.HasPrefix(reason, "Codex-budget: not_measurable - not measurable") {
		t.Errorf("gate = %#v, want a not_measurable skip", got)
	}
}
