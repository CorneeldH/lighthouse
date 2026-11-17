package scheduled_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Classification: io/filesystem (a real git repository) for StartState, pure for Record. The
// regression surface is what the tick records at a scheduled start for the check after the run:
// the start commit and a digest of the git configuration the agent could tamper with.

func TestStartStateRecordsHeadAndGitConfig(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run := workplace.ExecRunner()
	sha, state, err := scheduled.StartState(ctx, run, f.work)
	if err != nil {
		t.Fatalf("StartState: %v", err)
	}
	if sha != f.start {
		t.Errorf("start sha = %q, want HEAD %q", sha, f.start)
	}
	if state == "" {
		t.Fatal("git state is empty")
	}
	if _, again, err := scheduled.StartState(ctx, run, f.work); err != nil || again != state {
		t.Errorf("second StartState = %q, %v; want the same state %q", again, err, state)
	}

	// Each change below plants code git would run on add, checkout or push.
	steps := []struct {
		name string
		do   func()
	}{
		{"config", func() { f.git("config", "filter.x.clean", "cat") }},
		{"info/attributes", func() {
			p := filepath.Join(f.work, ".git", "info", "attributes")
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("* filter=x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, s := range steps {
		s.do()
		_, changed, err := scheduled.StartState(ctx, run, f.work)
		if err != nil {
			t.Fatalf("StartState after %s: %v", s.name, err)
		}
		if changed == state {
			t.Errorf("git state unchanged after a change of %s", s.name)
		}
		state = changed
	}
}

// A git-state file that cannot be read fails the start state with its base name, never the
// workplace path: the error becomes the run's reason.
func TestStartStateUnreadableFileNamesNoPath(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.work, ".git", "info", "attributes"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := scheduled.StartState(context.Background(), workplace.ExecRunner(), f.work)
	if err == nil || !strings.Contains(err.Error(), "read attributes: is a directory") || strings.Contains(err.Error(), f.work) {
		t.Errorf("StartState error = %v, want the base name and cause without the path", err)
	}
}

func TestStartStateOutsideARepository(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", os.TempDir())
	dir := t.TempDir()
	if _, _, err := scheduled.StartState(context.Background(), workplace.ExecRunner(), dir); err == nil {
		t.Error("StartState outside a repository: no error")
	}
}

func TestRecordRoundTrip(t *testing.T) {
	in := scheduled.Record{
		Verdict:    scheduled.Verdict{Mode: "full", Args: "--all", Score: 7},
		MaxRuntime: 2 * time.Hour,
		GitState:   "abc",
	}
	text := in.Encode()
	out, err := scheduled.DecodeRecord(text)
	if err != nil {
		t.Fatalf("DecodeRecord(%s): %v", text, err)
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
	if _, err := scheduled.DecodeRecord("{"); err == nil || !strings.Contains(err.Error(), "record") {
		t.Errorf("DecodeRecord of broken JSON: error = %v, want a record error", err)
	}
}
