package checkpointscan

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// FindingsCap is the most findings of one rule in one batch: more, and an allowlist is
	// probably missing.
	FindingsCap = 200
	// BlobPrefix is written before every blob in its file. `gitleaks dir` skips a file whose
	// first bytes are the magic number of a binary type (zip, gzip, ELF, PDF, RTF, SQLite,
	// OLE, ...; measured on 8.30.1), also when git sees text and `gitleaks git` would scan it.
	// No such type starts with a newline. Matches stay the same; only line numbers shift by
	// one, and this package reports none.
	BlobPrefix = "\n"
)

// Finding is one gitleaks hit in one blob.
type Finding struct{ Blob, Rule, Match string }

// ScanResult is what a scan found and how it ended.
type ScanResult struct {
	Findings []Finding
	// Overloaded holds the cap lines: one per rule with more than FindingsCap findings in one
	// batch.
	Overloaded []string
	// Aborted is the line that ended the scan early (ceiling, COULD NOT CHECK); empty means it
	// completed. A caller must read it: findings alone are not a complete scan.
	Aborted string
}

// ScanOptions are the knobs of ScanBlobs.
type ScanOptions struct {
	// Ledger is the file the clean blobs go into after each batch; empty writes none.
	Ledger string
	// StopOnCap ends the scan after a batch with a rule over FindingsCap, independent of
	// ReportRSS (`push` keeps going: it holds the dirty refs and continues).
	StopOnCap bool
	// ReportRSS also says gitleaks' peak RSS after every batch (the measurement).
	ReportRSS bool
	Config    string
	Limits    Limits
	// Stderr gets the per-batch lines; nil means os.Stderr.
	Stderr io.Writer
}

// EnvBytes is a number of bytes from the environment; unset or empty gives def.
func EnvBytes(name string, def int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return def, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, CouldNotCheck{Msg: fmt.Sprintf("%s=%s is not a number of bytes", name, value)}
	}
	return n, nil
}

// MissingBlobs is every blob reachable from refs (ref -> sha) but not from what
// RemoteExclusion leaves out, with its size and the places it sits in.
func MissingBlobs(repo string, refs map[string]string, remote string) (map[string]Blob, error) {
	exclusion, err := RemoteExclusion(repo, remote)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(refs))
	for ref := range refs {
		names = append(names, ref)
	}
	sort.Strings(names)
	places := map[string]map[Place]struct{}{}
	for _, ref := range names {
		args := []string{"rev-list", "--objects", refs[ref]}
		if len(exclusion) > 0 {
			args = append(append(args, "--not"), exclusion...)
		}
		out, err := git(repo, nil, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range lines(out) {
			oid, path, named := strings.Cut(line, " ")
			if !named { // trees and blobs carry a path; commits do not
				continue
			}
			if places[oid] == nil {
				places[oid] = map[Place]struct{}{}
			}
			places[oid][Place{Ref: ref, Path: path}] = struct{}{}
		}
	}
	blobs := map[string]Blob{}
	if len(places) == 0 {
		return blobs, nil
	}
	oids := make([]string, 0, len(places))
	for oid := range places {
		oids = append(oids, oid)
	}
	sort.Strings(oids)
	stdin := strings.Join(oids, "\n") + "\n"
	check, err := git(repo, &stdin, "cat-file", "--batch-check")
	if err != nil {
		return nil, err
	}
	for _, line := range lines(check) {
		parts := strings.Split(line, " ")
		if len(parts) != 3 {
			return nil, CouldNotCheck{Msg: "git cat-file: " + line}
		}
		if parts[1] != "blob" {
			continue
		}
		size, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, CouldNotCheck{Msg: "git cat-file: " + line}
		}
		blobs[parts[0]] = Blob{Size: size, Places: places[parts[0]]}
	}
	return blobs, nil
}

// UnscannedBlobs is what the ledger does not hold yet; with full it is everything: --full
// ignores the ledger and scans again. The ledger filter belongs to the caller, here once.
func UnscannedBlobs(blobs map[string]Blob, ledger string, full bool) (map[string]Blob, error) {
	if full {
		return blobs, nil
	}
	clean, err := LoadLedger(ledger)
	if err != nil {
		return nil, err
	}
	out := map[string]Blob{}
	for oid, blob := range blobs {
		if _, ok := clean[oid]; !ok {
			out[oid] = blob
		}
	}
	return out, nil
}

