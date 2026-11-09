package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/review"
)

// The cases that carry data live as JSON in
// tests/pack/fixtures/plan_review_record/, one file per case; internal/review and this file read the
// same files. A scenario is a list of steps against a throwaway repo (_world.json). In every
// string `${name}` is a variable of the scenario: repo, tmp, tmpdir, data, home, root, PLAN,
// SPEC, base_sha (HEAD before the steps), head (HEAD now), stdout (of the last run), and per
// bound attempt `a` (its path) and `a.name`.

const fixtureDir = "../../tests/pack/fixtures/plan_review_record"

type world struct {
	PlanPath    string            `json:"plan_path"`
	SpecPath    string            `json:"spec_path"`
	RepoFiles   map[string]string `json:"repo_files"`
	CodexConfig string            `json:"codex_config"`
	CodexVer    string            `json:"codex_version"`
	Language    string            `json:"language"`
	Refusal     string            `json:"refusal"`
}

func readJSON(t *testing.T, path string, into any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, into); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func loadWorld(t *testing.T) world {
	t.Helper()
	var w world
	readJSON(t, filepath.Join(fixtureDir, "_world.json"), &w)
	return w
}

// scenario is one fixture run.
type scenario struct {
	t       *testing.T
	w       world
	vars    map[string]string
	deps    review.Deps
	saved   map[string]string
	shots   map[string][]byte
	baseSHA string
}

type step = map[string]any

