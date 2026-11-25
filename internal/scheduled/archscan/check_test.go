package archscan

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
)

// Classification: io (real git in a clone, the real forge client against the fake GitHub of
// forgetest). The regression surface is what Cleanup, Check and Finish accept, what they read
// from the forge rather than from the corpus, and what they post. Each test names the
// production change that makes it fail; every Check failure also asserts that nothing was
// written to the forge.

// testRun is the run id of the open run row of scanEnv.
const testRun = "scan-20261122T0500"

// scanRig is a rig whose repo carries the headings file the job reads from the work tree.
func scanRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "pack", "core", "headings.toml"))
	if err != nil {
		t.Fatal(err)
	}
	put(t, r.repo, ".lighthouse/headings.toml", string(b))
	r.commit()
	return r
}

// scanEnv is the env of a run of schedule "scan" whose open run row carries testRun.
func (r *rig) scanEnv() scheduled.Env {
	env := r.env()
	env.Schedule = repoconfig.Schedule{Name: "scan", Job: Name}
	rec := scheduled.Record{Verdict: scheduled.Verdict{RunID: "scan-20260801T0500"}}
	open := scheduled.Record{Verdict: scheduled.Verdict{RunID: testRun}}
	// The open row of this schedule is not the last one: a closed row of it and an open row of
	// another schedule follow.
	env.History = []runs.Row{
		{ID: 3, Schedule: "scan", Open: true, Verdict: open.Encode()},
		{ID: 4, Schedule: "scan", Verdict: rec.Encode()},
		{ID: 5, Schedule: "other", Open: true, Verdict: scheduled.Record{Verdict: scheduled.Verdict{RunID: "scan-other"}}.Encode()},
	}
	return env
}

// seedScan seeds a scan issue of the job with marker m.
func (r *rig) seedScan(n int, state, created, closed string, m scheduled.Marker, content string) string {
	m.Kind, m.Version = Name, 1
	body := m.String() + "\n\n\n" + content
	r.issue(n, jobLogin, state, created, closed, body, scanLabel)
	return body
}

func (r *rig) writeResult(body string) {
	r.t.Helper()
	put(r.t, r.repo, resultPath, body)
}

// candidate is one candidate block with one claim.
func candidate(name, key string, rejected bool) string {
	box := "- [ ] Afgewezen" // lighthouse: allow-language
	if rejected {
		box = "- [x] Afgewezen" // lighthouse: allow-language
	}
	return "### " + name + " (`" + key + "`)\n\n" + box + "\n- Telt 12 bestanden [niet nagemeten]\n\n" // lighthouse: allow-language
}

// resultDoc is a valid result file around candidates and still-open bullets; an empty part
// gets a line that says so.
func resultDoc(candidates, stillOpen string) string {
	if candidates == "" {
		candidates = "Geen kandidaten.\n\n" // lighthouse: allow-language
	}
	if stillOpen == "" {
		stillOpen = "Geen.\n" // lighthouse: allow-language
	}
	return "# Scan van 2026-11-22\n\n## Kandidaten\n\n" + candidates + // lighthouse: allow-language
		"## Nog open uit eerdere scans\n\n" + stillOpen + // lighthouse: allow-language
		"\n## Methode\n\nVenster 3 maanden.\n\n## Topaanbeveling\n\nBegin met de eerste.\n" // lighthouse: allow-language
}

