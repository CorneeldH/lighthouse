package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// labelsFake is an in-memory forge that counts its write calls.
type labelsFake struct {
	forge.Forge
	issues    []forge.PlanIssue
	labels    []string
	status    map[int]string
	writes    int
	plannings int
	// onPlanning runs after the nth Planning call has returned its issues.
	onPlanning func(n int, f *labelsFake)
}

func (f *labelsFake) Planning(context.Context) (forge.Planning, error) {
	f.plannings++
	out := forge.Planning{}
	for _, is := range f.issues {
		is.Labels = slices.Clone(is.Labels)
		out.Issues = append(out.Issues, is)
	}
	if f.onPlanning != nil {
		f.onPlanning(f.plannings, f)
	}
	return out, nil
}

func (f *labelsFake) Labels(context.Context) ([]string, error) { return slices.Clone(f.labels), nil }

func (f *labelsFake) CreateLabel(_ context.Context, name, _ string) error {
	f.writes++
	f.labels = append(f.labels, name)
	return nil
}

func (f *labelsFake) DeleteLabel(_ context.Context, name string) error {
	f.writes++
	f.labels = slices.DeleteFunc(f.labels, func(s string) bool { return s == name })
	return nil
}

func (f *labelsFake) EditIssue(_ context.Context, n int, e forge.IssueEdit) (forge.Issue, error) {
	f.writes++
	for i := range f.issues {
		if f.issues[i].Number == n {
			f.issues[i].Labels = slices.DeleteFunc(f.issues[i].Labels, func(s string) bool { return slices.Contains(e.RemoveLabels, s) })
			f.issues[i].Labels = append(f.issues[i].Labels, e.AddLabels...)
		}
	}
	return forge.Issue{Number: n}, nil
}

func (f *labelsFake) IssueItem(_ context.Context, n int) (forge.BoardItem, bool, error) {
	s, ok := f.status[n]
	return forge.BoardItem{Issue: n, Status: s}, ok, nil
}

func (f *labelsFake) SetStatus(_ context.Context, it forge.BoardItem, status string) error {
	f.writes++
	f.status[it.Issue] = status
	return nil
}

func newLabelsFake() *labelsFake {
	return &labelsFake{
		issues: []forge.PlanIssue{{Number: 1, Labels: []string{"needs-shaping"}}},
		labels: []string{"needs-shaping", "needs-decision", "needs-testing"},
		status: map[int]string{},
	}
}

// labelsHarness is a repo directory with an old-style lighthouse.toml, a pause flag and a
// runtime directory; paused, runs and haveDB can be changed before run.
type labelsHarness struct {
	t      *testing.T
	dir    string
	toml   string
	fake   *labelsFake
	runs   []dispatch.RunView
	haveDB bool
	env    map[string]string
}

func newLabelsHarness(t *testing.T, paused bool) *labelsHarness {
	t.Helper()
	dir := t.TempDir()
	h := &labelsHarness{t: t, dir: dir, toml: filepath.Join(dir, "lighthouse.toml"), fake: newLabelsFake(), haveDB: true,
		env: map[string]string{"XDG_RUNTIME_DIR": t.TempDir(), "LIGHTHOUSE_CONTROLLER_STATE": t.TempDir()}}
	text := "[forge]\nrepo = \"Owner/repo\"\n\n[paths]\nlabels = \"labels.md\"\n\n[labels]\nneeds_shaping = \"needs-shaping\"\nneeds_decision = \"needs-decision\"\nneeds_testing = \"needs-testing\"\nneeds_mind = \"3-needs-mind\"\nneeds_hands = \"3-needs-hands\"\n"
	if err := os.WriteFile(h.toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	tax := "| Label | Colour |\n|---|---|\n| `needs-shaping` | `D93F0B` |\n| `needs-testing` | `0E8A16` |\n"
	if err := os.WriteFile(filepath.Join(dir, "labels.md"), []byte(tax), 0o600); err != nil {
		t.Fatal(err)
	}
	if paused {
		if err := park.WritePause(filepath.Join(h.env["LIGHTHOUSE_CONTROLLER_STATE"], "PAUSE"), time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	return h
}

func (h *labelsHarness) run(stdin string, args ...string) (int, string) {
	h.t.Helper()
	env := labelsEnv{
		Getenv:   func(k string) string { return h.env[k] },
		TomlPath: func() (string, error) { return h.toml, nil },
		Open:     func(context.Context, repoconfig.Repo) (forge.Forge, error) { return h.fake, nil },
		Runs: func(context.Context, string) ([]dispatch.RunView, bool, error) {
			return h.runs, h.haveDB, nil
		},
	}
	cmd := newLabelsCommandWith(env)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"migrate"}, args...))
	code := execute(cmd, &out)
	return code, out.String()
}

func TestLabelsMigrateRefusesWithoutPause(t *testing.T) {
	h := newLabelsHarness(t, false)
	code, out := h.run("y\n", "--stage", "forge", "--yes")
	if code != 1 || !strings.Contains(out, "lighthouse pause") {
		t.Fatalf("code=%d out=%q, want 1 naming lighthouse pause", code, out)
	}
	if h.fake.writes != 0 {
		t.Fatalf("%d forge writes, want none", h.fake.writes)
	}
}

func TestLabelsMigrateRefusesHeldLock(t *testing.T) {
	h := newLabelsHarness(t, true)
	lockPath, err := tickLockPath(func(k string) string { return h.env[k] })
	if err != nil {
		t.Fatal(err)
	}
	lock, err := flock.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	code, out := h.run("", "--stage", "forge", "--yes")
	if code != 1 || !strings.Contains(out, "tick is running") || h.fake.writes != 0 {
		t.Fatalf("code=%d out=%q writes=%d, want 1, tick is running, no writes", code, out, h.fake.writes)
	}
}

