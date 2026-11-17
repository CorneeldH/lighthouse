// Package postartefact is `lighthouse post-artefact <nr> <file>`: it posts a committed spec or
// plan file as a version chain of issue comments.
//
// Steps (the port of post_artefact.py):
//
//  1. resolve the path, find the git toplevel, take the kind from the directory
//     (`paths.specs` or `paths.plans`)
//  2. the file must be committed and clean
//  3. the commit must be on GitHub (push if needed, except on `forge.default_branch`, a
//     detached HEAD or with an open PR; in a controller run never a range that touches
//     .github/workflows/, as workflow-push-gate)
//  4. read the existing comments and find the current complete version
//  5. same hash and a commit that is still reachable: unchanged, post nothing
//  6. split, render all parts, then post them in order
//  7. if part k fails, report how many parts did get posted
//
// Every failure is an `artefact:` line, the text of the Python script: [Error]. The comment
// text itself (header, JSON metadata, split) is internal/protocol; the text rules of Python that
// the script relied on are internal/pytext.
package postartefact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/procrun"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/pytext"
	"github.com/CorneeldH/lighthouse/internal/workflowguard"
)

// RunTimeout is the timeout of every `git` and `gh` call: 300 s, not 60, because `git push` runs
// the whole pre-push chain.
const RunTimeout = 300 * time.Second

// stderrTail is how much of a failed command's stderr an error line keeps: pre-push hooks print
// their `ok` lines first, the real error comes last.
const stderrTail = 300

// Env is what Run needs from outside: a way to start `git` and `gh`, and the configuration.
type Env struct {
	Run    procrun.Func
	Config func() (*config.Config, error)
	// Getenv reads LIGHTHOUSE_CONTROLLER_RUN; nil is os.Getenv.
	Getenv func(string) string
}

// Error is a failure of post-artefact; Error() is the full `artefact:` line.
type Error struct{ Line string }

func (e *Error) Error() string { return e.Line }

func fail(format string, args ...any) *Error {
	return &Error{Line: "artefact: " + fmt.Sprintf(format, args...)}
}

// configError is a configuration failure on one line, as `_config_error` words it.
func configError(err error) *Error {
	return fail("config: %s", strings.Join(pytext.Fields(err.Error()), " "))
}

// runError is a `git` or `gh` that did not return 0 or took too long; the message carries the
// tail of its stderr.
type runError struct{ msg string }

func (e *runError) Error() string { return e.msg }

type poster struct {
	ctx    context.Context
	run    procrun.Func
	repo   string
	cfg    *config.Config
	getenv func(string) string
}

// tail is the last n characters (code points) of s.
func tail(s string, n int) string {
	r := []rune(s)
	return string(r[max(0, len(r)-n):])
}

// do runs the command and returns its stdout as `subprocess.run(text=True).stdout.strip()` does.
func (p *poster) do(stdin *string, name string, args ...string) (string, *runError) {
	res, err := p.run(p.ctx, "", stdin, name, args...)
	var timeout *procrun.Timeout
	var missing *procrun.NotFound
	switch {
	case errors.As(err, &timeout):
		return "", &runError{fmt.Sprintf("timed out after %.0f s", RunTimeout.Seconds())}
	case errors.As(err, &missing):
		return "", &runError{missing.Error()}
	case err != nil:
		return "", &runError{err.Error()}
	case res.Code != 0:
		return "", &runError{tail(pytext.Strip(pytext.UniversalNewlines(res.Stderr)), stderrTail)}
	}
	return pytext.Strip(pytext.UniversalNewlines(res.Stdout)), nil
}

func (p *poster) git(cwd string, args ...string) (string, *runError) {
	return p.do(nil, "git", append([]string{"-C", cwd}, args...)...)
}

func (p *poster) gh(stdin *string, args ...string) (string, *runError) {
	return p.do(stdin, "gh", args...)
}

