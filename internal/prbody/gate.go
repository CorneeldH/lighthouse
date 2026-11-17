package prbody

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// blockedMessage is BLOCKED_MESSAGE of pr_body_gate.py.
const blockedMessage = "pr-create-gate: BLOCKED — %s\n\n" +
	"Write the body with /lighthouse-write-pr <nr> and pass it with --body-file <path>.\n" +
	"Forms: `Closes #N` + `### Evidence #N` (one row per AC), `Refs #N — <reason>`," +
	" `No issue: <reason>`.\n"

// GitHubAPIURLEnv names the variable that replaces the GitHub API root of the AC count. It is a
// test seam: the end-to-end tests point it at a fake API on localhost. Only the GitHub forge of
// the default Issue reads it, and only a loopback host: the token goes along with the request.
const GitHubAPIURLEnv = "LIGHTHOUSE_GITHUB_API_URL"

// Env is what Main takes from outside the process: the forge, gh, and the GH_REPO that gh itself
// reads.
type Env struct {
	// GH runs `gh argv…` until ctx ends. err is an exec error when gh did not start (wrapping
	// exec.ErrNotFound when it is missing), non-nil when ctx ended it, and nil for any exit,
	// whose code is then code. The default Issue runs `gh auth token` with it when no GitHub
	// token is set; nil is RunGH.
	GH func(ctx context.Context, argv ...string) (stdout string, code int, err error)
	// Issue reads the body of issue nr of repo until ctx ends. A missing token is a
	// *forge.TokenError. nil is the forge of repo (forge.Open), its token from the environment
	// (forge.Token), for GitHub with `gh auth token` through GH as the last resort.
	Issue func(ctx context.Context, repo Repo, nr int) (body string, err error)
	// ProcessGHRepo is GH_REPO of the hook's own environment ("" when not set).
	ProcessGHRepo string
}

// DefaultEnv is the production Env: the forge API, the real gh, and GH_REPO of this process.
func DefaultEnv() Env {
	return Env{GH: RunGH, Issue: forgeIssue(RunGH, os.Getenv), ProcessGHRepo: os.Getenv("GH_REPO")}
}

// issue is e.Issue, or the forge read with e.GH as its gh.
func (e Env) issue() func(ctx context.Context, repo Repo, nr int) (string, error) {
	if e.Issue != nil {
		return e.Issue
	}
	gh := e.GH
	if gh == nil {
		gh = RunGH
	}
	return forgeIssue(gh, os.Getenv)
}

// forgeIssue reads an issue body through the forge of the repo, with the token that forge.Token
// finds through getenv; for GitHub `gh auth token` (through gh) is the last resort.
func forgeIssue(gh func(context.Context, ...string) (string, int, error), getenv func(string) string) func(context.Context, Repo, int) (string, error) {
	return func(ctx context.Context, repo Repo, nr int) (string, error) {
		token, err := forge.Token(repo.Kind, repo.TokenEnv, getenv)
		var te *forge.TokenError
		if errors.As(err, &te) && te.Kind == "github" {
			if stdout, code, ghErr := gh(ctx, "auth", "token"); ghErr == nil && code == 0 {
				if t := strings.TrimSpace(stdout); t != "" {
					token, err = t, nil
				}
			}
		}
		if err != nil {
			return "", err
		}
		cfg := forge.Config{Kind: repo.Kind, Repo: repo.Slug, Host: repo.Host, Token: token}
		if repo.Kind == "" || repo.Kind == "github" {
			cfg.BaseURL = loopbackAPIURL(getenv(GitHubAPIURLEnv))
		}
		f, err := forge.Open(cfg)
		if err != nil {
			return "", err
		}
		issue, err := f.Issue(ctx, nr)
		return issue.Body, err
	}
}

// loopbackAPIURL is v when it is an http(s) URL on a loopback host, "" otherwise: a value that
// points elsewhere is ignored and the real GitHub API is used, so the token never leaves for it.
func loopbackAPIURL(v string) string {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	switch u.Hostname() {
	case "127.0.0.1", "::1", "localhost":
		return v
	}
	return ""
}

// oneLine is s on one line: the line breaks become "; ".
func oneLine(s string) string { return strings.ReplaceAll(strings.TrimSpace(s), "\n", "; ") }

// RunGH runs the real gh in its own process group, which goes as a whole when ctx ends.
func RunGH(ctx context.Context, argv ...string) (string, int, error) {
	stdout, _, code, err := run(ctx, nil, "gh", argv...)
	return stdout, code, err
}