func TestLabelsMigrateRefusesOpenRun(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.runs = []dispatch.RunView{{Repo: "Owner/repo", Issue: 9, State: "open"}}
	code, out := h.run("", "--stage", "forge", "--yes")
	if code != 1 || !strings.Contains(out, "open") || h.fake.writes != 0 {
		t.Fatalf("code=%d out=%q writes=%d, want 1 naming the open run, no writes", code, out, h.fake.writes)
	}
}

func TestLabelsMigrateRefusesStep3WithoutDB(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.haveDB = false
	h.fake.issues = []forge.PlanIssue{{Number: 5, Labels: []string{"needs-shaping"},
		BlockedBy: []forge.Blocker{{Issue: 2, State: "OPEN"}}}}
	h.fake.status[5] = "In progress"
	code, out := h.run("", "--stage", "forge", "--yes")
	if code != 1 || !strings.Contains(out, "step 3 needs the runs database") || h.fake.writes != 0 {
		t.Fatalf("code=%d out=%q writes=%d, want 1, the step 3 refusal, no writes", code, out, h.fake.writes)
	}
}

func TestLabelsMigrateAbortsOnNo(t *testing.T) {
	h := newLabelsHarness(t, true)
	code, out := h.run("n\n", "--stage", "forge")
	if code != 1 || h.fake.writes != 0 {
		t.Fatalf("code=%d writes=%d out=%q, want 1 and no writes", code, h.fake.writes, out)
	}
	if !strings.Contains(out, "relabel #1") {
		t.Fatalf("out = %q, want the preview before the question", out)
	}
}

func TestLabelsMigrateReadsOldToml(t *testing.T) {
	h := newLabelsHarness(t, false)
	if _, err := config.Load(h.toml); err == nil || !strings.Contains(err.Error(), "lighthouse labels migrate") {
		t.Fatalf("config.Load err = %v, want it to name lighthouse labels migrate", err)
	}
	code, out := h.run("", "--stage", "files")
	if code != 0 {
		t.Fatalf("code=%d out=%q, want 0", code, out)
	}
	if _, err := config.Load(h.toml); err != nil {
		t.Fatalf("config.Load after migrate: %v", err)
	}
}

func TestLabelsMigrateFilesStage(t *testing.T) {
	h := newLabelsHarness(t, false) // no pause: files needs none
	code, out := h.run("", "--stage", "files")
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	toml, _ := os.ReadFile(h.toml)
	if !strings.Contains(string(toml), `needs_mind = "3-needs-mind"`) || strings.Contains(string(toml), "needs_shaping") {
		t.Fatalf("lighthouse.toml:\n%s", toml)
	}
	tax, _ := os.ReadFile(filepath.Join(h.dir, "labels.md"))
	if !strings.Contains(string(tax), "`3-needs-mind`") || !strings.Contains(string(tax), "`3-needs-hands`") {
		t.Fatalf("taxonomy:\n%s", tax)
	}
	if h.fake.writes != 0 || h.fake.plannings != 0 {
		t.Fatalf("forge touched: writes=%d plannings=%d", h.fake.writes, h.fake.plannings)
	}
}

func TestLabelsMigrateRereadsAfterConfirm(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.fake.onPlanning = func(n int, f *labelsFake) {
		if n == 1 { // the preview has read; issue 2 appears before the second read
			f.issues = append(f.issues, forge.PlanIssue{Number: 2, Labels: []string{"needs-decision"}})
		}
	}
	code, out := h.run("y\n", "--stage", "forge")
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	for _, is := range h.fake.issues {
		if !slices.Contains(is.Labels, "3-needs-mind") || slices.Contains(is.Labels, "needs-decision") {
			t.Fatalf("issue %d labels = %v, want the executed plan to follow the new state", is.Number, is.Labels)
		}
	}
}

// RF1: a lighthouse.toml without a [labels] table keeps working: nothing to rewrite there.
func TestLabelsMigrateFilesStageWithoutLabelsTable(t *testing.T) {
	h := newLabelsHarness(t, false)
	text := "[forge]\nrepo = \"Owner/repo\"\n\n[paths]\nlabels = \"labels.md\"\n"
	if err := os.WriteFile(h.toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out := h.run("", "--stage", "files")
	if code != 0 {
		t.Fatalf("code=%d out=%q, want 0", code, out)
	}
	if got, _ := os.ReadFile(h.toml); string(got) != text {
		t.Fatalf("lighthouse.toml changed:\n%s", got)
	}
	tax, _ := os.ReadFile(filepath.Join(h.dir, "labels.md"))
	if !strings.Contains(string(tax), "`needs-mind`") || !strings.Contains(string(tax), "`needs-hands`") {
		t.Fatalf("taxonomy:\n%s", tax)
	}
}

func TestLabelsMigrateFilesStageLabelsHeaderWithComment(t *testing.T) {
	h := newLabelsHarness(t, false)
	b, _ := os.ReadFile(h.toml)
	if err := os.WriteFile(h.toml, []byte(strings.Replace(string(b), "[labels]\n", "[labels] # human\n", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := h.run("", "--stage", "files"); code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	got, _ := os.ReadFile(h.toml)
	if strings.Contains(string(got), "needs_shaping") || !strings.Contains(string(got), `needs_mind = "3-needs-mind"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
}