// scanError is a batch that could not be scanned: COULD NOT CHECK. Output is gitleaks' own,
// shown before the abort line.
type scanError struct{ msg, output string }

func (e *scanError) Error() string { return e.msg }

// writeBlobs writes each blob of batch to dest/<blob id>, BlobPrefix first, through one
// `git cat-file --batch`, streamed: no more than a buffer of a blob is in memory at once. It
// returns the bytes written, prefixes included.
func writeBlobs(repo string, batch []string, dest string) (written int, err error) {
	cmd := exec.Command("git", "-C", repo, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(batch, "\n") + "\n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	defer func() {
		_ = stdout.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	r := bufio.NewReaderSize(stdout, 1<<20)
	for _, oid := range batch {
		line, _ := r.ReadString('\n')
		header := strings.Fields(line)
		if len(header) != 3 || header[0] != oid || header[1] != "blob" {
			got := strings.Join(header, " ")
			if got == "" {
				got = "nothing"
			}
			return written, &scanError{msg: fmt.Sprintf("git cat-file --batch gave %s for %s", got, oid)}
		}
		left, err := strconv.Atoi(header[2])
		if err != nil {
			return written, &scanError{msg: fmt.Sprintf("git cat-file --batch gave %s for %s", line, oid)}
		}
		written += len(BlobPrefix) + left
		if err := writeBlob(r, filepath.Join(dest, oid), left); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				return written, &scanError{msg: fmt.Sprintf("git cat-file --batch ended inside %s", oid)}
			}
			return written, err
		}
		_, _ = r.ReadByte() // the newline after the content
	}
	return written, nil
}

