package scheduled

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

// Record is what the row of a scheduled run keeps of its start (runs.verdict), as JSON: the
// gate's verdict, the limit of the agent phase, and the git state of the workplace before the
// agent ran (GitState, "" until the workplace is prepared).
type Record struct {
	Verdict    Verdict       `json:"verdict"`
	MaxRuntime time.Duration `json:"max_runtime"`
	GitState   string        `json:"git_state,omitempty"`
}

// Encode is the JSON of r.
func (r Record) Encode() string {
	b, err := json.Marshal(r)
	if err != nil {
		// Strings, ints and a duration cannot fail to marshal.
		panic("scheduled: encode record: " + err.Error())
	}
	return string(b)
}

// DecodeRecord reads a Record from its JSON.
func DecodeRecord(s string) (Record, error) {
	var r Record
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return Record{}, fmt.Errorf("scheduled: decode record: %w", err)
	}
	return r, nil
}

// gitStateFiles are the files under the git dir (gitDir) and the common dir (commonDir) whose
// content decides what git runs in a workplace: config can name filters, a credential helper,
// an ssh command, an upload-pack or another origin; info/attributes can attach a filter to every
// path.
func gitStateFiles(gitDir, commonDir string) []string {
	return []string{
		filepath.Join(commonDir, "config"),
		filepath.Join(gitDir, "config.worktree"),
		filepath.Join(commonDir, "info", "attributes"),
	}
}

// StartState returns the commit HEAD of the workplace dir is on and its GitState. The tick
// records both at the start of a scheduled run, before the agent runs; the check after the run
// compares them. Git runs with the same hardening as Postcheck.
func StartState(ctx context.Context, run workplace.Runner, dir string) (sha, state string, err error) {
	g := gitIn{run: run, dir: dir}
	if sha, err = g.ok(ctx, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return "", "", err
	}
	if !shaPattern.MatchString(sha) {
		return "", "", fmt.Errorf("scheduled: HEAD %q is no commit sha", sha)
	}
	if state, err = GitState(ctx, run, dir); err != nil {
		return "", "", err
	}
	return sha, state, nil
}

// GitState is a digest of the git state of the workplace dir: its git dir and common dir, and the
// content of the files that decide what git runs there (gitStateFiles; a missing file counts as
// missing). Of the two config files it hashes the entries (configEntries) without the branch.*
// ones: tracking branches of other runs land in the common config of a shared repository, and
// none of those keys makes git run anything. It runs only `git rev-parse` for the two dirs and
// `git config --file`, which run no filter, hook or helper, so it is safe in a workplace whose
// config the agent may have changed. Before the check after a run, the tick compares it with the
// state recorded at the start: a change means the agent wrote config that the commit, the push or
// a checkout would run.
func GitState(ctx context.Context, run workplace.Runner, dir string) (string, error) {
	g := gitIn{run: run, dir: dir}
	gitDir, err := g.ok(ctx, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	commonDir, err := g.ok(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "git-dir\x00%s\x00common-dir\x00%s\x00", gitDir, commonDir)
	for _, p := range gitStateFiles(gitDir, commonDir) {
		b, err := os.ReadFile(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			_, _ = fmt.Fprintf(h, "%s\x00absent\x00", p)
		case err != nil:
			// The base name and the cause only: the error becomes a run reason, which never
			// carries a raw path.
			cause := err
			var pe *fs.PathError
			if errors.As(err, &pe) {
				cause = pe.Err
			}
			return "", fmt.Errorf("scheduled: git state: read %s: %w", filepath.Base(p), cause)
		case filepath.Base(p) != "attributes":
			entries, err := g.configEntries(ctx, p, b)
			if err != nil {
				return "", err
			}
			_, _ = fmt.Fprintf(h, "%s\x00%d\x00", p, len(entries))
			_, _ = h.Write([]byte(entries)) // a hash.Hash never fails a Write
		default:
			_, _ = fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
			_, _ = h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// configEntries is the config file p (content b) as git reads it, one key\nvalue\x00 per
// entry in file order, without the entries of a branch.* section and without following
// includes (an include stays an entry of its own). A file git cannot read is its raw content,
// marked as such: it then differs from any readable file.
func (g gitIn) configEntries(ctx context.Context, p string, b []byte) (string, error) {
	out, code, err := g.raw(ctx, "config", "--file", p, "--no-includes", "--list", "-z")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "unreadable\x00" + string(b), nil
	}
	var kept strings.Builder
	for _, e := range strings.SplitAfter(out, "\x00") {
		if e != "" && !strings.HasPrefix(e, "branch.") {
			kept.WriteString(e)
		}
	}
	return kept.String(), nil
}
