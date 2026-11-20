//go:build unix

package checkpointscan

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	refA = "refs/entire/checkpoints/aa/one"
	refB = "refs/entire/checkpoints/bb/two"
)

func TestMissingBlobsHasSizesAndEveryPlace(t *testing.T) {
	f := newFixture(t, true)
	body := "same body\n"
	refs := f.checkpoints(map[string]map[string]string{
		refA: {"0/full.jsonl": body},
		refB: {"1/full.jsonl": body, "1/metadata.json": "{}\n"},
	})

	got := f.missing(refs)

	eq(t, map[string]Blob{
		blobID(body):   {Size: len(body), Places: map[Place]struct{}{{refA, "0/full.jsonl"}: {}, {refB, "1/full.jsonl"}: {}}},
		blobID("{}\n"): {Size: 3, Places: map[Place]struct{}{{refB, "1/metadata.json"}: {}}},
	}, got)
}

func TestMissingBlobsLeavesOutWhatTheRemoteHas(t *testing.T) {
	f := newFixture(t, true)
	old := f.single(refA, "old\n")
	// As if a fetch from `ckpt` had brought it: a remote-tracking ref.
	gitOut(t, f.repo, "", "update-ref", "refs/remotes/ckpt/entire/checkpoints/aa/one", old[refA])
	fresh := f.single(refB, "new\n")

	got := f.missing(map[string]string{refA: old[refA], refB: fresh[refB]})

	eq(t, []string{blobID("new\n")}, mapKeys(got))
}

func TestMissingBlobsUnknownShaIsCouldNotCheck(t *testing.T) {
	f := newFixture(t, true)

	_, err := MissingBlobs(f.repo, map[string]string{refA: strings.Repeat("a", 40)}, "ckpt")

	_ = cnc(t, err)
}

func mapKeys(m map[string]Blob) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return sorted(out...)
}

func TestScanBlobsCleanPasses(t *testing.T) {
	f := newFixture(t, true)
	refs := f.checkpoints(map[string]map[string]string{refA: {"0/full.jsonl": "clean one\n", "0/metadata.json": "{}\n"}})
	opts := f.opts()

	res := f.scan(refs, opts)

	eq(t, ScanResult{}, res)
	eq(t, sorted(blobID("clean one\n"), blobID("{}\n")), f.ledger(opts))
	eq(t, "secret-scan: batch 1/1, 0 MB, 0 findings\n", f.stderr.String())
}

func TestScanBlobsFindsSecretAndKeepsItOutOfTheLedger(t *testing.T) {
	f := newFixture(t, true)
	dirty := `{"text": "` + fakeSecret + `"}` + "\n"
	refs := f.checkpoints(map[string]map[string]string{
		refA: {"0/full.jsonl": "clean one\n", "0/metadata.json": "{}\n"},
		refB: {"0/full.jsonl": dirty, "0/metadata.json": "{}\n"},
	})
	opts := f.opts()

	res := f.scan(refs, opts)

	eq(t, []Finding{{Blob: blobID(dirty), Rule: "fake-rule", Match: "REDACTED"}}, res.Findings)
	eq(t, "", res.Aborted)
	eq(t, 1, f.calls()) // one batch
	eq(t, sorted(blobID("clean one\n"), blobID("{}\n")), f.ledger(opts))
}

func TestScanBlobsSharedBlobIsScannedOnce(t *testing.T) {
	f := newFixture(t, true)
	body := fakeSecret + "\n"
	refs := f.checkpoints(map[string]map[string]string{refA: {"0/full.jsonl": body}, refB: {"1/full.jsonl": body}})

	res := f.scan(refs, f.opts())

	eq(t, []string{blobID(body)}, f.logged())
	eq(t, 1, len(res.Findings)) // one hit; the places tell the refs
}

func TestScanBlobsArchiveMemberMapsToItsBlob(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv("FAKE_GITLEAKS_MEMBER", "inner/dir/file.txt")
	body := fakeSecret + "\n"
	opts := f.opts()

	res := f.scan(f.single(refA, body), opts)

	eq(t, []Finding{{Blob: blobID(body), Rule: "fake-rule", Match: "REDACTED"}}, res.Findings)
	eq(t, "", res.Aborted)
	eq(t, []string{}, f.ledger(opts))
}

func TestScanBlobsMatchIsOneLine(t *testing.T) {
	if got := oneLine("a\r\nb"); got != `a\r\nb` {
		t.Errorf("got %q", got)
	}
}