func newScenario(t *testing.T, c map[string]any) *scenario {
	t.Helper()
	w := loadWorld(t)
	tmp := t.TempDir()
	s := &scenario{t: t, w: w, saved: map[string]string{}, shots: map[string][]byte{}, vars: map[string]string{
		"PLAN": w.PlanPath, "SPEC": w.SpecPath, "tmp": tmp, "tmpdir": filepath.Join(tmp, "tmp"),
		"data": filepath.Join(tmp, "data"), "home": filepath.Join(tmp, "home"),
		"repo": filepath.Join(tmp, "repo"), "root": filepath.Join(tmp, "root"), "stdout": "",
	}}
	codexHome := filepath.Join(tmp, "ch")
	mustWrite(t, filepath.Join(codexHome, "config.toml"), w.CodexConfig)
	mustWrite(t, filepath.Join(tmp, "lighthouse.toml"), "[project]\nlanguage = \""+w.Language+"\"\n")
	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(tmp, "lighthouse.toml"))
	t.Setenv("LIGHTHOUSE_HEADINGS", "../../pack/core/headings.toml")
	t.Setenv("HOME", s.vars["home"])
	t.Setenv("TMPDIR", s.vars["tmpdir"])
	t.Setenv("LIGHTHOUSE_DATA", s.vars["data"])
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("CODEX_SPECREVIEW_HOME", codexHome)
	version := w.CodexVer
	s.deps = review.Deps{CodexVersion: func() *string { return &version }, Now: time.Now}
	if c["world"] == "bare" {
		mustMkdir(t, s.vars["root"])
		t.Setenv("LIGHTHOUSE_REPO_ROOT", s.vars["root"])
	} else {
		repo := s.vars["repo"]
		for rel, text := range w.RepoFiles {
			mustWrite(t, filepath.Join(repo, rel), text)
		}
		s.git("init", "-q")
		s.git("add", "-A")
		s.git("commit", "-qm", "init")
		if files, ok := c["world_commit"].(map[string]any); ok {
			s.commit(files, "world")
		}
		s.baseSHA = s.git("rev-parse", "HEAD")
		t.Setenv("LIGHTHOUSE_REPO_ROOT", repo)
	}
	switch host, _ := c["host"].(string); host {
	case "", "ok":
		s.deps.HostRefusal = func(string) *string { return nil }
	case "refuse":
		s.deps.HostRefusal = func(kind string) *string {
			msg := strings.ReplaceAll(w.Refusal, "${KIND}", strings.ToUpper(kind))
			return &msg
		}
	default: // unknown: the real refusal, with a host.toml that is not there
		t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(tmp, "no-host.toml"))
		s.deps.HostRefusal = review.HostRefusal
	}
	return s
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func (s *scenario) git(args ...string) string {
	s.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", s.vars["repo"], "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (s *scenario) commit(files map[string]any, message string) {
	s.t.Helper()
	for rel, text := range files {
		mustWrite(s.t, filepath.Join(s.vars["repo"], s.subString(rel)), text.(string))
	}
	s.git("add", "-A")
	s.git("commit", "-qm", message)
}

var placeholder = regexp.MustCompile(`\$\{([A-Za-z_.]+)\}`)

func (s *scenario) lookup(name string) string {
	switch {
	case name == "head":
		return s.git("rev-parse", "HEAD")
	case name == "base_sha":
		return s.baseSHA
	case strings.HasSuffix(name, ".name"):
		return filepath.Base(s.vars[strings.TrimSuffix(name, ".name")])
	}
	v, ok := s.vars[name]
	if !ok {
		s.t.Fatalf("unknown variable ${%s}", name)
	}
	return v
}

func (s *scenario) subString(text string) string {
	return placeholder.ReplaceAllStringFunc(text, func(m string) string {
		return s.lookup(m[2 : len(m)-1])
	})
}

// sub replaces the variables in every string of v, keys included.
func (s *scenario) sub(v any) any {
	switch t := v.(type) {
	case string:
		return s.subString(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = s.sub(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[s.subString(k)] = s.sub(e)
		}
		return out
	}
	return v
}

func (s *scenario) path(st step, key string) string { return s.subString(st[key].(string)) }

func strs(v any) []string {
	var out []string
	for _, e := range v.([]any) {
		out = append(out, e.(string))
	}
	return out
}

func (s *scenario) runStep(st step) {
	t := s.t
	t.Helper()
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.AddCommand(newPlanReviewRecordCommand(s.deps))
	args := []string{"plan-review-record"}
	for _, a := range strs(st["args"]) {
		args = append(args, s.subString(a))
	}
	root.SetArgs(args)
	code := execute(root, &errOut)
	s.vars["stdout"] = out.String()
	exp, _ := st["expect"].(map[string]any)
	want := 0
	if c, ok := exp["code"].(float64); ok {
		want = int(c)
	}
	if code != want {
		t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out.String(), errOut.String())
	}
	s.checkOutput(exp, out.String(), errOut.String())
	if name, ok := st["save_stdout"].(string); ok {
		s.saved[name] = out.String()
	} else if name, ok := exp["save_stdout"].(string); ok {
		s.saved[name] = out.String()
	}
	if name, ok := st["bind"].(string); ok {
		s.vars[name] = strings.TrimSpace(out.String())
	}
	s.checkRecord(exp, out.String())
}

func (s *scenario) checkOutput(exp map[string]any, out, errOut string) {
	t := s.t
	t.Helper()
	if v, ok := exp["stdout_equals"].(string); ok && out != s.subString(v) {
		t.Errorf("stdout = %q, want %q", out, s.subString(v))
	}
	if v, ok := exp["stdout_starts_with"].(string); ok && !strings.HasPrefix(out, s.subString(v)) {
		t.Errorf("stdout = %q, want prefix %q", out, s.subString(v))
	}
	for _, v := range strs(orEmpty(exp["stdout_contains"])) {
		if !strings.Contains(out, s.subString(v)) {
			t.Errorf("stdout = %q, want it to contain %q", out, s.subString(v))
		}
	}
	for _, v := range strs(orEmpty(exp["stdout_not_contains"])) {
		if strings.Contains(out, s.subString(v)) {
			t.Errorf("stdout = %q, want it without %q", out, s.subString(v))
		}
	}
	if v, ok := exp["stdout_matches"].(string); ok {
		re := placeholder.ReplaceAllStringFunc(v, func(m string) string { return regexp.QuoteMeta(s.lookup(m[2 : len(m)-1])) })
		if !regexp.MustCompile(re).MatchString(out) {
			t.Errorf("stdout = %q, want a match of %s", out, re)
		}
	}
	if v, ok := exp["stdout_not_matches"].(string); ok && regexp.MustCompile(v).MatchString(out) {
		t.Errorf("stdout = %q, want no match of %s", out, v)
	}
	if v, ok := exp["stdout_equals_saved"].(string); ok && out != s.saved[v] {
		t.Errorf("stdout = %q, want the saved %q", out, s.saved[v])
	}
	if v, ok := exp["stdout_differs_from_saved"].(string); ok && out == s.saved[v] {
		t.Errorf("stdout = %q, want it to differ from the saved one", out)
	}
	for _, v := range strs(orEmpty(exp["stderr_contains"])) {
		if !strings.Contains(errOut, s.subString(v)) {
			t.Errorf("stderr = %q, want it to contain %q", errOut, s.subString(v))
		}
	}
	for _, v := range strs(orEmpty(exp["stderr_not_contains"])) {
		if strings.Contains(errOut, v) {
			t.Errorf("stderr = %q, want it without %q", errOut, v)
		}
	}
	if v, ok := exp["stderr_starts_with"].(string); ok && !strings.HasPrefix(errOut, v) {
		t.Errorf("stderr = %q, want prefix %q", errOut, v)
	}
}

func orEmpty(v any) any {
	if v == nil {
		return []any{}
	}
	return v
}

func (s *scenario) checkRecord(exp map[string]any, out string) {
	t := s.t
	t.Helper()
	_, hasRecord := exp["record"]
	_, hasAbsent := exp["record_absent"]
	_, hasFiles := exp["dir_files"]
	if !hasRecord && !hasAbsent && !hasFiles {
		return
	}
	dir := strings.TrimSpace(out)
	if hasFiles {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		if want := strs(exp["dir_files"]); !reflect.DeepEqual(names, want) {
			t.Errorf("files in %s = %v, want %v", dir, names, want)
		}
	}
	var record map[string]any
	readJSON(t, filepath.Join(dir, "record.json"), &record)
	if want, ok := s.sub(exp["record"]).(map[string]any); ok {
		for key, w := range want {
			if !reflect.DeepEqual(record[key], w) {
				t.Errorf("record[%q] = %#v, want %#v", key, record[key], w)
			}
		}
	}
	for _, key := range strs(orEmpty(exp["record_absent"])) {
		if _, present := record[key]; present {
			t.Errorf("record has key %q, want it absent", key)
		}
	}
}

func (s *scenario) roundStep(st step) {
	findings := []any{}
	if raw, ok := st["findings"]; ok {
		findings = raw.([]any)
	} else {
		for _, sev := range strs(st["severities"]) {
			findings = append(findings, map[string]any{
				"severity": sev, "cat": "code", "plan_section": "Taak 1", "problem": "p",
				"evidence": "e", "proposed_fix": "f",
			})
		}
	}
	doc := map[string]any{"review_markdown": "x", "status": "Majors only", "findings": findings}
	if v, ok := st["verified"]; ok {
		doc["verified"] = v
	}
	s.writeJSON(filepath.Join(s.vars[st["attempt"].(string)], "round-"+jsonInt(st["n"])+".json"), doc)
}

func jsonInt(v any) string {
	b, _ := json.Marshal(int(v.(float64)))
	return string(b)
}

func (s *scenario) writeJSON(path string, v any) {
	s.t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		s.t.Fatal(err)
	}
	mustWrite(s.t, path, string(data))
}

