package scheduledtest

import (
	"context"
	"errors"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: pure logic. Fails if Job stops returning its configured results, stops
// matching Allowed by prefix, or stops recording calls in order.
func TestJob(t *testing.T) {
	boom := errors.New("boom")
	j := &Job{Verdict: scheduled.Verdict{Skip: "no"}, CheckErr: boom, AllowedPre: []string{"docs/"}}
	ctx := context.Background()
	if v, _ := j.Gate(ctx, scheduled.Env{}); v.Skip != "no" {
		t.Errorf("Gate verdict = %+v", v)
	}
	if err := j.Check(ctx, scheduled.Env{}, ""); !errors.Is(err, boom) {
		t.Errorf("Check = %v", err)
	}
	if !j.Allowed("docs/a.md") || j.Allowed("src/a.go") {
		t.Error("Allowed does not follow the prefixes")
	}
	_ = j.Stage(ctx, scheduled.Env{}, scheduled.Verdict{})
	if got := j.Calls(); len(got) != 3 || got[0] != "Gate" || got[1] != "Check" || got[2] != "Stage" {
		t.Errorf("Calls = %v", got)
	}
	if j.Name() != "test" {
		t.Errorf("Name = %q", j.Name())
	}
}
