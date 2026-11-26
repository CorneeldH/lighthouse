package review

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/testpath"
)

// The cases that patch the code itself: the host judgment, a
// failing rename and the skill directories. Hosts come from a real host.toml here, where
// pytest patched `recognize`.

func TestRecordSkillDirsAreLighthouse(t *testing.T) {
	want := map[string]string{
		"plan": ".claude/skills/lighthouse-review-plan",
		"spec": ".claude/skills/lighthouse-review-spec",
	}
	if !reflect.DeepEqual(DefaultSkillDirs, want) {
		t.Errorf("DefaultSkillDirs = %v, want %v", DefaultSkillDirs, want)
	}
}

func hostToml(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.toml")
	writeFile(t, path, text)
	t.Setenv("LIGHTHOUSE_HOST_TOML", path)
}

func TestReserveWritesWithoutHostToml(t *testing.T) {
	data := t.TempDir()
	t.Setenv("LIGHTHOUSE_DATA", data)
	t.Setenv("XDG_DATA_HOME", "")
	testpath.SetHome(t, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "missing.toml"))
	attemptDir := t.TempDir()
	writeFile(t, filepath.Join(attemptDir, "attempt.json"),
		`{"kind":"plan","chain_key":"chain","attempt":"att"}`)

	var stderr strings.Builder
	Reserve(attemptDir, DefaultDeps(), &stderr)

	if _, err := os.Stat(filepath.Join(data, "plan-review", "chain", ".att.tmp", "attempt.json")); err != nil {
		t.Errorf("reservation not written without host.toml: %v", err)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestRecognizeController(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"true", "controller = true\n", true},
		{"false", "controller = false\ntailscale_ip = \"192.0.2.1\"\n", false},
		{"absent without ip", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostToml(t, "[host]\nname = \"box\"\n"+tt.body)
			got, err := config.Recognize()
			if err != nil || got != tt.want {
				t.Errorf("Recognize() = %v, %v; want %v, nil", got, err, tt.want)
			}
		})
	}
}

func TestRecordWriteFailureLeavesOnlyStaging(t *testing.T) {
	attempt := t.TempDir()
	writeFile(t, filepath.Join(attempt, "attempt.json"), `{}`)
	writeFile(t, filepath.Join(attempt, "round-1.json"), `{"findings":[]}`)
	dataRoot := t.TempDir()
	record := orderedjson.NewObject()
	record.Set("kind", "plan")
	record.Set("chain_key", "chain")
	record.Set("attempt", "att")

	boom := errors.New("schijf vol")
	defer func(old func(string, string) error) { rename = old }(rename)
	rename = func(string, string) error { return boom }
	_, err := Write(attempt, record, dataRoot, "")
	if !errors.Is(err, boom) {
		t.Fatalf("Write error = %v, want %v", err, boom)
	}
	entries, _ := os.ReadDir(filepath.Join(dataRoot, "plan-review", "chain"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{".att.tmp"}; !reflect.DeepEqual(names, want) {
		t.Errorf("chain dir holds %v, want only %v", names, want)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "plan-review", "chain", ".att.tmp", "record.json")); err != nil {
		t.Errorf("staging has no record.json: %v", err)
	}
}

func TestStartReservesAnAttemptWithTheReviewedCommit(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeFile(t, filepath.Join(repo, "docs", "plan.md"), "# plan\n")
	run("init", "-q")
	run("add", "-A")
	run("commit", "-qm", "init")
	deps := Deps{Now: func() time.Time { return time.Date(2026, 11, 8, 12, 30, 45, 0, time.UTC) }}

	tmp := t.TempDir()
	attempt, err := Start(docsDir+"plan.md", repo, tmp, "plan", deps)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`/plan-review/plan/20261108T123045Z-[0-9a-f]{6}$`).MatchString(attempt) {
		t.Errorf("attempt = %s", attempt)
	}
	meta, err := ReadMeta(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if meta["reviewed_sha"] != run("rev-parse", "HEAD") || meta["plan"] != docsDir+"plan.md" || meta["chain_key"] != "plan" {
		t.Errorf("attempt.json = %v", meta)
	}

	writeFile(t, filepath.Join(repo, "docs", "plan.md"), "# changed\n")
	_, err = Start(docsDir+"plan.md", repo, tmp, "plan", deps)
	var invalid *InvalidError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "commit first") {
		t.Errorf("Start on an uncommitted plan: error = %v, want an InvalidError with 'commit first'", err)
	}
}