func resultExists(t *testing.T, r *rig) bool {
	t.Helper()
	_, err := os.Lstat(filepath.Join(r.repo, resultPath))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

func TestAllowedOnlyResultFile(t *testing.T) {
	// Fails if Allowed lets any path but the result file through, or refuses the result file.
	j := bind(t)
	if !j.Allowed(resultPath) {
		t.Fatalf("Allowed(%s) = false", resultPath)
	}
	for _, p := range []string{corpusPath, "x/" + resultPath, "README.md", "internal/a.go", ".lighthouse/headings.toml", resultPath + ".bak"} {
		if j.Allowed(p) {
			t.Errorf("Allowed(%s) = true, want false", p)
		}
	}
}

func TestCleanupMissingResultFails(t *testing.T) {
	// Fails if Cleanup accepts a run without a result file the way git status shows it (an
	// ignored file is never seen by Postcheck), or excuses it while the open scan issue
	// carries another run.
	t.Run("no file", func(t *testing.T) {
		r := scanRig(t)
		r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err == nil || !strings.Contains(err.Error(), resultPath) {
			t.Fatalf("Cleanup = %v, want an error naming %s", err, resultPath)
		}
	})
	t.Run("ignored file", func(t *testing.T) {
		r := scanRig(t)
		put(t, r.repo, ".gitignore", resultPath+"\n")
		r.commit()
		r.writeResult(resultDoc(candidate("A", "a", false), ""))
		if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err == nil {
			t.Fatal("Cleanup = nil for a result git status does not show")
		}
	})
}

func TestCleanupEmptyResultFails(t *testing.T) {
	// Fails if Cleanup lets an empty or blank result through: Postcheck would then check it as
	// one changed path, or with zero, end the run done.
	for _, body := range []string{"", " \n\t\n"} {
		r := scanRig(t)
		r.writeResult(body)
		if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err == nil || !strings.Contains(err.Error(), "empty") {
			t.Fatalf("Cleanup(%q) = %v, want an error about an empty result", body, err)
		}
	}
}

func TestCleanupMissingFileAfterPublish(t *testing.T) {
	// Fails if Cleanup fails a repeated Postcheck after Finish already published (the open scan
	// issue carries the run id of the open run row of this schedule), or reads the run id from
	// another row than the open one of env.Schedule.Name.
	r := scanRig(t)
	r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: testRun, Base: "abc", First: "2026-08-01", Scans: 2}, "x")
	if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err != nil {
		t.Fatalf("Cleanup = %v, want nil: this run already published", err)
	}
	if len(r.srv.Posts) != 0 {
		t.Fatalf("Cleanup wrote to the forge: %+v", r.srv.Posts)
	}
	t.Run("run id of a closed row", func(t *testing.T) {
		r := scanRig(t)
		r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err == nil {
			t.Fatal("Cleanup = nil while the scan issue carries the run of a closed row")
		}
	})
	t.Run("run id of another schedule", func(t *testing.T) {
		r := scanRig(t)
		r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-other", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
		if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err == nil {
			t.Fatal("Cleanup = nil while the scan issue carries the run of another schedule")
		}
	})
}

func TestCleanupRemovesCorpus(t *testing.T) {
	// Fails if Cleanup leaves the corpus (it would then be a second changed path), or removes the
	// result file.
	r := scanRig(t)
	if err := bind(t).Stage(context.Background(), r.scanEnv(), scheduled.Verdict{RunID: testRun}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	r.writeResult(resultDoc(candidate("A", "a", false), ""))
	if err := bind(t).Cleanup(context.Background(), r.scanEnv(), r.repo); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(r.repo, filepath.FromSlash(corpusPath))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("corpus still there: %v", err)
	}
	if !resultExists(t, r) {
		t.Fatal("Cleanup removed the result file")
	}
}

// checkFails runs Check and wants an error containing want, and nothing written to the forge.
func checkFails(t *testing.T, r *rig, want string) {
	t.Helper()
	err := bind(t).Check(context.Background(), r.scanEnv(), r.repo)
	if err == nil {
		t.Fatalf("Check = nil, want an error containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("Check = %v, want an error containing %q", err, want)
	}
	if len(r.srv.Posts) != 0 {
		t.Fatalf("Check wrote to the forge: %+v", r.srv.Posts)
	}
}

func checkPasses(t *testing.T, r *rig) {
	t.Helper()
	if err := bind(t).Check(context.Background(), r.scanEnv(), r.repo); err != nil {
		t.Fatalf("Check: %v", err)
	}
}

func TestCheckValidResultPasses(t *testing.T) {
	// The baseline of the Check failures below: the same fixtures, valid, pass.
	r := scanRig(t)
	r.issue(12, "someone", "open", "2026-10-01T05:00:00Z", "", "work")
	r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1},
		resultDoc(candidate("A", "a", true), ""))
	r.writeResult(resultDoc(candidate("A", "a", true)+candidate("B", "b", false), "- #12 — Oude kandidaat\n")) // lighthouse: allow-language
	checkPasses(t, r)
}