func threeBatches(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	t.Setenv(BatchBytesEnv, "1500")
	trees := map[string]map[string]string{}
	for i := range 3 {
		trees[fmt.Sprintf("refs/entire/checkpoints/0%d/ck%d", i, i)] = map[string]string{"0/full.jsonl": strings.Repeat(string(rune('A'+i)), 1000)}
	}
	return f.checkpoints(trees)
}

func TestScanBlobsKillMidRunKeepsEarlierBatches(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv("FAKE_GITLEAKS_KILL_AT", "2")
	refs := threeBatches(t, f)
	opts := f.opts()

	res := f.scan(refs, opts)

	want := "secret-scan: ABORTED at memory ceiling (2G) in batch 2; raise host.scan_memory_max or lower the batch size"
	eq(t, want, res.Aborted)
	contains(t, f.stderr.String(), "secret-scan: batch 1/3, ")
	contains(t, f.stderr.String(), want)
	eq(t, 2, f.calls()) // batch 3 never ran
	eq(t, 1, len(f.ledger(opts)))
}

func TestScanBlobsUnreadableReportIsNotClean(t *testing.T) {
	for _, tc := range []struct{ report, rc string }{{"none", "0"}, {"none", "1"}, {"garbage", "0"}, {"garbage", "1"}} {
		t.Run(tc.report+tc.rc, func(t *testing.T) {
			f := newFixture(t, true)
			t.Setenv("FAKE_GITLEAKS_REPORT", tc.report)
			t.Setenv("FAKE_GITLEAKS_RC", tc.rc)
			opts := f.opts()

			res := f.scan(f.single(refA, "x\n"), opts)

			isTrue(t, strings.HasPrefix(res.Aborted, "secret-scan: COULD NOT CHECK — "), res.Aborted)
			eq(t, 1, len(f.logged())) // gitleaks ran
			eq(t, []string{}, f.ledger(opts))
		})
	}
}

func TestScanBlobsRC1WithEmptyReportIsNotClean(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv("FAKE_GITLEAKS_RC", "1")
	opts := f.opts()

	res := f.scan(f.single(refA, "x\n"), opts)

	contains(t, res.Aborted, "COULD NOT CHECK — gitleaks exited 1 in batch 1 with no findings in its report")
	eq(t, []string{}, f.ledger(opts))
}

func TestScanBlobsOtherRCWithAReportIsNotClean(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv("FAKE_GITLEAKS_RC", "2")
	opts := f.opts()

	res := f.scan(f.single(refA, "x\n"), opts)

	eq(t, "secret-scan: COULD NOT CHECK — gitleaks exited 2 in batch 1", res.Aborted)
	eq(t, 1, len(f.logged()))
	eq(t, []string{}, f.ledger(opts))
}

func TestScanBlobsSkippedBytesAreNotClean(t *testing.T) {
	for _, said := range []string{"none", "short"} {
		t.Run(said, func(t *testing.T) {
			f := newFixture(t, true)
			t.Setenv("FAKE_GITLEAKS_SCANNED", said)
			opts := f.opts()

			res := f.scan(f.single(refA, "x\n"), opts)

			isTrue(t, strings.HasPrefix(res.Aborted, "secret-scan: COULD NOT CHECK — gitleaks "), res.Aborted)
			contains(t, res.Aborted, "in batch 1")
			eq(t, []string{}, f.ledger(opts))
		})
	}
}

func TestScanBlobsNamesTheSkippedBlob(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv("FAKE_GITLEAKS_SCANNED", "short")
	body := "x\n"

	res := f.scan(f.single(refA, body), f.opts())

	eq(t, "secret-scan: COULD NOT CHECK — gitleaks scanned 2 of 3 bytes in batch 1; it skipped a blob it did not name", res.Aborted)
}

func capRefs(f *fixture, n int) map[string]string {
	trees := map[string]map[string]string{}
	for i := range n {
		trees[fmt.Sprintf("refs/entire/checkpoints/cap/ck%03d", i)] = map[string]string{"0/full.jsonl": fmt.Sprintf("%s %d\n", fakeSecret, i)}
	}
	return f.checkpoints(trees)
}

const capLine = "secret-scan: 201 findings for rule fake-rule in one batch; an allowlist is probably missing for this pattern (sample: REDACTED)"

func TestScanBlobsOverTheCapSaysSo(t *testing.T) {
	f := newFixture(t, true)
	opts := f.opts()
	opts.StopOnCap = true

	res := f.scan(capRefs(f, 201), opts)

	eq(t, 201, len(res.Findings))
	eq(t, []string{capLine}, res.Overloaded)
	contains(t, f.stderr.String(), capLine+"\n")
}