func TestBuildRecordTrajectory(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeFile(t, filepath.Join(repo, "docs", "plan.md"), "# plan\n")
	skill := filepath.Join(repo, ".claude", "skills", "lighthouse-review-plan")
	writeFile(t, filepath.Join(skill, "reviewer-prompt.md"), "prompt\n")
	writeFile(t, filepath.Join(skill, "findings.schema.json"), "{}\n")
	run("init", "-q")
	run("add", "-A")
	run("commit", "-qm", "init")
	deps := Deps{
		CodexVersion: func() *string { v := "codex 1"; return &v },
		Now:          func() time.Time { return time.Date(2026, 11, 8, 12, 30, 45, 0, time.UTC) },
	}
	attempt, err := Start(docsDir+"plan.md", repo, t.TempDir(), "plan", deps)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(attempt, "round-1.json"), `{"review_markdown":"x","status":"s","findings":[]}`)
	in := RecordInput{
		Repo: repo, Outcome: "converged", ReviewMode: "codex", FinalSHA: run("rev-parse", "HEAD"),
		SkillDir: DefaultSkillDirs["plan"], CodexHome: t.TempDir(),
	}

	rec, err := BuildRecord(attempt, in, deps)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := rec.Get("trajectory"); !ok || v != nil {
		t.Errorf("trajectory without input = %v (present %v), want present and nil", v, ok)
	}
	if v, _ := rec.Get("schema_version"); fmt.Sprint(v) != "6" {
		t.Errorf("schema_version = %v, want 6", v)
	}

	in.Trajectory = &Trajectory{Repo: "CorneeldH/lighthouse", Issue: 376}
	rec, err = BuildRecord(attempt, in, deps)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := rec.Get("trajectory")
	o, ok := v.(*orderedjson.Object)
	if !ok {
		t.Fatalf("trajectory = %#v, want an object", v)
	}
	if r, _ := o.Get("repo"); r != "CorneeldH/lighthouse" {
		t.Errorf("trajectory.repo = %v", r)
	}
	if n, _ := o.Get("issue"); fmt.Sprint(n) != "376" {
		t.Errorf("trajectory.issue = %v", n)
	}
}

func TestRecordAssignmentFields(t *testing.T) {
	repo := t.TempDir()
	run := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	writeFile(t, filepath.Join(repo, "docs", "plan.md"), "# plan\n")
	skill := filepath.Join(repo, ".claude", "skills", "lighthouse-review-plan")
	writeFile(t, filepath.Join(skill, "reviewer-prompt.md"), "prompt\n")
	writeFile(t, filepath.Join(skill, "findings.schema.json"), "{}\n")
	run("init", "-q")
	run("add", "-A")
	run("commit", "-qm", "init")
	deps := Deps{
		CodexVersion: func() *string { v := "codex 1"; return &v },
		Now:          func() time.Time { return time.Date(2026, 11, 8, 12, 30, 45, 0, time.UTC) },
	}
	attempt, err := Start(docsDir+"plan.md", repo, t.TempDir(), "plan", deps)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(attempt, "round-1.json"), `{"review_markdown":"x","status":"s","findings":[]}`)
	base := RecordInput{
		Repo: repo, Outcome: "converged", ReviewMode: "codex", FinalSHA: run("rev-parse", "HEAD"),
		SkillDir: DefaultSkillDirs["plan"], CodexHome: t.TempDir(),
	}
	yes, no := true, false
	const variant = "variant:plan_review=codex"

	tests := []struct {
		name        string
		assignment  string
		applied     *bool
		want        string
		wantApplied any
		wantErr     bool
	}{
		{"none given", "", nil, "standard", nil, false},
		{"standard", "standard", nil, "standard", nil, false},
		{"variant applied", variant, &yes, variant, true, false},
		{"variant not applied", variant, &no, variant, false, false},
		{"variant without applied", variant, nil, "", nil, true},
		{"unknown form", "codex", &yes, "", nil, true},
		{"variant without component and choice", "variant:foo", &yes, "", nil, true},
		{"variant with empty component", "variant:=codex", &yes, "", nil, true},
		{"variant with empty choice", "variant:plan_review=", &yes, "", nil, true},
		{"variant with unknown component", "variant:code_review=codex", &yes, "", nil, true},
		{"variant with unknown choice", "variant:plan_review=gpt", &yes, "", nil, true},
		// A variant of another component than this review's kind is rejected, not recorded.
		{"variant of another component", "variant:spec_review=codex", &yes, "", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			in.Assignment, in.AssignmentApplied = tt.assignment, tt.applied
			rec, err := BuildRecord(attempt, in, deps)
			if tt.wantErr {
				if err == nil {
					t.Fatal("BuildRecord: nil error, want one")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if v, ok := rec.Get("assignment"); !ok || v != tt.want {
				t.Errorf("assignment = %v (present %v), want %q", v, ok, tt.want)
			}
			if v, ok := rec.Get("assignment_applied"); !ok || v != tt.wantApplied {
				t.Errorf("assignment_applied = %v (present %v), want %v", v, ok, tt.wantApplied)
			}
			if v, _ := rec.Get("schema_version"); fmt.Sprint(v) != "6" {
				t.Errorf("schema_version = %v, want 6", v)
			}
		})
	}
}