func TestCheckMoreThanOneFile(t *testing.T) {
	// Fails if Check accepts a second changed path next to the result file, untracked or a
	// change to a tracked file.
	t.Run("untracked", func(t *testing.T) {
		r := scanRig(t)
		r.writeResult(resultDoc(candidate("A", "a", false), ""))
		put(t, r.repo, "notes.md", "x\n")
		checkFails(t, r, "notes.md")
	})
	t.Run("tracked", func(t *testing.T) {
		r := scanRig(t)
		r.writeResult(resultDoc(candidate("A", "a", false), ""))
		put(t, r.repo, "lighthouse.toml", defaultTOM+"# changed\n")
		checkFails(t, r, "lighthouse.toml")
	})
}

func TestCheckMissingHeading(t *testing.T) {
	// Fails if Check does not parse the result against the headings of the work tree.
	r := scanRig(t)
	doc := strings.Replace(resultDoc(candidate("A", "a", false), ""), "## Methode\n", "## Iets anders\n", 1) // lighthouse: allow-language
	r.writeResult(doc)
	checkFails(t, r, "scan_method")
}

func TestCheckClaimMarks(t *testing.T) {
	// Fails if Check lets a claim through without a mark, with two marks, or a measured claim
	// without its command or outcome.
	for name, claim := range map[string]string{
		"no mark":           "- Telt 12 bestanden",                                             // lighthouse: allow-language
		"two marks":         "- Telt 12 bestanden [niet nagemeten] [niet meetbaar vanaf hier]", // lighthouse: allow-language
		"measured, no cmd":  "- Telt 12 [nagemeten: `` → 12]",                                  // lighthouse: allow-language
		"measured, no outc": "- Telt 12 [nagemeten: `rg -c x` → ]",                             // lighthouse: allow-language
	} {
		t.Run(name, func(t *testing.T) {
			r := scanRig(t)
			block := "### A (`a`)\n\n- [ ] Afgewezen\n" + claim + "\n\n" // lighthouse: allow-language
			r.writeResult(resultDoc(block, ""))
			checkFails(t, r, "line ")
		})
	}
}

func TestCheckStillOpenNumberMissing(t *testing.T) {
	// Fails if Check does not look up a still-open number on the forge.
	r := scanRig(t)
	r.writeResult(resultDoc(candidate("A", "a", false), "- #99 — Weg\n")) // lighthouse: allow-language
	checkFails(t, r, "#99")
}

func TestCheckStillOpenNumberClosed(t *testing.T) {
	// Fails if Check accepts a still-open number that is closed on the forge.
	r := scanRig(t)
	r.issue(12, "someone", "closed", "2026-10-01T05:00:00Z", "2026-11-01T05:00:00Z", "work")
	r.writeResult(resultDoc(candidate("A", "a", false), "- #12 — Dicht\n")) // lighthouse: allow-language
	checkFails(t, r, "#12 is not open")
}

func TestCheckStillOpenIsTheScanIssue(t *testing.T) {
	// Fails if Check lets the open scan issue count as the issue a candidate falls under.
	r := scanRig(t)
	r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
	r.writeResult(resultDoc(candidate("A", "a", false), "- #7 — Scan\n"))
	checkFails(t, r, "#7 is the scan issue")
}