func (s *scenario) fileStep(st step) {
	path := s.path(st, "path")
	switch {
	case st["json"] != nil:
		s.writeJSON(path, s.sub(st["json"]))
	case st["bytes_hex"] != nil:
		raw, err := hex.DecodeString(st["bytes_hex"].(string))
		if err != nil {
			s.t.Fatal(err)
		}
		mustMkdir(s.t, filepath.Dir(path))
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			s.t.Fatal(err)
		}
	default:
		mustWrite(s.t, path, s.subString(st["text"].(string)))
	}
}

func (s *scenario) editJSON(st step) {
	path := s.path(st, "path")
	var doc map[string]any
	readJSON(s.t, path, &doc)
	for _, key := range strs(st["delete"]) {
		delete(doc, key)
	}
	s.writeJSON(path, doc)
}

func (s *scenario) expectFile(st step) {
	t := s.t
	t.Helper()
	path := s.path(st, "path")
	if want, ok := st["exists"].(bool); ok {
		if _, err := os.Stat(path); (err == nil) != want {
			t.Errorf("%s exists = %v, want %v", path, err == nil, want)
		}
	}
	read := func() []byte {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	if want, ok := st["text"].(string); ok && string(read()) != s.subString(want) {
		t.Errorf("%s = %q, want %q", path, read(), s.subString(want))
	}
	if want, ok := st["json"]; ok {
		var got any
		readJSON(t, path, &got)
		if !reflect.DeepEqual(got, s.sub(want)) {
			t.Errorf("%s = %#v, want %#v", path, got, s.sub(want))
		}
	}
	if other, ok := st["json_equals_file"].(string); ok {
		var got, want any
		readJSON(t, path, &got)
		readJSON(t, s.subString(other), &want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %#v, want the content of %s", path, got, other)
		}
	}
	if name, ok := st["same_as"].(string); ok && !bytes.Equal(read(), s.shots[name]) {
		t.Errorf("%s changed since snapshot %s", path, name)
	}
	if counts, ok := st["counts"].(map[string]any); ok {
		for text, n := range counts {
			if got := strings.Count(string(read()), text); got != int(n.(float64)) {
				t.Errorf("%s has %d x %q, want %v", path, got, text, n)
			}
		}
	}
}

func TestPlanReviewRecordFixtures(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(fixtureDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures in %s: %v", fixtureDir, err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		if strings.HasPrefix(name, "_") {
			continue
		}
		var c map[string]any
		readJSON(t, file, &c)
		if kind, _ := c["kind"].(string); kind == "open_section" || kind == "render_open" {
			continue // internal/review reads these
		}
		t.Run(name, func(t *testing.T) {
			s := newScenario(t, c)
			for _, raw := range c["steps"].([]any) {
				st := raw.(map[string]any)
				switch op := st["op"].(string); op {
				case "run":
					s.runStep(st)
				case "round", "round_raw":
					s.roundStep(st)
				case "file":
					s.fileStep(st)
				case "edit_json":
					s.editJSON(st)
				case "commit":
					s.commit(st["files"].(map[string]any), st["message"].(string))
				case "snapshot":
					data, err := os.ReadFile(s.path(st, "path"))
					if err != nil {
						t.Fatal(err)
					}
					s.shots[st["as"].(string)] = data
				case "unset_env":
					if err := os.Unsetenv(st["name"].(string)); err != nil {
						t.Fatal(err)
					}
				case "expect_file":
					s.expectFile(st)
				default:
					t.Fatalf("unknown op %q", op)
				}
			}
		})
	}
}
