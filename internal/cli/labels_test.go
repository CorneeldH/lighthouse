package cli

import (
	"bytes"
	"context"
	"errors"
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
	// failDelete is the 1-based DeleteLabel call that fails without deleting; 0 is never.
	failDelete, deletes int
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
	f.deletes++
	if f.deletes == f.failDelete {
		return errors.New("delete refused")
	}
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
	text := "[forge]\nrepo = \"Owner/repo\"\n\n[forge.board]\nproject_number = 1\nproject_id = \"P\"\nstatus_field_id = \"F\"\n\n[forge.board.status_options]\n\"To do\" = \"a\"\n\"In progress\" = \"b\"\nDone = \"c\"\n\n[paths]\nlabels = \"labels.md\"\n\n[labels]\nneeds_shaping = \"needs-shaping\"\nneeds_decision = \"needs-decision\"\nneeds_testing = \"needs-testing\"\nneeds_mind = \"3-needs-mind\"\nneeds_hands = \"3-needs-hands\"\n"
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

// useOldToml gives the harness a repo whose [labels] still holds the three old keys with the
// given prefix, and no needs_mind or needs_hands; the forge carries labels with that prefix.
func (h *labelsHarness) useOldToml(prefix string) {
	h.t.Helper()
	text := "[forge]\nrepo = \"Owner/repo\"\n\n[paths]\nlabels = \"labels.md\"\n\n[labels]\n" +
		"needs_shaping = \"" + prefix + "needs-shaping\"\nneeds_decision = \"" + prefix + "needs-decision\"\nneeds_testing = \"" + prefix + "needs-testing\"\n"
	if err := os.WriteFile(h.toml, []byte(text), 0o600); err != nil {
		h.t.Fatal(err)
	}
	tax := "| Label | Colour |\n|---|---|\n| `" + prefix + "needs-shaping` | `D93F0B` |\n| `" + prefix + "needs-testing` | `0E8A16` |\n"
	if err := os.WriteFile(filepath.Join(h.dir, "labels.md"), []byte(tax), 0o600); err != nil {
		h.t.Fatal(err)
	}
	h.fake.labels = []string{prefix + "needs-shaping", prefix + "needs-decision", prefix + "needs-testing"}
	h.fake.issues = []forge.PlanIssue{{Number: 1, Labels: []string{prefix + "needs-shaping"}}}
}

func TestLabelsMigrateDerivesNewNamesFromOldPrefix(t *testing.T) {
	h := newLabelsHarness(t, false)
	h.useOldToml("3-")
	if code, out := h.run("", "--stage", "files"); code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	got, _ := os.ReadFile(h.toml)
	if !strings.Contains(string(got), `needs_mind = "3-needs-mind"`) || !strings.Contains(string(got), `needs_hands = "3-needs-hands"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
	tax, _ := os.ReadFile(filepath.Join(h.dir, "labels.md"))
	if !strings.Contains(string(tax), "`3-needs-mind`") || !strings.Contains(string(tax), "`3-needs-hands`") {
		t.Fatalf("taxonomy:\n%s", tax)
	}
	if _, err := config.Load(h.toml); err != nil {
		t.Fatalf("config.Load: %v", err)
	}
}

func TestLabelsMigrateNoPrefixKeepsDefaults(t *testing.T) {
	h := newLabelsHarness(t, false)
	h.useOldToml("")
	if code, out := h.run("", "--stage", "files"); code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	got, _ := os.ReadFile(h.toml)
	if !strings.Contains(string(got), `needs_mind = "needs-mind"`) || strings.Contains(string(got), "needs_hands") {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
}

func TestLabelsMigrateKeepsExplicitNewKeys(t *testing.T) {
	h := newLabelsHarness(t, false) // harness toml has needs_mind and needs_hands with 3-
	b, _ := os.ReadFile(h.toml)
	text := strings.ReplaceAll(string(b), "needs_shaping = \"needs-shaping\"", "needs_shaping = \"9-needs-shaping\"")
	if err := os.WriteFile(h.toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out := h.run("", "--stage", "files"); code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	got, _ := os.ReadFile(h.toml)
	if !strings.Contains(string(got), `needs_mind = "3-needs-mind"`) {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
}

func TestLabelsMigrateStageAll(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.useOldToml("3-")
	code, out := h.run("", "--yes")
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !slices.Equal(h.fake.issues[0].Labels, []string{"3-needs-mind"}) {
		t.Fatalf("issue labels = %v", h.fake.issues[0].Labels)
	}
	if slices.Contains(h.fake.labels, "3-needs-shaping") || !slices.Contains(h.fake.labels, "3-needs-mind") {
		t.Fatalf("forge labels = %v", h.fake.labels)
	}
	got, _ := os.ReadFile(h.toml)
	if !strings.Contains(string(got), `needs_mind = "3-needs-mind"`) || strings.Contains(string(got), "needs_shaping") {
		t.Fatalf("lighthouse.toml:\n%s", got)
	}
}

var threeOld = []string{"--old", "needs_shaping=3-needs-shaping", "--old", "needs_decision=3-needs-decision", "--old", "needs_testing=3-needs-testing"}

func TestLabelsMigrateRerunAfterPartialFailure(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.useOldToml("3-")
	h.fake.failDelete = 1
	code, out := h.run("", "--yes")
	if code != 1 || !strings.Contains(out, "delete refused") {
		t.Fatalf("first run code=%d out=%q, want 1", code, out)
	}
	if got, _ := os.ReadFile(h.toml); strings.Contains(string(got), "needs_shaping") {
		t.Fatalf("files were not written before the failing delete:\n%s", got)
	}
	// Without --old the old names fall back to the defaults: the run must say so loudly.
	code, out = h.run("", "--stage", "forge", "--yes")
	if code != 0 || !strings.Contains(out, "forge: nothing to do") || !strings.Contains(out, "--old") || !strings.Contains(out, "built-in") {
		t.Fatalf("rerun without --old code=%d out=%q, want a warning naming --old and the built-in names", code, out)
	}
	if !slices.Contains(h.fake.labels, "3-needs-decision") {
		t.Fatalf("forge labels = %v, want the old labels still there", h.fake.labels)
	}
	code, out = h.run("", append([]string{"--stage", "forge", "--yes"}, threeOld...)...)
	if code != 0 || strings.Contains(out, "built-in") {
		t.Fatalf("rerun with --old code=%d out=%q, want 0 and no warning", code, out)
	}
	for _, n := range []string{"3-needs-shaping", "3-needs-decision", "3-needs-testing"} {
		if slices.Contains(h.fake.labels, n) {
			t.Fatalf("forge labels = %v, want %s deleted", h.fake.labels, n)
		}
	}
}

func TestLabelsMigrateNoWarningWhenTomlHasOldKeys(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.fake.issues, h.fake.labels = nil, []string{"3-needs-mind"}
	code, out := h.run("", "--stage", "forge", "--yes")
	if code != 0 || !strings.Contains(out, "nothing to do") || strings.Contains(out, "built-in") {
		t.Fatalf("code=%d out=%q, want nothing to do without a warning", code, out)
	}
}

func TestLabelsMigrateOldFlagErrors(t *testing.T) {
	h := newLabelsHarness(t, true)
	for _, bad := range []string{"needs_shaping", "needs_shaping=", "bogus=x"} {
		code, out := h.run("", "--stage", "forge", "--yes", "--old", bad)
		if code != 2 || h.fake.writes != 0 {
			t.Fatalf("--old %q: code=%d out=%q writes=%d, want 2 and no writes", bad, code, out, h.fake.writes)
		}
	}
}

func TestLabelsMigrateOldFlagOverridesToml(t *testing.T) {
	h := newLabelsHarness(t, true)
	h.fake.labels = []string{"legacy-shaping"}
	h.fake.issues = []forge.PlanIssue{{Number: 1, Labels: []string{"legacy-shaping"}}}
	code, out := h.run("", "--stage", "forge", "--yes", "--old", "needs_shaping=legacy-shaping")
	if code != 0 || !slices.Equal(h.fake.issues[0].Labels, []string{"3-needs-mind"}) || slices.Contains(h.fake.labels, "legacy-shaping") {
		t.Fatalf("code=%d out=%q labels=%v issue=%v", code, out, h.fake.labels, h.fake.issues[0].Labels)
	}
}

func TestLabelsMigrateWithoutBoardSkipsStep3(t *testing.T) {
	h := newLabelsHarness(t, true)
	text := "[forge]\nrepo = \"Owner/repo\"\n\n[paths]\nlabels = \"labels.md\"\n\n[labels]\nneeds_shaping = \"needs-shaping\"\nneeds_decision = \"needs-decision\"\nneeds_testing = \"needs-testing\"\n"
	if err := os.WriteFile(h.toml, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	h.haveDB = false
	h.fake.issues = []forge.PlanIssue{{Number: 5, Labels: []string{"needs-shaping"},
		BlockedBy: []forge.Blocker{{Issue: 2, State: "OPEN"}}}}
	code, out := h.run("", "--stage", "forge", "--yes")
	if code != 0 || strings.Contains(out, "status #") {
		t.Fatalf("code=%d out=%q, want 0 and no status step", code, out)
	}
}
