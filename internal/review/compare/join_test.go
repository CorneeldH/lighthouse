package compare

import (
	"fmt"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/telemetry"
	"github.com/CorneeldH/lighthouse/internal/telemetry/report"
)

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

func jtraj(issue int, route string, status telemetry.RecoveryStatus) *report.Trajectory {
	t := &report.Trajectory{Repo: "r/x", Issue: issue, Route: str(route), PlanTasks: num(2)}
	if status != "" {
		t.Recoveries = []report.Recovery{{PR: issue, Status: string(status)}}
		t.Closed = status != telemetry.RecoveryOpen
	}
	return t
}

func jrec(issue int, source string) Record {
	return Record{Repo: "r/x", Issue: issue, Linked: true,
		Findings: []Finding{f("a", source, "MAJOR", "MAJOR", "adopted", 1)}}
}

func classOf(t *testing.T, cs []Class, key, value string) Class {
	t.Helper()
	for _, c := range cs {
		if c.Key == key && c.Value == value {
			return c
		}
	}
	t.Fatalf("no class %s=%s in %+v", key, value, cs)
	return Class{}
}

func many(n int, source string) ([]Record, []*report.Trajectory) {
	var rs []Record
	var ts []*report.Trajectory
	for i := 1; i <= n; i++ {
		rs = append(rs, jrec(i, source))
		ts = append(ts, jtraj(i, "sdd", telemetry.RecoveryRecovered))
	}
	return rs, ts
}

func TestByClass_UnderTenIsNoConclusion(t *testing.T) {
	rs, ts := many(9, "both")
	if c := classOf(t, ByClass(rs, ts), "route", "sdd"); c.Trajectories != 9 || c.Conclusion {
		t.Errorf("9 trajectories: want no conclusion, got %+v", c)
	}
	rs, ts = many(10, "both")
	if c := classOf(t, ByClass(rs, ts), "route", "sdd"); c.Trajectories != 10 || !c.Conclusion {
		t.Errorf("10 trajectories in both groups: want a conclusion, got %+v", c)
	}
}

func TestByClass_EachGroupNeedsTen(t *testing.T) {
	// 12 trajectories see a Codex finding, only 9 an Opus finding.
	rs, ts := many(12, "codex")
	for i := 0; i < 9; i++ {
		rs[i] = jrec(i+1, "both")
	}
	if c := classOf(t, ByClass(rs, ts), "route", "sdd"); c.Conclusion {
		t.Errorf("Opus group has 9: want no conclusion, got %+v", c)
	}
}

func TestByClass_UnlinkedRecordCountsFindingsNotRecovery(t *testing.T) {
	rs := []Record{jrec(1, "codex"), {Repo: "r/x", Issue: 2, Linked: false,
		Findings: []Finding{f("b", "codex", "MAJOR", "", "adopted", 1)}}}
	ts := []*report.Trajectory{jtraj(1, "sdd", telemetry.RecoveryRecovered), jtraj(2, "sdd", telemetry.RecoveryRecovered)}
	cs := ByClass(rs, ts)
	c := classOf(t, cs, "route", "sdd")
	if c.Trajectories != 1 || c.Recovered != 1 || c.Result.Sources["codex"].N != 1 {
		t.Errorf("linked record only in the class: %+v", c)
	}
	u := classOf(t, cs, "route", Unknown)
	if u.Result.Sources["codex"].N != 1 || u.Trajectories != 0 || u.Recovered != 0 || u.Open != 0 {
		t.Errorf("unlinked: findings yes, recovery no: %+v", u)
	}
}

func TestByClass_RecoveryOnlyFromClosedMeasurements(t *testing.T) {
	rs := []Record{jrec(1, "codex"), jrec(2, "codex"), jrec(3, "codex"), jrec(4, "codex")}
	open := jtraj(2, "sdd", telemetry.RecoveryOpen)
	mixed := jtraj(3, "sdd", telemetry.RecoveryRecovered)
	mixed.Closed = false
	mixed.Recoveries = append(mixed.Recoveries, report.Recovery{PR: 9, Status: string(telemetry.RecoveryOpen)})
	ts := []*report.Trajectory{jtraj(1, "sdd", telemetry.RecoveryRecovered), open, mixed, jtraj(4, "sdd", "")}
	c := classOf(t, ByClass(rs, ts), "route", "sdd")
	if c.Recovered != 1 || c.Open != 2 || c.Trajectories != 4 {
		t.Errorf("want 1 recovered, 2 open, 4 trajectories: %+v", c)
	}
}

func TestByClass_ClassKeys(t *testing.T) {
	tr := jtraj(1, "sdd", telemetry.RecoveryNone)
	tr.Area = str("web")
	tr.PlanTasks = num(5)
	cs := ByClass([]Record{jrec(1, "both"), jrec(1, "codex")}, []*report.Trajectory{tr})
	for _, kv := range [][2]string{{"route", "sdd"}, {"plangrootte", "medium"}, {"gebied", "web"}, {"repo", "r/x"}} {
		if c := classOf(t, cs, kv[0], kv[1]); c.Trajectories != 1 {
			t.Errorf("%v: a trajectory with two records counts once, got %+v", kv, c)
		}
	}
	if fmt.Sprint(classOf(t, cs, "repo", "r/x").Result.Sources["both"].N) != "1" {
		t.Error("findings of both records must be tallied")
	}
}

func TestByClass_RecoveredNeedsClosedTrajectory(t *testing.T) {
	tr := jtraj(1, "sdd", telemetry.RecoveryRecovered)
	tr.Closed = false
	c := classOf(t, ByClass([]Record{jrec(1, "codex")}, []*report.Trajectory{tr}), "route", "sdd")
	if c.Recovered != 0 || c.Open != 0 {
		t.Errorf("not closed: neither recovered nor open, got %+v", c)
	}
}