// commitKnown reports whether sha is on GitHub. Only a 404 or 422 from gh counts as no; every
// other error is returned.
func (p *poster) commitKnown(sha string) (bool, *runError) {
	_, err := p.gh(nil, "api", fmt.Sprintf("repos/%s/commits/%s", p.repo, sha), "--jq", ".sha")
	if err != nil {
		if strings.Contains(err.msg, "HTTP 404") || strings.Contains(err.msg, "HTTP 422") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (p *poster) commitKnownOrFail(sha string) (bool, *Error) {
	known, err := p.commitKnown(sha)
	if err != nil {
		return false, fail("gh failed: %s", err.msg)
	}
	return known, nil
}

func (p *poster) openPR(branch string) (bool, *runError) {
	out, err := p.gh(nil, "pr", "list", "--repo", p.repo, "--head", branch, "--state", "open",
		"--json", "number", "--jq", "length")
	return out != "0", err
}

// readComments reads every comment of the issue, oldest first.
func (p *poster) readComments(nr int) ([]protocol.Comment, *runError) {
	out, err := p.gh(nil, "api", "--paginate", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", p.repo, nr),
		"--jq", ".[] | {id, body, created_at, html_url}")
	if err != nil {
		return nil, err
	}
	var comments []protocol.Comment
	// One object per line, cut at \n only: jq leaves U+2028 and the other separators that
	// str.splitlines cuts at in a body, so splitting there would tear a comment in two.
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		c, ok := commentOf(line)
		if !ok {
			return nil, &runError{"unreadable comment: " + tail(line, 80)}
		}
		comments = append(comments, c)
	}
	return comments, nil
}

// commentOf reads one `{id, body, created_at, html_url}` line.
func commentOf(line string) (protocol.Comment, bool) {
	var raw struct {
		ID        json.Number `json:"id"`
		Body      *string     `json:"body"`
		CreatedAt *string     `json:"created_at"`
		HTMLURL   *string     `json:"html_url"`
	}
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return protocol.Comment{}, false
	}
	id, err := raw.ID.Int64()
	if err != nil {
		return protocol.Comment{}, false
	}
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return protocol.Comment{ID: id, Body: str(raw.Body), CreatedAt: str(raw.CreatedAt), URL: str(raw.HTMLURL)}, true
}

// postComment posts body to the issue and returns the url of the new comment. The payload is
// `json.dumps({"body": body})`: ASCII only, with Python's separators.
func (p *poster) postComment(nr int, body string) (string, *runError) {
	payload := `{"body": ` + pytext.JSONString(body, true) + `}`
	out, err := p.gh(&payload, "api", fmt.Sprintf("repos/%s/issues/%d/comments", p.repo, nr),
		"--input", "-", "--jq", "{id, html_url}")
	if err != nil {
		return "", err
	}
	var reply struct {
		URL *string `json:"html_url"`
	}
	if json.Unmarshal([]byte(out), &reply) != nil || reply.URL == nil {
		return "", &runError{"unreadable reply: " + tail(out, 80)}
	}
	return *reply.URL, nil
}

// setting is a key of lighthouse.toml, with its default, as text.
func (p *poster) setting(key string) (string, *Error) {
	v, err := config.Lookup(p.cfg, key)
	if err != nil {
		return "", configError(err)
	}
	return fmt.Sprint(v), nil
}

type kindDir struct{ prefix, kind string }

// dirs are the directories of a spec and a plan, as a prefix with a single trailing `/`. When
// both are one directory the plan wins, as in the dict of the script.
func (p *poster) dirs() ([]kindDir, *Error) {
	var out []kindDir
	for _, e := range []struct{ key, kind string }{{"paths.specs", "spec"}, {"paths.plans", "plan"}} {
		value, err := p.setting(e.key)
		if err != nil {
			return nil, err
		}
		prefix := strings.Trim(value, "/") + "/"
		replaced := false
		for i := range out {
			if out[i].prefix == prefix {
				out[i].kind, replaced = e.kind, true
			}
		}
		if !replaced {
			out = append(out, kindDir{prefix, e.kind})
		}
	}
	return out, nil
}

