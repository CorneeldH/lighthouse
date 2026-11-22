//go:build unix

package checkpointscan

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pushFixture is the fixture plus what Push needs: entire set up the way lighthouse is (no
// automatic push, `ckpt` as the checkpoint push remote) and the held file's place.
type pushFixture struct {
	*fixture
	ckpt           string
	out, errOutBuf bytes.Buffer
}

func newPushFixture(t *testing.T) *pushFixture {
	t.Helper()
	f := newFixture(t, true)
	p := &pushFixture{fixture: f, ckpt: filepath.Join(filepath.Dir(f.repo), "ckpt.git")}
	p.entire(`{"enabled": true, "strategy_options": {"push_sessions": false}}`,
		`{"strategy_options": {"checkpoint_push_remote": "ckpt"}}`)
	return p
}

func (p *pushFixture) entire(shared, local string) {
	p.t.Helper()
	dir := filepath.Join(p.repo, ".entire")
	noErr(p.t, os.MkdirAll(dir, 0o755))
	noErr(p.t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(shared), 0o644))
	if local != "" {
		noErr(p.t, os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(local), 0o644))
	}
}

func (p *pushFixture) push(remote string, opts PushOptions) int {
	p.t.Helper()
	p.out.Reset()
	opts.Config, opts.Limits = p.config, Limits{MemoryMax: "2G", CPUQuota: "200%"}
	return Push(p.repo, remote, opts, &p.out, &p.errOutBuf)
}

func (p *pushFixture) pushed() []string {
	p.t.Helper()
	out := gitOut(p.t, p.ckpt, "", "for-each-ref", "--format=%(refname)", "refs/entire/")
	return strings.Fields(out)
}

func (p *pushFixture) held() string {
	return filepath.Join(p.repo, ".git", "lighthouse", "secret-scan", "held.txt")
}

const (
	refOne  = "refs/entire/checkpoints/01/one"
	refTwo  = "refs/entire/checkpoints/02/two"
	refLeak = "refs/entire/checkpoints/03/leak"
)

func (p *pushFixture) threeRefs() map[string]string {
	return p.checkpoints(map[string]map[string]string{
		refOne:  {"0/full.jsonl": "one\n"},
		refTwo:  {"0/full.jsonl": "two\n"},
		refLeak: {"0/full.jsonl": "x " + fakeSecret + "\n"},
	})
}

func TestPushSendsCleanAndHoldsDirty(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()

	rc := p.push("ckpt", PushOptions{})

	eq(t, 1, rc, p.errOutBuf.String())
	eq(t, []string{refOne, refTwo}, p.pushed())
	held, err := os.ReadFile(p.held())
	noErr(t, err)
	eq(t, refLeak+"\n  0/full.jsonl  fake-rule  REDACTED\n", string(held))
	eq(t, "held  "+refLeak+"\n2 pushed, 1 held — see "+p.held()+"\n", p.out.String())
}

func TestPushRefusesANonCheckpointRemote(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()
	gitOut(t, p.repo, "", "remote", "add", "origin", p.ckpt)

	rc := p.push("origin", PushOptions{})

	eq(t, 2, rc)
	contains(t, p.errOutBuf.String(), "secret-scan: BLOCKED — 'origin' ("+p.ckpt+") is not the checkpoint remote; checkpoints hold agent transcripts and go only there")
	eq(t, 0, len(p.pushed()))
}

func TestPushRefusesWhileEntirePushesByItself(t *testing.T) {
	p := newPushFixture(t)
	p.entire(`{"enabled": true, "strategy_options": {}}`, "")
	p.threeRefs()

	rc := p.push("ckpt", PushOptions{})

	eq(t, 2, rc)
	contains(t, p.errOutBuf.String(), "secret-scan: BLOCKED — entire may push checkpoints by itself here, past the scan; set strategy_options.push_sessions to false in .entire/settings.json")
	eq(t, 0, len(p.pushed()))
}

func TestPushByURLOfTheCheckpointRepo(t *testing.T) {
	p := newPushFixture(t)
	p.entire(`{"strategy_options": {"push_sessions": false, "checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}}}`, "")
	gitOut(t, p.repo, "", "config", "url."+p.ckpt+".insteadOf", "https://github.com/Acme/ckpt.git")
	p.threeRefs()

	rc := p.push("https://github.com/Acme/ckpt.git", PushOptions{})

	eq(t, 1, rc, p.errOutBuf.String())
	eq(t, []string{refOne, refTwo}, p.pushed())
}

func TestPushChunksTheRefspecsAndCountsAFailedChunk(t *testing.T) {
	p := newPushFixture(t)
	trees := map[string]map[string]string{}
	for i := 0; i < 6; i++ {
		trees[fmt.Sprintf("refs/entire/checkpoints/%02d/c", i)] = map[string]string{"0/full.jsonl": fmt.Sprintf("body %d\n", i)}
	}
	p.checkpoints(trees)
	// The remote refuses the first ref of chunk 2; the second ref alone would be accepted.
	hook := filepath.Join(p.ckpt, "hooks", "update")
	noErr(t, os.WriteFile(hook, []byte("#!/bin/sh\n[ \"$1\" != refs/entire/checkpoints/02/c ]\n"), 0o755))

	rc := p.push("ckpt", PushOptions{Chunk: 2})

	eq(t, 2, rc)
	eq(t, []string{"refs/entire/checkpoints/00/c", "refs/entire/checkpoints/01/c"}, p.pushed())
	contains(t, p.errOutBuf.String(), "secret-scan: git push exited 1 at refs 3-4; 2 of 6 clean refs pushed, the rest not")
}