func TestScanBlobsAtTheCapSaysNothing(t *testing.T) {
	f := newFixture(t, true)
	opts := f.opts()
	opts.StopOnCap = true

	res := f.scan(capRefs(f, 200), opts)

	eq(t, 200, len(res.Findings))
	empty(t, res.Overloaded)
	isFalse(t, strings.Contains(f.stderr.String(), "allowlist"))
}

// capThenClean is 201 dirty one-line blobs (batch 1, over the cap), then two clean 3000-byte
// blobs that each get a batch of their own with a 4000-byte batch.
func capThenClean(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	t.Setenv(BatchBytesEnv, "4000")
	trees := map[string]map[string]string{}
	for i := range 201 {
		trees[fmt.Sprintf("refs/entire/checkpoints/cap/ck%03d", i)] = map[string]string{"0/full.jsonl": fmt.Sprintf("%s %d\n", fakeSecret, i)}
	}
	for i := range 2 {
		trees[fmt.Sprintf("refs/entire/checkpoints/ok/ck%d", i)] = map[string]string{"0/full.jsonl": strings.Repeat(string(rune('A'+i)), 3000)}
	}
	return f.checkpoints(trees)
}

func TestScanStopsAtFindingsCapUnlessKeepGoing(t *testing.T) {
	f := newFixture(t, true)
	refs := capThenClean(t, f)
	opts := f.opts()
	opts.StopOnCap = true

	res := f.scan(refs, opts)

	eq(t, 1, len(res.Overloaded))
	eq(t, 1, f.calls()) // batches 2 and 3 never ran
	eq(t, "", res.Aborted)
}

func TestScanBlobsKeepGoingScansPastTheCap(t *testing.T) {
	f := newFixture(t, true)
	refs := capThenClean(t, f)
	opts := f.opts()

	res := f.scan(refs, opts)

	eq(t, 1, len(res.Overloaded))
	eq(t, 3, f.calls())
}

func TestPushStopsNotAtCapWithoutReportRSS(t *testing.T) {
	// `push` sets StopOnCap=false also when it reports no RSS: the two options are independent.
	f := newFixture(t, true)
	refs := capThenClean(t, f)
	opts := f.opts()
	opts.StopOnCap, opts.ReportRSS = false, false

	res := f.scan(refs, opts)

	eq(t, 1, len(res.Overloaded))
	eq(t, 3, f.calls())
	isFalse(t, strings.Contains(f.stderr.String(), "peak RSS"))
}

func TestScanBlobsReportRSSIsIndependentOfStopOnCap(t *testing.T) {
	f := newFixture(t, true)
	refs := threeBatches(t, f)
	opts := f.opts()
	opts.StopOnCap, opts.ReportRSS = true, true

	f.scan(refs, opts)

	for i := 1; i <= 3; i++ {
		isTrue(t, strings.Contains(f.stderr.String(), fmt.Sprintf("secret-scan: batch %d/3 peak RSS ", i)), f.stderr.String())
	}
	isTrue(t, peakRSSLine.MatchString(f.stderr.String()))
}

func TestScanBlobsWithoutLedgerWritesNone(t *testing.T) {
	f := newFixture(t, true)
	opts := f.opts()
	opts.Ledger = ""

	res := f.scan(f.single(refA, "x\n"), opts)

	eq(t, "", res.Aborted)
	_, err := os.Stat(filepath.Join(f.repo, ".git", "ledger.txt"))
	isTrue(t, os.IsNotExist(err))
}

func TestScanBlobsLedgerWriteFailureIsAbortedNotFindings(t *testing.T) {
	f := newFixture(t, true)
	opts := f.opts()
	// The ledger's directory is a file: MkdirAll fails with a raw error, no CouldNotCheck.
	blocker := filepath.Join(f.repo, "blocker")
	noErr(t, os.WriteFile(blocker, nil, 0o644))
	opts.Ledger = filepath.Join(blocker, "sub", "ledger.txt")

	res, err := ScanBlobs(f.repo, f.missing(f.single(refA, "x\n")), opts)

	noErr(t, err)
	isTrue(t, strings.HasPrefix(res.Aborted, "secret-scan: COULD NOT CHECK — cannot write the ledger "), res.Aborted)
	empty(t, res.Findings) // the batch did not count
}