// resolve is `Path(arg).resolve()`: absolute, with symlinks resolved. A path that does not exist
// is only made absolute.
func resolve(arg string) string {
	path := arg
	if !filepath.IsAbs(path) {
		if cwd, err := os.Getwd(); err == nil {
			path = cwd + string(filepath.Separator) + path
		}
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// resolveFile is the git root, the path relative to it and the kind of pathArg.
func (p *poster) resolveFile(pathArg string) (top, file, kind string, err *Error) {
	path := resolve(pathArg)
	if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
		return "", "", "", fail("%s does not exist", pathArg)
	}
	top, runErr := p.git(filepath.Dir(path), "rev-parse", "--show-toplevel")
	if runErr != nil {
		return "", "", "", fail("%s is not in a git repository", pathArg)
	}
	rel, relErr := filepath.Rel(top, path)
	if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", "", fail("%s is not in a git repository", pathArg)
	}
	file = filepath.ToSlash(rel)
	dirs, err := p.dirs()
	if err != nil {
		return "", "", "", err
	}
	for _, d := range dirs {
		if strings.HasPrefix(file, d.prefix) {
			return top, file, d.kind, nil
		}
	}
	return "", "", "", fail("%s is no spec or plan", file)
}

// committedSHA is the last commit of file; an error if the file is modified or untracked.
func (p *poster) committedSHA(top, file string) (string, *Error) {
	status, err := p.git(top, "status", "--porcelain", "--", file)
	if err != nil {
		return "", fail("git failed: %s", err.msg)
	}
	if status != "" {
		return "", fail("%s not committed", file)
	}
	sha, err := p.git(top, "log", "-1", "--format=%H", "--", file)
	if err != nil {
		return "", fail("git failed: %s", err.msg)
	}
	if sha == "" {
		return "", fail("%s not committed", file)
	}
	return sha, nil
}

// ensureCommitOnGitHub pushes the commit when GitHub does not know it, unless that is never ours
// to do: a force-push to the default branch, a detached HEAD or a branch with an open PR.
func (p *poster) ensureCommitOnGitHub(top, sha string) *Error {
	known, err := p.commitKnownOrFail(sha)
	if err != nil || known {
		return err
	}
	branch, runErr := p.git(top, "rev-parse", "--abbrev-ref", "HEAD")
	if runErr != nil {
		return fail("git failed: %s", runErr.msg)
	}
	hasPR, runErr := p.openPR(branch)
	if runErr != nil {
		return fail("gh failed: %s", runErr.msg)
	}
	defaultBranch, err := p.setting("forge.default_branch")
	if err != nil {
		return err
	}
	if branch == defaultBranch || branch == "HEAD" || hasPR {
		return fail("commit %s not on GitHub", runeHead(sha, 7))
	}
	if err := p.refuseWorkflows(top, branch, defaultBranch); err != nil {
		return err
	}
	if _, runErr := p.git(top, "push", "-u", "--force-with-lease", "origin", "HEAD"); runErr != nil {
		return fail("push of %s failed: %s", branch, runErr.msg)
	}
	return nil
}

// refuseWorkflows refuses, in a controller run, the push of a range that touches
// .github/workflows/: the push below falls outside workflow-push-gate, so it says the same.
// Only in a controller run, like the hook: the refusal points at V2b and `next: push-workflows`,
// a route only the dispatcher runs; a person outside it pushes with their own credentials, and
// GitHub's check on those decides.
func (p *poster) refuseWorkflows(top, branch, defaultBranch string) *Error {
	if !park.ControllerRun(p.getenv) {
		return nil
	}
	git := func(args ...string) (string, error) {
		out, err := p.git(top, args...)
		if err != nil {
			return "", errors.New(err.msg)
		}
		return out, nil
	}
	head, err := git("rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return fail("%s", workflowguard.Uncheckable("HEAD cannot be read: "+err.Error()))
	}
	rng, paths, err := workflowguard.Check(git, head, branch, defaultBranch)
	if err != nil {
		return fail("%s", workflowguard.Uncheckable(err.Error()))
	}
	if len(paths) > 0 {
		return fail("push of %s refused — %s", branch, workflowguard.Refusal(rng, paths))
	}
	return nil
}

