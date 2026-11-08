package review

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// The cases of test_plan_review_record.py that patch the module itself: the host judgment, a
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

func TestRecordHostRefusalUnknownHost(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "missing.toml"))
	got := HostRefusal("plan")
	if got == nil {
		t.Fatal("HostRefusal = nil, want a refusal")
	}
	for _, want := range []string{"PLAN-REVIEW NOT RECORDED", "host unknown: no host.toml", "falls outside the measurement"} {
		if !strings.Contains(*got, want) {
			t.Errorf("refusal = %q, want it to contain %q", *got, want)
		}
	}
}

func TestRecordHostRefusalOtherHost(t *testing.T) {
	hostToml(t, "[host]\nname = \"laptop\"\ntailscale_ip = \"192.0.2.1\"\n") // TEST-NET-1: on no interface
	got := HostRefusal("spec")
	if got == nil {
		t.Fatal("HostRefusal = nil, want a refusal")
	}
	for _, want := range []string{"SPEC-REVIEW NOT RECORDED", "not on the host from host.toml", "falls outside the measurement"} {
		if !strings.Contains(*got, want) {
			t.Errorf("refusal = %q, want it to contain %q", *got, want)
		}
	}
}

func TestRecordHostRefusalWithoutTailscaleIP(t *testing.T) {
	hostToml(t, "[host]\nname = \"server\"\n")
	got := HostRefusal("spec")
	if got == nil || !strings.Contains(*got, "not on the host from host.toml") {
		t.Errorf("HostRefusal = %v, want 'not on the host from host.toml'", got)
	}
}

func TestRecordHostRefusalRecognizedHost(t *testing.T) {
	if got := hostRefusalWith("plan", func() (bool, error) { return true, nil }); got != nil {
		t.Errorf("HostRefusal on the recognised host = %q, want nil", *got)
	}
	if got := hostRefusalWith("plan", func() (bool, error) { return false, nil }); got == nil {
		t.Error("HostRefusal on another host = nil, want a refusal")
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
	attempt, err := Start("docs/plan.md", repo, tmp, "plan", deps)
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
	if meta["reviewed_sha"] != run("rev-parse", "HEAD") || meta["plan"] != "docs/plan.md" || meta["chain_key"] != "plan" {
		t.Errorf("attempt.json = %v", meta)
	}

	writeFile(t, filepath.Join(repo, "docs", "plan.md"), "# changed\n")
	_, err = Start("docs/plan.md", repo, tmp, "plan", deps)
	var invalid *InvalidError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "commit first") {
		t.Errorf("Start on an uncommitted plan: error = %v, want an InvalidError with 'commit first'", err)
	}
}