func TestCheckDroppedTickAgainstLiveBody(t *testing.T) {
	// Fails if Check compares the ticks with the corpus instead of the live body of the open scan
	// issue: the corpus has the box open, the forge has it ticked since, the result has it open.
	r := scanRig(t)
	open := r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1},
		resultDoc(candidate("A", "a", false), ""))
	env := r.scanEnv()
	if err := bind(t).Stage(context.Background(), env, scheduled.Verdict{RunID: testRun}); err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// The user ticks the box after the corpus was written.
	ticked := strings.Replace(open, "- [ ] Afgewezen", "- [x] Afgewezen", 1) // lighthouse: allow-language
	if err := env.Forge.UpdateIssueBody(context.Background(), 7, ticked); err != nil {
		t.Fatal(err)
	}
	r.srv.Posts = nil
	r.writeResult(resultDoc(candidate("A", "a", false), ""))
	if err := bind(t).Cleanup(context.Background(), env, r.repo); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	checkFails(t, r, "`a`")
	t.Run("candidate left out", func(t *testing.T) {
		r.srv.Posts = nil
		r.writeResult(resultDoc(candidate("B", "b", false), ""))
		checkFails(t, r, "`a`")
	})
	t.Run("tick kept", func(t *testing.T) {
		r.writeResult(resultDoc(candidate("A", "a", true), ""))
		checkPasses(t, r)
	})
}

func TestCheckDroppedTickWithoutOpenUsesLastClosed(t *testing.T) {
	// Fails if Check, without an open scan issue, compares the ticks with another closed scan
	// issue than the last closed one, or with none.
	seed := func(r *rig) {
		// #3 closed last but was created after #2: the oldest by creation is not the reference.
		r.seedScan(3, "closed", "2026-06-01T05:00:00Z", "2026-10-01T05:00:00Z", scheduled.Marker{Run: "scan-20260601T0500", Base: "abc", First: "2026-06-01", Scans: 1},
			resultDoc(candidate("A", "a", true), ""))
		r.seedScan(2, "closed", "2026-05-01T05:00:00Z", "2026-07-01T05:00:00Z", scheduled.Marker{Run: "scan-20260501T0500", Base: "abc", First: "2026-05-01", Scans: 1},
			resultDoc(candidate("B", "b", true), ""))
	}
	r := scanRig(t)
	seed(r)
	r.writeResult(resultDoc(candidate("A", "a", false), ""))
	checkFails(t, r, "`a`")
	t.Run("older closed is not the reference", func(t *testing.T) {
		r := scanRig(t)
		seed(r)
		r.writeResult(resultDoc(candidate("A", "a", true)+candidate("B", "b", false), ""))
		checkPasses(t, r)
	})
}

func TestCheckBodyOver60000(t *testing.T) {
	// Fails if Check measures the result without the marker and warning line, or not at all.
	r := scanRig(t)
	r.seedScan(7, "open", "2026-08-01T05:00:00Z", "", scheduled.Marker{Run: "scan-20260801T0500", Base: "abc", First: "2026-08-01", Scans: 1}, "x")
	base := resultDoc(candidate("A", "a", false), "")
	// Within 60000 on its own, over it with the header.
	pad := strings.Repeat("é", maxBody-len([]rune(base))-20)
	r.writeResult(strings.Replace(base, "Venster 3 maanden.", "Venster 3 maanden. "+pad, 1)) // lighthouse: allow-language
	checkFails(t, r, "60000")
	t.Run("within the limit with the header", func(t *testing.T) {
		pad := strings.Repeat("é", maxBody-len([]rune(base))-500)
		r.writeResult(strings.Replace(base, "Venster 3 maanden.", "Venster 3 maanden. "+pad, 1)) // lighthouse: allow-language
		checkPasses(t, r)
	})
}

func TestCheckSecretInResult(t *testing.T) {
	// Fails if Check does not run the secret scan on the result file itself.
	r := scanRig(t)
	key := "AKIA" + strings.Repeat("Q", 16)
	r.writeResult(strings.Replace(resultDoc(candidate("A", "a", false), ""), "Venster 3 maanden.", "Sleutel "+key, 1)) // lighthouse: allow-language
	checkFails(t, r, "AWS access key")
	err := bind(t).Check(context.Background(), r.scanEnv(), r.repo)
	if err != nil && strings.Contains(err.Error(), key) {
		t.Fatal("the error carries the secret")
	}
}