func runeHead(s string, n int) string {
	r := []rune(s)
	return string(r[:min(n, len(r))])
}

// unchangedMeta is the meta of the current version if it has the same content and a reachable commit.
func (p *poster) unchangedMeta(latest []protocol.Comment, hash string) (*protocol.ArtefactMeta, *Error) {
	if len(latest) == 0 {
		return nil, nil
	}
	meta, ok := protocol.ParseArtefactMeta(latest[0].Body)
	if !ok || meta.Hash != hash {
		return nil, nil
	}
	known, err := p.commitKnownOrFail(meta.Commit)
	if err != nil || !known {
		return nil, err
	}
	return &meta, nil
}

// postParts posts the parts in order and returns the urls. If part k fails: an error.
func (p *poster) postParts(nr int, bodies []string) ([]string, *Error) {
	urls := make([]string, 0, len(bodies))
	for k, body := range bodies {
		url, err := p.postComment(nr, body)
		if err != nil {
			return nil, fail("part %d/%d not posted (%d were): %s", k+1, len(bodies), k, err.msg)
		}
		urls = append(urls, url)
	}
	return urls, nil
}

// Run posts the file at path to issue nr and returns the success line. A failure is an *Error.
func Run(ctx context.Context, env Env, nr int, path string) (string, error) {
	line, err := run(ctx, env, nr, path)
	if err != nil {
		return "", err
	}
	return line, nil
}

func run(ctx context.Context, env Env, nr int, pathArg string) (string, *Error) {
	cfg, cfgErr := env.Config()
	if cfgErr != nil {
		return "", configError(cfgErr)
	}
	if cfg.Repo == "" {
		return "", fail("forge.repo is not set in %s", cfg.Path)
	}
	getenv := env.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	p := &poster{ctx: ctx, run: env.Run, repo: cfg.Repo, cfg: cfg, getenv: getenv}
	top, file, kind, err := p.resolveFile(pathArg)
	if err != nil {
		return "", err
	}
	sha, err := p.committedSHA(top, file)
	if err != nil {
		return "", err
	}
	if err := p.ensureCommitOnGitHub(top, sha); err != nil {
		return "", err
	}
	comments, runErr := p.readComments(nr)
	if runErr != nil {
		return "", fail("gh failed: %s", runErr.msg)
	}
	latest := protocol.CurrentArtefact(comments, kind, file)

	data, readErr := os.ReadFile(filepath.Join(top, filepath.FromSlash(file)))
	if readErr != nil {
		return "", fail("%s unreadable: %v", file, readErr)
	}
	hash := protocol.ArtefactHash(data)
	meta, err := p.unchangedMeta(latest, hash)
	if err != nil {
		return "", err
	}
	if meta != nil {
		return fmt.Sprintf("artefact: %s unchanged (version %d)", kind, meta.Version), nil
	}

	version := protocol.NextArtefactVersion(comments, kind, file)
	var replaces *string
	if len(latest) > 0 {
		replaces = &latest[0].URL
	}
	if !utf8.Valid(data) {
		return "", fail("%s is not valid UTF-8", file)
	}
	parts := protocol.SplitArtefact(string(data), protocol.DefaultSplitLimit)
	// Render everything first: on a refusal nothing is left half posted.
	bodies := make([]string, len(parts))
	for k, part := range parts {
		m := protocol.ArtefactMeta{Kind: kind, Version: version, Part: k + 1, Parts: len(parts),
			File: file, Commit: sha, Hash: hash, Replaces: replaces}
		body, renderErr := protocol.RenderArtefact(m, part, cfg.Language, cfg.Repo)
		var metaErr *protocol.MetadataError
		switch {
		case errors.As(renderErr, &metaErr):
			return "", fail("metadata unusable: %s", metaErr.Msg)
		case renderErr != nil:
			return "", configError(renderErr)
		}
		bodies[k] = body
	}
	urls, err := p.postParts(nr, bodies)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("artefact: %s version %d posted, %d parts → %s", kind, version, len(parts), urls[0]), nil
}
