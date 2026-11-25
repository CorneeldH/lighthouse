package checkpointscan

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitpath"
)

// PushChunk is the most refspecs one `git push` of Push gets: a bound on the command line.
const PushChunk = 500

// PushOptions are the knobs of Push.
type PushOptions struct {
	// Full ignores the ledger (the ledger is still written).
	Full bool
	// ReportRSS says gitleaks' peak RSS per batch.
	ReportRSS bool
	// Config is the gitleaks config; the hook's, so push and hook share one ledger.
	Config string
	Limits Limits
	// Chunk is the refspecs per `git push`; 0 means PushChunk.
	Chunk int
}

// resolveRemote is (name, url) the way git hands them to the pre-push hook: a configured
// remote by name and its push URL, anything else as a URL, which is then the name too.
func resolveRemote(repo, arg string) (name, url string, err error) {
	names, err := git(repo, nil, "remote")
	if err != nil {
		return "", "", err
	}
	if slices.Contains(strings.Fields(names), arg) {
		out, err := git(repo, nil, "remote", "get-url", "--push", arg)
		if err != nil {
			return "", "", err
		}
		return arg, strings.TrimSpace(out), nil
	}
	return arg, arg, nil
}

// refsOf reads `<ref> <sha>` lines (shaFirst: `<sha>\t<ref>`) into ref -> sha.
func refsOf(out string, shaFirst bool) map[string]string {
	refs := map[string]string{}
	sep := " "
	if shaFirst {
		sep = "\t"
	}
	for _, line := range lines(out) {
		a, b, _ := strings.Cut(line, sep)
		if shaFirst {
			refs[b] = a
		} else {
			refs[a] = b
		}
	}
	return refs
}