func TestScanBlobsBrokenBatchSizeIsCouldNotCheck(t *testing.T) {
	f := newFixture(t, true)
	t.Setenv(BatchBytesEnv, "lots")

	_, err := ScanBlobs(f.repo, f.missing(f.single(refA, "x\n")), f.opts())

	contains(t, cnc(t, err).Msg, "LIGHTHOUSE_SCAN_BATCH_BYTES=lots is not a number of bytes")
}

func TestScanBlobsQuotaThatIsNoPercentageAbortsInsteadOfFindings(t *testing.T) {
	f := newFixture(t, true)
	opts := f.opts()
	opts.Limits.CPUQuota = "lots"

	res, err := ScanBlobs(f.repo, f.missing(f.single(refA, "x\n")), opts)

	noErr(t, err)
	isTrue(t, strings.HasPrefix(res.Aborted, "secret-scan: COULD NOT CHECK — "), res.Aborted)
	eq(t, 0, f.calls())
}

func TestScanBlobsRelativeConfigIsFound(t *testing.T) {
	f := newFixture(t, true)
	refs := f.single(refA, "x\n")
	blobs := f.missing(refs)
	t.Chdir(f.repo)
	opts := f.opts()
	rel, err := filepath.Rel(f.repo, f.config)
	noErr(t, err)
	opts.Config = rel

	res, err := ScanBlobs(f.repo, blobs, opts)

	noErr(t, err)
	eq(t, "", res.Aborted)
	eq(t, 1, len(f.ledger(opts)))
}

func TestScanBlobsBlobsAreWrittenWithTheNewlinePrefix(t *testing.T) {
	f := newFixture(t, true)
	// The fake counts bytes: a prefix less than `\n` per blob would make it say fewer.
	res := f.scan(f.single(refA, "abc"), f.opts())

	eq(t, "", res.Aborted)
	contains(t, f.stderr.String(), "batch 1/1")
}

func TestFullIgnoresTheLedger(t *testing.T) {
	f := newFixture(t, true)
	refs := f.single(refA, "x\n")
	opts := f.opts()
	f.scan(refs, opts)
	blobs := f.missing(refs)

	withLedger, err := UnscannedBlobs(blobs, opts.Ledger, false)
	noErr(t, err)
	full, err := UnscannedBlobs(blobs, opts.Ledger, true)
	noErr(t, err)

	eq(t, 0, len(withLedger))
	eq(t, blobs, full)
	// A scan of what --full leaves in scans it again.
	_ = os.Remove(f.log)
	_ = os.Remove(f.log + ".calls")
	f.scan(refs, opts)
	eq(t, []string{blobID("x\n")}, f.logged())
}

func TestUnscannedBlobsKeepsWhatTheLedgerLacks(t *testing.T) {
	f := newFixture(t, true)
	refs := f.checkpoints(map[string]map[string]string{refA: {"0/full.jsonl": "one\n", "0/metadata.json": "two\n"}})
	blobs := f.missing(refs)
	ledger := filepath.Join(t.TempDir(), "l.txt")
	noErr(t, AddToLedger(ledger, []string{blobID("one\n")}))

	got, err := UnscannedBlobs(blobs, ledger, false)

	noErr(t, err)
	eq(t, []string{blobID("two\n")}, mapKeys(got))
}

func TestEnvBytes(t *testing.T) {
	t.Setenv("CHECKPOINTSCAN_N", "")
	n, err := EnvBytes("CHECKPOINTSCAN_N", 7)
	noErr(t, err)
	eq(t, 7, n)
	t.Setenv("CHECKPOINTSCAN_N", "1000")
	n, err = EnvBytes("CHECKPOINTSCAN_N", 7)
	noErr(t, err)
	eq(t, 1000, n)
	t.Setenv("CHECKPOINTSCAN_N", "x")
	_, err = EnvBytes("CHECKPOINTSCAN_N", 7)
	contains(t, cnc(t, err).Msg, "CHECKPOINTSCAN_N=x is not a number of bytes")
}

func TestScanBlobsCallsGitleaksDirWithTheOriginalFlags(t *testing.T) {
	f := newFixture(t, true)

	f.scan(f.single(refA, "x\n"), f.opts())

	raw, err := os.ReadFile(f.log + ".args")
	noErr(t, err)
	got := strings.Fields(string(raw))
	// The report path is a temp path; the rest is literal.
	eq(t, []string{"dir", ".", "--config", f.config, "--no-banner", "--redact", "-f", "json", "-r"}, got[:9])
	eq(t, []string{"--exit-code", "1", "--no-color", "--log-level", "debug"}, got[10:])
	isTrue(t, strings.HasSuffix(got[9], "/report.json"), got[9])
}