// fetchACCount is (count, true, "") or (0, false, cause) for an ordinary failure: a missing
// token, a forge error or timeout, an issue without an AC heading. err is the failure that blocks:
// a headings table that cannot be read.
func fetchACCount(ctx context.Context, env Env, repo Repo, nr int) (int, bool, string, error) {
	timeout := budget(ctx, acTimeout)
	if timeout <= 0 {
		return 0, false, "deadline reached", nil
	}
	if repo.Slug == "" {
		return 0, false, "no forge.repo in lighthouse.toml", nil
	}
	issueCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	body, err := env.issue()(issueCtx, repo, nr)
	var te *forge.TokenError
	switch {
	case err == nil:
	case issueCtx.Err() != nil:
		if timeout < acTimeout {
			return 0, false, "deadline reached", nil
		}
		return 0, false, "timeout after " + strconv.FormatFloat(acTimeout.Seconds(), 'g', -1, 64) + " s", nil
	case errors.As(err, &te):
		return 0, false, te.Error(), nil
	default:
		return 0, false, "forge error: " + oneLine(err.Error()), nil
	}
	count, ok, err := countACs(body)
	if err != nil {
		return 0, false, "", err
	}
	if !ok {
		return 0, false, "no acceptance criteria heading", nil
	}
	return count, true, "", nil
}

// forms names the forms of a body that passed, for the line on stdout. counts holds the AC count
// per Closes target that was fetched; nil is not counted.
func forms(body string, repo Repo, counts map[int]*int) string {
	var out []string
	for _, nr := range ClosesTargets(body, repo) {
		rows, _ := EvidenceRows(body, nr)
		acText := "AC not counted, "
		if count := counts[nr]; count != nil {
			acText = strconv.Itoa(*count) + " AC, "
		}
		out = append(out, fmt.Sprintf("Closes #%d (%s%d rows)", nr, acText, len(rows)))
	}
	for _, nr := range RefsIssues(body) {
		out = append(out, "Refs #"+strconv.Itoa(nr))
	}
	if NoIssue(body) {
		out = append(out, "No issue")
	}
	return strings.Join(out, ", ")
}

// maximalSubpart is the length of the invalid UTF-8 sequence at the start of b that Python's
// decoder replaces with one U+FFFD: the lead byte plus the continuation bytes that still fit it.
func maximalSubpart(b []byte) int {
	lo, hi := byte(0x80), byte(0xBF)
	var need int
	switch c := b[0]; {
	case c >= 0xC2 && c <= 0xDF:
		need = 1
	case c == 0xE0:
		need, lo = 2, 0xA0
	case c == 0xED:
		need, hi = 2, 0x9F
	case c >= 0xE1 && c <= 0xEF:
		need = 2
	case c == 0xF0:
		need, lo = 3, 0x90
	case c == 0xF4:
		need, hi = 3, 0x8F
	case c >= 0xF1 && c <= 0xF3:
		need = 3
	default:
		return 1
	}
	n := 1
	for ; n <= need && n < len(b); n++ {
		if b[n] < lo || b[n] > hi {
			break
		}
		lo, hi = 0x80, 0xBF
	}
	return n
}

// pyDecode is bytes.decode("utf-8", errors="replace") with universal newlines, as Path.read_text
// reads: one U+FFFD per maximal invalid subpart, `\r\n` and a lone `\r` become `\n`.
func pyDecode(b []byte) string {
	var s strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			size = maximalSubpart(b)
			s.WriteRune(utf8.RuneError)
		} else {
			s.Write(b[:size])
		}
		b = b[size:]
	}
	return strings.ReplaceAll(strings.ReplaceAll(s.String(), "\r\n", "\n"), "\r", "\n")
}

// strerror is the C strerror text of err, as Python's OSError.strerror gives it.
func strerror(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	text := errno.Error()
	r, size := utf8.DecodeRuneInString(text)
	return string(unicode.ToUpper(r)) + text[size:]
}

// readBodyFile is the body in path, or the blocker why it cannot be read.
func readBodyFile(path string) (string, string) {
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", "body file " + path + " does not exist"
	case err != nil:
		return "", "body file " + path + " cannot be read (" + strerror(err) + ")"
	}
	return pyDecode(raw), ""
}