func TestPushReportRSSReportsPeakPerBatch(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()

	p.push("ckpt", PushOptions{ReportRSS: true})

	contains(t, p.errOutBuf.String(), "secret-scan: batch 1/1 peak RSS ")
}

func TestPushWithoutReportRSSSaysNoRSS(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()

	p.push("ckpt", PushOptions{})

	isFalse(t, strings.Contains(p.errOutBuf.String(), "peak RSS"))
}

func TestPushFullIgnoresTheLedger(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()
	ledger, err := LedgerPath(p.repo, p.config)
	noErr(t, err)
	// The leaking blob is in the ledger: a ledger run trusts it, --full scans it again.
	noErr(t, AddToLedger(ledger, []string{blobID("x " + fakeSecret + "\n")}))

	eq(t, 0, p.push("ckpt", PushOptions{}), p.errOutBuf.String())
	eq(t, []string{refOne, refTwo, refLeak}, p.pushed())
	gitOut(t, p.ckpt, "", "update-ref", "-d", refLeak)

	rc := p.push("ckpt", PushOptions{Full: true})

	eq(t, 1, rc, p.errOutBuf.String())
	eq(t, []string{refOne, refTwo}, p.pushed())
}

func TestPushCapHoldsAndContinues(t *testing.T) {
	p := newPushFixture(t)
	t.Setenv(BatchBytesEnv, "5000")
	trees := map[string]map[string]string{
		"refs/entire/checkpoints/ok/clean": {"0/full.jsonl": strings.Repeat("c", 6000) + "\n"},
	}
	for i := 0; i < FindingsCap+1; i++ {
		trees[fmt.Sprintf("refs/entire/checkpoints/cap/%03d", i)] = map[string]string{"0/full.jsonl": fmt.Sprintf("%s %d\n", fakeSecret, i)}
	}
	p.checkpoints(trees)

	rc := p.push("ckpt", PushOptions{})

	eq(t, 1, rc, p.errOutBuf.String())
	contains(t, p.errOutBuf.String(), "secret-scan: batch 2/2, ") // the batch after the cap still ran
	eq(t, []string{"refs/entire/checkpoints/ok/clean"}, p.pushed())
	capLine := fmt.Sprintf("secret-scan: %d findings for rule fake-rule in one batch; an allowlist is probably missing for this pattern (sample: REDACTED)", FindingsCap+1)
	isTrue(t, strings.HasPrefix(p.out.String(), capLine+"\n"), p.out.String())
	isTrue(t, strings.HasSuffix(p.out.String(), "\n1 pushed, 201 held — see "+p.held()+"\n"), p.out.String())
	held, err := os.ReadFile(p.held())
	noErr(t, err)
	isTrue(t, strings.HasPrefix(string(held), capLine+"\n"))
	eq(t, 201, strings.Count(string(held), "\nrefs/entire/checkpoints/cap/"))
}

func TestPushAbortedPushesNothingAndRemovesHeld(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()
	noErr(t, os.MkdirAll(filepath.Dir(p.held()), 0o755))
	noErr(t, os.WriteFile(p.held(), []byte("refs/entire/checkpoints/old/held\n"), 0o644))
	t.Setenv("FAKE_GITLEAKS_KILL_AT", "1")

	rc := p.push("ckpt", PushOptions{})

	eq(t, 2, rc)
	contains(t, p.errOutBuf.String(), "ABORTED at memory ceiling (2G) in batch 1")
	contains(t, p.errOutBuf.String(), "secret-scan: nothing pushed; removed "+p.held()+"; a rerun goes on from the ledger and writes it again")
	_, err := os.Stat(p.held())
	isTrue(t, os.IsNotExist(err))
	eq(t, 0, len(p.pushed()))
}

func TestPushCouldNotCheckPushesNothing(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()
	t.Setenv("FAKE_GITLEAKS_REPORT", "none")

	rc := p.push("ckpt", PushOptions{})

	eq(t, 2, rc)
	contains(t, p.errOutBuf.String(), "secret-scan: COULD NOT CHECK — ")
	eq(t, 0, len(p.pushed()))
}

func TestPushWithoutTheConfigIsCouldNotCheck(t *testing.T) {
	p := newPushFixture(t)
	p.threeRefs()

	rc := Push(p.repo, "ckpt", PushOptions{Config: filepath.Join(p.repo, "nope.toml")}, &p.out, &p.errOutBuf)

	eq(t, 2, rc)
	contains(t, p.errOutBuf.String(), "secret-scan: COULD NOT CHECK — cannot find "+filepath.Join(p.repo, "nope.toml")+"; scans must use the same config as CI")
}

func TestPushSendsTheScannedCommit(t *testing.T) {
	p := newPushFixture(t)
	refs := p.threeRefs()
	other := gitOut(t, p.repo, "", "commit-tree", refs[refTwo]+"^{tree}", "-m", "moved after the scan")
	// The ref moves while gitleaks runs: what lands is the commit the scan read.
	t.Setenv("FAKE_GITLEAKS_HOOK", "git -C '"+p.repo+"' update-ref "+refOne+" "+strings.TrimSpace(other))

	rc := p.push("ckpt", PushOptions{})

	eq(t, 1, rc, p.errOutBuf.String())
	eq(t, refs[refOne], strings.TrimSpace(gitOut(t, p.ckpt, "", "rev-parse", refOne)))
}
