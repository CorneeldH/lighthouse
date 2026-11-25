package history

import (
	"fmt"
	"reflect"
	"testing"
)

func bashRec(sess, branch, cwd, cmd string) Record {
	return Record{File: "f.jsonl", Line: 1, Type: "assistant", SessionID: sess, GitBranch: branch, Cwd: cwd, Commands: []string{cmd}}
}

func feed(d Detector, recs ...Record) {
	for _, r := range recs {
		d.Add(r)
	}
}

func TestIsTrunkName(t *testing.T) {
	for _, n := range []string{"main", "master", "develop", "trunk", "release/r1"} {
		if !isTrunkName(n, "") {
			t.Errorf("%s must be trunk", n)
		}
	}
	if isTrunkName("stable", "") || isTrunkName("issue-1-x", "main") || isTrunkName("", "") {
		t.Error("false positive")
	}
	if !isTrunkName("stable", "stable") {
		t.Error("default branch must be trunk")
	}
}

func TestTrunk(t *testing.T) {
	t.Run("shifting trunk", func(t *testing.T) {
		d := newTrunk(Env{})
		d.Add(bashRec("s", "main", "/r", "git push origin HEAD:release/r1"))
		for i := 0; i < 41; i++ {
			d.Add(bashRec("s", "main", "/r", "git push origin HEAD:release/r5.11"))
		}
		for i := 0; i < 9; i++ {
			d.Add(bashRec("s", "main", "/r", "git push origin HEAD:release/r1"))
		}
		f := d.Finish(Env{})
		if f.Key != "trunk" || f.Status != "found" || f.Confidence != "high" || f.Proposal.Kind != "advice" {
			t.Fatalf("%+v", f)
		}
		if got := f.Measured["trunk_names"]; !reflect.DeepEqual(got, []string{"release/r1", "release/r5.11"}) {
			t.Fatalf("names %v", got)
		}
		pb := f.Measured["pushes_by_branch"].(map[string]int)
		if pb["release/r5.11"] != 41 || pb["release/r1"] != 10 {
			t.Fatalf("%v", pb)
		}
		if f.Measured["trunk_push_ratio"].(float64) != 1 || f.Meaning == "" || f.Proposal.Text == "" {
			t.Fatalf("%+v", f)
		}
		tp, kp := d.Counts()
		if tp != 51 || kp != 51 {
			t.Fatalf("counts %d %d", tp, kp)
		}
	})
	t.Run("unknown branch", func(t *testing.T) {
		d := newTrunk(Env{})
		d.Add(bashRec("s", "main", "/r", "git -C /wt push"))
		d.Add(bashRec("s", "feat", "/r", "git push"))
		f := d.Finish(Env{})
		if f.Measured["unknown_branch"].(int) != 1 {
			t.Fatalf("%v", f.Measured)
		}
		if f.Measured["trunk_push_ratio"].(float64) != 0 || f.Status != "none" || f.Confidence != "high" {
			t.Fatalf("%+v", f)
		}
		tp, kp := d.Counts()
		if tp != 0 || kp != 1 {
			t.Fatalf("counts %d %d", tp, kp)
		}
	})
	t.Run("known cwd resolves", func(t *testing.T) {
		d := newTrunk(Env{})
		d.Add(Record{SessionID: "s", GitBranch: "issue-1", Cwd: "/wt", Type: "user"})
		d.Add(bashRec("s", "main", "/r", "cd /wt && git push"))
		f := d.Finish(Env{})
		if f.Measured["unknown_branch"].(int) != 0 || f.Measured["pushes_by_branch"].(map[string]int)["issue-1"] != 1 {
			t.Fatalf("%v", f.Measured)
		}
	})
	t.Run("default branch from env", func(t *testing.T) {
		env := Env{TrunkNames: []string{"stable"}}
		d := newTrunk(env)
		d.Add(bashRec("s", "stable", "/r", "git push"))
		if f := d.Finish(env); f.Status != "found" {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("low confidence", func(t *testing.T) {
		d := newTrunk(Env{})
		d.Add(bashRec("s", "main", "/r", "git -C /a push"))
		d.Add(bashRec("s", "main", "/r", "git -C /b commit -m x"))
		d.Add(bashRec("s", "main", "/r", "git push origin HEAD:main"))
		if f := d.Finish(Env{}); f.Confidence != "low" || f.Status != "found" {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("none", func(t *testing.T) {
		d := newTrunk(Env{})
		feed(d, bashRec("s", "main", "/r", "ls -la"), Record{Type: "user"})
		f := d.Finish(Env{})
		if f.Status != "none" || f.Measured["trunk_push_ratio"].(float64) != 0 {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("commits counted, no records", func(t *testing.T) {
		d := newTrunk(Env{})
		d.Add(bashRec("s", "feat", "/r", "git commit -m x"))
		d.Add(bashRec("s", "feat", "/r", "git add . && git commit -am y"))
		f := d.Finish(Env{})
		if f.Measured["commits_by_branch"].(map[string]int)["feat"] != 2 || f.Status != "none" {
			t.Fatalf("%+v", f)
		}
		_ = fmt.Sprint(f)
	})
}

func TestTrunk_FoundThreshold(t *testing.T) {
	push := func(d *trunk, branch string, n int) {
		for i := 0; i < n; i++ {
			d.Add(bashRec("s", branch, "/r", "git push"))
		}
	}
	t.Run("below ratio, one trunk push: none with counts", func(t *testing.T) {
		d := newTrunk(Env{})
		push(d, "main", 1)
		push(d, "feat", 9)
		f := d.Finish(Env{})
		if f.Status != "none" || f.Measured["trunk_push_ratio"].(float64) != 0.1 || f.Measured["pushes_by_branch"].(map[string]int)["main"] != 1 {
			t.Fatalf("%+v", f)
		}
		if len(f.Sources) != 0 {
			t.Fatalf("sources %+v", f.Sources)
		}
	})
	t.Run("at the ratio: found", func(t *testing.T) {
		d := newTrunk(Env{})
		push(d, "main", 2)
		push(d, "feat", 8)
		if f := d.Finish(Env{}); f.Status != "found" {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("two release names below the ratio: found", func(t *testing.T) {
		d := newTrunk(Env{})
		push(d, "release/r1", 1)
		push(d, "release/r2", 1)
		push(d, "feat", 18)
		if f := d.Finish(Env{}); f.Status != "found" {
			t.Fatalf("%+v", f)
		}
	})
	t.Run("one release name below the ratio: none", func(t *testing.T) {
		d := newTrunk(Env{})
		push(d, "release/r1", 1)
		push(d, "feat", 18)
		if f := d.Finish(Env{}); f.Status != "none" {
			t.Fatalf("%+v", f)
		}
	})
}