// writeHeld writes the cap lines, then one block per held ref: the ref, then
// `  <path>  <rule> <match>` per finding. Rewritten every run, through a temp file and a rename.
func writeHeld(path string, overloaded []string, held map[string]map[string]struct{}) error {
	var parts []string
	if len(overloaded) > 0 {
		parts = append(parts, strings.Join(overloaded, "\n")+"\n")
	}
	refs := make([]string, 0, len(held))
	for ref := range held {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		items := make([]string, 0, len(held[ref]))
		for item := range held[ref] {
			items = append(items, item)
		}
		sort.Strings(items)
		parts = append(parts, ref+"\n"+strings.Join(items, "\n")+"\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(strings.Join(parts, "\n"))
	cerr := tmp.Close()
	if err := firstErr(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// Push scans the checkpoints remote misses and pushes the clean refs through the pre-push
// hook; the dirty ones are held and written to held.txt next to the ledger. out is the
// overview, errOut gets the progress and `git push`' own output. Exit 0 all pushed, 1 something
// held, 2 could not check (every error, also a failed push of a chunk).
func Push(repo, remote string, opts PushOptions, out, errOut io.Writer) int {
	code, err := push(repo, remote, opts, out, errOut)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "%s: COULD NOT CHECK — %v\n", Prefix, err)
		return 2
	}
	return code
}

func push(repo, remote string, opts PushOptions, out, errOut io.Writer) (int, error) {
	top, err := git(repo, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return 0, err
	}
	remote, url, err := resolveRemote(repo, remote)
	if err != nil {
		return 0, err
	}
	settings, err := ReadEntireSettings(gitpath.Native(top))
	if err != nil {
		return 0, err
	}
	if settings.SelfPush {
		_, _ = fmt.Fprintf(errOut, "%s: BLOCKED — entire may push checkpoints by itself here, past the scan; set strategy_options.push_sessions to false in .entire/settings.json\n", Prefix)
		return 2, nil
	}
	if !IsCheckpointRemote(settings, remote, url) {
		_, _ = fmt.Fprintf(errOut, "%s: BLOCKED — '%s' (%s) is not the checkpoint remote; checkpoints hold agent transcripts and go only there\n", Prefix, remote, url)
		return 2, nil
	}
	if info, err := os.Stat(opts.Config); err != nil || !info.Mode().IsRegular() {
		return 0, CouldNotCheck{Msg: fmt.Sprintf("cannot find %s; scans must use the same config as CI", opts.Config)}
	}
	ledger, err := LedgerPath(repo, opts.Config)
	if err != nil {
		return 0, err
	}
	localOut, err := git(repo, nil, "for-each-ref", "--format=%(refname) %(objectname)", "refs/entire/")
	if err != nil {
		return 0, err
	}
	// The URL the rule checked (the push URL), not the remote's fetch URL: with a separate
	// pushurl those are two repos.
	thereOut, err := git(repo, nil, "ls-remote", url, "refs/entire/*")
	if err != nil {
		return 0, err
	}
	local, there := refsOf(localOut, false), refsOf(thereOut, true)
	missing := map[string]string{}
	for ref, sha := range local {
		if there[ref] != sha {
			missing[ref] = sha
		}
	}
	blobs, err := MissingBlobs(repo, missing, remote)
	if err != nil {
		return 0, err
	}
	todo, err := UnscannedBlobs(blobs, ledger, opts.Full)
	if err != nil {
		return 0, err
	}
	volume := 0
	for _, b := range todo {
		volume += b.Size
	}
	_, _ = fmt.Fprintf(errOut, "%s: %d refs to push, %d blobs (%s MB) to scan\n", Prefix, len(missing), len(todo), mb(volume))
	// Push never stops at the cap: it holds the dirty refs and continues.
	result, err := ScanBlobs(repo, todo, ScanOptions{
		Ledger: ledger, StopOnCap: false, ReportRSS: opts.ReportRSS, Config: opts.Config, Limits: opts.Limits, Stderr: errOut,
	})
	if err != nil {
		return 0, err
	}
	heldPath := filepath.Join(filepath.Dir(ledger), "held.txt")
	if result.Aborted != "" {
		// A held.txt of an earlier run no longer says what is held.
		_ = os.Remove(heldPath)
		_, _ = fmt.Fprintf(errOut, "%s: nothing pushed; removed %s; a rerun goes on from the ledger and writes it again\n", Prefix, heldPath)
		return 2, nil
	}
	held := map[string]map[string]struct{}{}
	for _, f := range result.Findings {
		for place := range blobs[f.Blob].Places {
			if held[place.Ref] == nil {
				held[place.Ref] = map[string]struct{}{}
			}
			held[place.Ref][fmt.Sprintf("  %s  %s  %s", place.Path, f.Rule, f.Match)] = struct{}{}
		}
	}
	if err := writeHeld(heldPath, result.Overloaded, held); err != nil {
		return 0, err
	}
	var clean []string
	for ref := range missing {
		if _, bad := held[ref]; !bad {
			clean = append(clean, ref)
		}
	}
	sort.Strings(clean)
	chunk := opts.Chunk
	if chunk <= 0 {
		chunk = PushChunk
	}
	pushed := 0
	for start := 0; start < len(clean); start += chunk {
		part := clean[start:min(start+chunk, len(clean))]
		// A plain push, through the pre-push hook. Its stdout goes to errOut: out is the
		// overview. `<sha>:<ref>`: the commit the scan read, not what the ref holds by now.
		// Atomic: a refused chunk lands not at all, so the count holds.
		args := []string{"-C", repo, "push", "--atomic", remote}
		for _, ref := range part {
			args = append(args, missing[ref]+":"+ref)
		}
		cmd := exec.Command("git", args...)
		cmd.Stdout, cmd.Stderr = errOut, errOut
		rc := 0
		if err := cmd.Run(); err != nil {
			ee, ok := err.(*exec.ExitError) //nolint:errorlint // only a plain exit has a code
			if !ok {
				return 0, CouldNotCheck{Msg: fmt.Sprintf("git did not start: %v", err)}
			}
			rc = ee.ExitCode()
		}
		if rc != 0 {
			_, _ = fmt.Fprintf(errOut, "%s: git push exited %d at refs %d-%d; %d of %d clean refs pushed, the rest not\n", Prefix, rc, start+1, start+len(part), pushed, len(clean))
			return 2, nil
		}
		pushed += len(part)
	}
	for _, line := range result.Overloaded {
		_, _ = fmt.Fprintln(out, line)
	}
	heldRefs := make([]string, 0, len(held))
	for ref := range held {
		heldRefs = append(heldRefs, ref)
	}
	sort.Strings(heldRefs)
	for _, ref := range heldRefs {
		_, _ = fmt.Fprintf(out, "held  %s\n", ref)
	}
	_, _ = fmt.Fprintf(out, "%d pushed, %d held — see %s\n", pushed, len(held), heldPath)
	if len(held) > 0 {
		return 1, nil
	}
	return 0, nil
}