func writeBlob(r io.Reader, path string, size int) error {
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := out.WriteString(BlobPrefix); err != nil {
		_ = out.Close()
		return err
	}
	if _, err := io.CopyN(out, r, int64(size)); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

var (
	// gitleaks' own count of what it read, and the debug line of a file it skipped.
	scannedRE  = regexp.MustCompile(`\bscanned ~(\d+) bytes\b`)
	skippingRE = regexp.MustCompile(`\bskipping\b`)
)

// skipped is the blobs of batch named in gitleaks' `skipping` lines.
func skipped(output string, batch []string) []string {
	var named []string
	for _, line := range strings.Split(output, "\n") {
		if skippingRE.MatchString(line) {
			named = append(named, line)
		}
	}
	var out []string
	for _, oid := range batch {
		for _, line := range named {
			if strings.Contains(line, oid) {
				out = append(out, oid)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

type reportItem struct{ rule, file, match string }

// readReport is the gitleaks JSON report; ok is false when it is missing or not a list of
// findings with a string RuleID, File and Match.
func readReport(path string) (items []reportItem, ok bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var data []map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil { // null is not a list
		return nil, false
	}
	items = []reportItem{}
	for _, item := range data {
		rule, ok1 := item["RuleID"].(string)
		file, ok2 := item["File"].(string)
		match, ok3 := item["Match"].(string)
		if !ok1 || !ok2 || !ok3 {
			return nil, false
		}
		items = append(items, reportItem{rule, file, match})
	}
	return items, true
}

func oneLine(s string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(s)
}

// scanBatch scans one batch in its own temp dir <root>: blobs in <root>/blobs/, the report at
// <root>/report.json, which does not exist before gitleaks runs. It returns the run and its
// findings; ceiling is true when the memory ceiling killed gitleaks.
//
// gitleaks runs in <root>/blobs/ on `.`, so the path it matches its path allowlists against
// is the bare blob id. On the absolute path, a TMPDIR under node_modules/ (the default config
// allowlists it, and more such directories) made every blob clean. The cwd also keeps a
// .gitleaksignore of the repo out of the scan.
//
// gitleaks says how many bytes it scanned. Less than was written means it skipped a file (a
// binary type at an offset BlobPrefix does not move, a path allowlist, a size limit): the
// batch could not be checked, and none of it is clean.
func scanBatch(repo string, batch []string, opts ScanOptions, i int) (run Run, found []Finding, err error) {
	root, mkErr := os.MkdirTemp("", "secret-scan-")
	if mkErr != nil {
		return Run{}, nil, &scanError{msg: fmt.Sprintf("cannot write the blobs of batch %d: %v", i, mkErr)}
	}
	defer os.RemoveAll(root) //nolint:errcheck // a temp dir
	blobdir := filepath.Join(root, "blobs")
	if mkErr := os.Mkdir(blobdir, 0o755); mkErr != nil {
		return Run{}, nil, &scanError{msg: fmt.Sprintf("cannot write the blobs of batch %d: %v", i, mkErr)}
	}
	report := filepath.Join(root, "report.json")
	written, werr := writeBlobs(repo, batch, blobdir)
	if werr != nil {
		var se *scanError
		if errors.As(werr, &se) {
			return Run{}, nil, werr
		}
		return Run{}, nil, &scanError{msg: fmt.Sprintf("cannot write the blobs of batch %d: %v", i, werr)}
	}
	config, absErr := filepath.Abs(opts.Config)
	if absErr != nil {
		return Run{}, nil, &scanError{msg: fmt.Sprintf("cannot resolve %s: %v", opts.Config, absErr)}
	}
	argv := []string{
		"gitleaks", "dir", ".", "--config", config, "--no-banner",
		"--redact", "-f", "json", "-r", report, "--exit-code", "1",
		// debug: gitleaks names a file it skips only at this level.
		"--no-color", "--log-level", "debug",
	}
	run, rerr := runBounded(argv, opts.Limits, blobdir, opts.stderr())
	if rerr != nil {
		return Run{}, nil, &scanError{msg: rerr.Error()}
	}
	if run.CeilingHit {
		return run, nil, nil
	}
	if run.ReturnCode != 0 && run.ReturnCode != 1 {
		return run, nil, &scanError{msg: fmt.Sprintf("gitleaks exited %d in batch %d", run.ReturnCode, i), output: run.Output}
	}
	items, ok := readReport(report)
	if !ok {
		return run, nil, &scanError{msg: fmt.Sprintf("gitleaks exited %d in batch %d without a readable report", run.ReturnCode, i), output: run.Output}
	}
	// gitleaks also exits 1 on an error; rc 1 is findings only with findings to show.
	if run.ReturnCode == 1 && len(items) == 0 {
		return run, nil, &scanError{msg: fmt.Sprintf("gitleaks exited 1 in batch %d with no findings in its report", i), output: run.Output}
	}
	counts := scannedRE.FindAllStringSubmatch(run.Output, -1)
	if len(counts) != 1 {
		return run, nil, &scanError{msg: fmt.Sprintf("gitleaks did not say how many bytes it scanned in batch %d", i)}
	}
	if counts[0][1] != strconv.Itoa(written) {
		named := strings.Join(skipped(run.Output, batch), ", ")
		if named == "" {
			named = "a blob it did not name"
		}
		return run, nil, &scanError{msg: fmt.Sprintf("gitleaks scanned %s of %d bytes in batch %d; it skipped %s", counts[0][1], written, i, named)}
	}
	members := make(map[string]struct{}, len(batch))
	for _, oid := range batch {
		members[oid] = struct{}{}
	}
	for _, item := range items {
		// `<blob id>`, relative to the cwd; an archive member adds `!<inner path>`, which may
		// hold a `/`: split it off first.
		inner, _, _ := strings.Cut(item.file, "!")
		oid := filepath.Base(filepath.FromSlash(inner))
		if _, ok := members[oid]; !ok {
			return run, nil, &scanError{msg: fmt.Sprintf("the report of batch %d names %s, not a blob of the batch", i, item.file)}
		}
		found = append(found, Finding{Blob: oid, Rule: item.rule, Match: oneLine(item.match)})
	}
	return run, found, nil
}

func (o ScanOptions) stderr() io.Writer {
	if o.Stderr == nil {
		return os.Stderr
	}
	return o.Stderr
}

func mb(size int) string { return fmt.Sprintf("%.0f", float64(size)/(1<<20)) }

// ScanBlobs scans blobs in batches and reports each batch on opts.Stderr as it ends.
//
// After every completed batch its blobs without a finding go into opts.Ledger. A ceiling hit
// or a batch that could not be scanned ends the scan with Aborted set, and nothing of that
// batch goes into the ledger; so does a ledger that cannot be written, whatever the error. A
// rule over FindingsCap in one batch ends the scan after that batch only with StopOnCap.
//
// The error is only ever a CouldNotCheck, for what is wrong before the first batch (a
// batch size from the environment that is no number). A caller must treat both an error and a
// non-empty Aborted as exit 2, never as findings.
func ScanBlobs(repo string, blobs map[string]Blob, opts ScanOptions) (ScanResult, error) {
	batchBytes, err := EnvBytes(BatchBytesEnv, BatchBytes)
	if err != nil {
		return ScanResult{}, err
	}
	stderr := opts.stderr()
	result := ScanResult{}
	abort := func(line string) (ScanResult, error) {
		_, _ = fmt.Fprintln(stderr, line)
		result.Aborted = line
		return result, nil
	}
	plan := Batches(blobs, batchBytes)
	for n, batch := range plan {
		i := n + 1
		label := fmt.Sprintf("batch %d/%d", i, len(plan))
		run, found, err := scanBatch(repo, batch, opts, i)
		if err != nil {
			var se *scanError
			if !errors.As(err, &se) {
				se = &scanError{msg: err.Error()}
			}
			if strings.TrimSpace(se.output) != "" {
				_, _ = fmt.Fprintln(stderr, strings.TrimRight(se.output, "\n"))
			}
			return abort(fmt.Sprintf("%s: COULD NOT CHECK — %s", Prefix, se.msg))
		}
		if run.CeilingHit {
			return abort(fmt.Sprintf("%s: ABORTED at memory ceiling (%s) in batch %d; raise host.scan_memory_max or lower the batch size", Prefix, opts.Limits.MemoryMax, i))
		}
		dirty := map[string]struct{}{}
		for _, f := range found {
			dirty[f.Blob] = struct{}{}
		}
		if opts.Ledger != "" {
			var clean []string
			for _, oid := range batch {
				if _, bad := dirty[oid]; !bad {
					clean = append(clean, oid)
				}
			}
			if err := AddToLedger(opts.Ledger, clean); err != nil {
				var c CouldNotCheck
				if errors.As(err, &c) {
					return abort(fmt.Sprintf("%s: COULD NOT CHECK — %s", Prefix, c.Msg))
				}
				return abort(fmt.Sprintf("%s: COULD NOT CHECK — cannot write the ledger %s: %v", Prefix, opts.Ledger, err))
			}
		}
		result.Findings = append(result.Findings, found...)
		size := 0
		for _, oid := range batch {
			size += blobs[oid].Size
		}
		_, _ = fmt.Fprintf(stderr, "%s: %s, %s MB, %d findings\n", Prefix, label, mb(size), len(found))
		if opts.ReportRSS {
			rss := "unknown"
			if run.PeakRSSKB != nil {
				rss = fmt.Sprintf("%.0f", float64(*run.PeakRSSKB)/1024)
			}
			_, _ = fmt.Fprintf(stderr, "%s: %s peak RSS %s MB\n", Prefix, label, rss)
		}
		if capped := reportCap(found, stderr, &result); capped && opts.StopOnCap {
			break
		}
	}
	return result, nil
}

// reportCap says each rule over FindingsCap in found, once, and records the line.
func reportCap(found []Finding, stderr io.Writer, result *ScanResult) bool {
	counts := map[string]int{}
	sample := map[string]string{}
	for _, f := range found {
		if counts[f.Rule] == 0 {
			sample[f.Rule] = f.Match
		}
		counts[f.Rule]++
	}
	rules := make([]string, 0, len(counts))
	for rule := range counts {
		rules = append(rules, rule)
	}
	sort.Strings(rules)
	capped := false
	for _, rule := range rules {
		if counts[rule] > FindingsCap {
			line := fmt.Sprintf("%s: %d findings for rule %s in one batch; an allowlist is probably missing for this pattern (sample: %s)", Prefix, counts[rule], rule, sample[rule])
			_, _ = fmt.Fprintln(stderr, line)
			result.Overloaded = append(result.Overloaded, line)
			capped = true
		}
	}
	return capped
}
