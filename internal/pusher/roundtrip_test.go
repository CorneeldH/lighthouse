package pusher

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/roborev"
	"github.com/CorneeldH/lighthouse/internal/usage"
)

// pushReviewFixture is the fixture with the branch on origin, one unpushed fix commit, a
// lighthouse.toml with a local roborev review and fake roborev and codex on PATH: what a fix-pr
// run has when it runs V2b with `roborev-gate gate --push`.
func pushReviewFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	tgit(t, f.wt, "push", "-q", "origin", branch)
	f.commit("fix.txt", "fix\n")
	write(t, filepath.Join(f.wt, "lighthouse.toml"), "[project]\nlanguage = \"nl\"\n[review]\nroborev = \"local\"\n")
	write(t, filepath.Join(f.wt, ".roborev.toml"), "")
	bin := filepath.Join(f.dir, "bin")
	for _, tool := range []string{"roborev", "codex"} {
		write(t, filepath.Join(bin, tool), "#!/bin/sh\n")
		if err := os.Chmod(filepath.Join(bin, tool), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	return f
}

func codexOK() (int, string) { return 0, `{"decision": "codex", "reason": "ok"}` }

func gateString(t *testing.T, o *orderedjson.Object, key string) string {
	t.Helper()
	v, _ := o.Get(key)
	s, ok := v.(string)
	if !ok {
		t.Fatalf("gate output %s = %v, want a string", key, v)
	}
	return s
}

// A record that `roborev-gate gate --push` and `record --out <state>` write is the record check 5
// accepts: the writer and the reader of push-<head>.json cannot drift apart.
func TestRoborevPushRecordRoundTrip(t *testing.T) {
	f := pushReviewFixture(t)
	head := f.head()

	out, err := roborev.GatePush(f.wt, codexOK, noClaudeAlarm)
	if err != nil {
		t.Fatal(err)
	}
	if v := gateString(t, out, "verdict"); v != "run" {
		t.Fatalf("verdict %q, want run", v)
	}
	state, rng := gateString(t, out, "state"), gateString(t, out, "range")
	if _, err := roborev.Record(f.wt, roborev.RecordOptions{Sha: &head, Decisions: []any{}, Out: state, Range: &rng}); err != nil {
		t.Fatal(err)
	}

	req := f.req
	req.CloneDir = f.wt // the worktree holds every commit the check reads
	if red := reviewed(context.Background(), req, head); red != nil {
		t.Fatalf("check 5 refuses the push record: %s", red.Why)
	}
	if got := newestPushRecord(filepath.Dir(state)); got != state {
		t.Errorf("check 5 reads %q, the gate wrote %q", got, state)
	}
}

// A skip of the push gate is red at check 5.
func TestRoborevPushSkipRecordIsRed(t *testing.T) {
	f := pushReviewFixture(t)
	head := f.head()
	if err := os.Remove(filepath.Join(f.wt, ".roborev.toml")); err != nil {
		t.Fatal(err)
	}
	out, err := roborev.GatePush(f.wt, codexOK, noClaudeAlarm)
	if err != nil {
		t.Fatal(err)
	}
	if v := gateString(t, out, "verdict"); v != "skip" {
		t.Fatalf("verdict %q, want skip", v)
	}

	req := f.req
	req.CloneDir = f.wt
	red := reviewed(context.Background(), req, head)
	wantStep(t, red, StepRoborev)
}

// `roborev-gate body-line --push`, the handoff's roborev line, says done exactly when check 5
// accepts the push records.
func TestRoborevBodyLinePushAgreesWithCheck5(t *testing.T) {
	for _, listed := range []bool{true, false} {
		t.Run(map[bool]string{true: "fix listed", false: "fix unlisted"}[listed], func(t *testing.T) {
			f := pushReviewFixture(t)
			old := f.head()
			fix := f.commit("fix2.txt", "fix\n")
			record := done(old)
			if listed {
				record = done(old, fix[:9])
			}
			f.record("push-"+old+".json", record)

			line, err := roborev.BodyLinePush(f.wt)
			if err != nil {
				t.Fatal(err)
			}
			req := f.req
			req.CloneDir = f.wt
			red := reviewed(context.Background(), req, fix)
			marker := line == "<!-- roborev-lokaal: "+old+" -->"
			if marker != (red == nil) || marker != listed {
				t.Errorf("body-line %q, check 5 red %v; want both to accept only a listed fix", line, red)
			}
		})
	}
}

func noClaudeAlarm() (usage.Alarm, bool) { return usage.Alarm{}, false }