// checkCreate is (blockers, lines for stdout) for one in-scope create.
func checkCreate(ctx context.Context, pc PrCreate, repo repoFunc, env Env) ([]string, []string, error) {
	if pc.Problem != "" {
		return []string{pc.Problem}, nil, nil
	}
	var body string
	if pc.Body != nil {
		body = *pc.Body
	}
	if pc.BodyFile != nil {
		var problem string
		if body, problem = readBodyFile(*pc.BodyFile); problem != "" {
			return []string{problem}, nil, nil
		}
	}
	own, err := repo()
	if err != nil {
		return nil, nil, err
	}
	counts := map[int]*int{}
	var fatal error
	acCount := func(nr int) (int, bool, string) {
		if fatal != nil {
			return 0, false, ""
		}
		count, ok, cause, err := fetchACCount(ctx, env, own, nr)
		if err != nil {
			fatal = err
			return 0, false, ""
		}
		counts[nr] = nil
		if ok {
			counts[nr] = &count
		}
		return count, ok, cause
	}
	verdict := CheckBody(body, own, acCount)
	if fatal != nil {
		return nil, nil, fatal
	}
	if len(verdict.Blockers) > 0 {
		return verdict.Blockers, nil, nil
	}
	return nil, append([]string{"pr-create-gate: body checked — " + forms(body, own, counts)}, verdict.Notices...), nil
}

// tokenEnv is forge.token_env of cfg, "" when not set.
func tokenEnv(cfg *config.Config) string {
	f, _ := cfg.Raw["forge"].(map[string]any)
	v, _ := f["token_env"].(string)
	return v
}

// lazyRepo reads forge.repo on the first call only, as Python reads the config only where it
// needs the repo: a command without a `gh pr create` passes with a broken lighthouse.toml.
func lazyRepo() repoFunc {
	var repo Repo
	var err error
	loaded := false
	return func() (Repo, error) {
		if !loaded {
			loaded = true
			var path string
			if path, err = config.Path(); err == nil {
				var cfg *config.Config
				if cfg, err = config.Load(path); err == nil {
					repo = Repo{Kind: cfg.Kind, Slug: cfg.Repo, Host: cfg.Host, TokenEnv: tokenEnv(cfg)}
				}
			}
		}
		return repo, err
	}
}

// Main checks every pull or merge request create (FindPrCreates) in the command on stdin, run from cwd ("" for unknown), with a
// deadline of DeadlineS on ctx. It returns 0 (stdout: the pass lines), 2 (stderr: the blocker),
// 3 (an open quote) or 1 (a configuration error, one line on stderr). The project is
// CLAUDE_PROJECT_DIR, else the repo root.
func Main(ctx context.Context, cwd string, stdin io.Reader, stdout, stderr io.Writer, env Env) int {
	ctx, cancel := context.WithTimeout(ctx, gateDeadline)
	defer cancel()
	fail := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "pr-create-gate: %v\n", err)
		return 1
	}
	project := os.Getenv("CLAUDE_PROJECT_DIR")
	if project == "" {
		root, err := config.RepoRoot()
		if err != nil {
			return fail(err)
		}
		project = root
	}
	command, err := io.ReadAll(stdin)
	if err != nil {
		return fail(err)
	}
	creates, err := FindPrCreates(string(command), cwd)
	if errors.Is(err, gitinvocations.ErrUnparseable) {
		return 3
	}
	if err != nil {
		return fail(err)
	}

	repo := lazyRepo()
	var blockers, lines []string
	for _, pc := range creates {
		// Only gh reads GH_REPO; glab and `lighthouse forge` do not.
		if pc.Tool == "gh" && pc.Repo == nil && pc.RepoProblem == "" && env.ProcessGHRepo != "" {
			value := env.ProcessGHRepo
			pc.Repo = &value
		}
		decision, reason, err := inScope(ctx, pc, project, repo)
		if err != nil {
			return fail(err)
		}
		switch decision {
		case decisionSkip:
			lines = append(lines, "pr-create-gate: "+reason)
		case decisionBlock:
			blockers = append(blockers, reason)
		default:
			b, l, err := checkCreate(ctx, pc, repo, env)
			if err != nil {
				return fail(err)
			}
			blockers = append(blockers, b...)
			lines = append(lines, l...)
		}
	}
	if len(blockers) > 0 {
		_, _ = fmt.Fprintf(stderr, blockedMessage, strings.Join(blockers, "; "))
		return 2
	}
	for _, line := range lines {
		_, _ = fmt.Fprintln(stdout, line)
	}
	return 0
}
