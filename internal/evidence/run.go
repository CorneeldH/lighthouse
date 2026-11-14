package evidence

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/deadline"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// The outcomes are keys of headings.toml; the text follows `[project] language`.
const (
	Holds             = "evidence_holds"
	Fails             = "evidence_fails"
	Invalid           = "evidence_invalid"
	Unmeasurable      = "evidence_unmeasurable"
	AfterPRUnmeasured = "evidence_after_pr_unmeasured"
)

// outcomes in the order outcome_of tries them.
var outcomes = []string{Holds, Fails, Invalid, Unmeasurable, AfterPRUnmeasured}

const (
	// MaxOutput is the cap in bytes on stdout and stderr together.
	MaxOutput = 4 << 20
	// GHProbeTimeoutS is the time limit in seconds of the `gh` reachability probe.
	GHProbeTimeoutS = 10
	// rowTimeout is the time limit of one row in Run.
	rowTimeout = 300 * time.Second
	readChunk  = 64 << 10
	// probeWaitDelay bounds the wait for the pipes of a probe that the deadline ended.
	probeWaitDelay = time.Second
)

// maxOutput is MaxOutput; a test sets a smaller cap.
var maxOutput = MaxOutput

// subEnv goes on top of the environment of every command this package runs (_SUB_ENV).
var subEnv = []string{"GIT_PAGER=cat", "PAGER=cat", "GH_PAGER=cat", "GH_PROMPT_DISABLED=1", "NO_COLOR=1"}

// expected is Expected, with the error of a headings table that cannot be read.
func expected(result string) (string, bool, error) {
	s := strings.ReplaceAll(stripBackticks(result), `\|`, "|")
	prefixes, err := config.Variants("evidence_after_pr")
	if err != nil {
		return "", false, err
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return s[len(prefix):], true, nil
		}
	}
	return s, false, nil
}

// Expected is the Result cell normalised: (expected fragment, was after PR). The prefix
// `na PR: ` counts in every language of headings.toml. A headings table that cannot be read
// gives (the cell, false).
func Expected(result string) (fragment string, afterPR bool) {
	fragment, afterPR, err := expected(result)
	if err != nil {
		return strings.ReplaceAll(stripBackticks(result), `\|`, "|"), false
	}
	return fragment, afterPR
}

// allPySpace is a string of Python whitespace only, the empty string included.
func allPySpace(s string) bool { return strings.TrimFunc(s, pyIsSpace) == "" }

// fullmatchPadded is re.fullmatch(r"\s*" + re.escape(v) + r"\s*", cell).
func fullmatchPadded(cell, v string) bool {
	for i, r := range cell {
		if strings.HasPrefix(cell[i:], v) && allPySpace(cell[i+len(v):]) {
			return true
		}
		if !pyIsSpace(r) {
			return false
		}
	}
	return false // a variant is never empty: headings.toml requires non-empty strings
}

// OutcomeOf is the outcome key a table cell names, in any language; "" when it names none, or
// when the headings table cannot be read. The whole cell decides: `klopt` is a prefix of
// `klopt niet`.
func OutcomeOf(cell string) string {
	for _, key := range outcomes {
		variants, err := config.Variants(key)
		if err != nil {
			return ""
		}
		for _, v := range variants {
			if fullmatchPadded(cell, v) {
				return key
			}
		}
	}
	return ""
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

// decodeReplace is bytes.decode("utf-8", errors="replace"): one U+FFFD per maximal invalid
// subpart, newlines unchanged.
func decodeReplace(b []byte) string {
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
	return s.String()
}

// pyRepr is Python's repr() of a str: `'` as the quote, unless s holds a `'` and no `"`.
func pyRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

// strerror is the C strerror text of errno, as Python's OSError.strerror gives it.
func strerror(errno syscall.Errno) string {
	text := errno.Error()
	r, size := utf8.DecodeRuneInString(text)
	return string(unicode.ToUpper(r)) + text[size:]
}

// pyOSError is str(OSError) with a filename: `[Errno N] <strerror>: '<filename>'`.
func pyOSError(err error, filename string) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return err.Error()
	}
	return fmt.Sprintf("[Errno %d] %s: %s", int(errno), strerror(errno), pyRepr(filename))
}

// seconds renders a time limit as Python prints the number of seconds: 300, 0.5.
func seconds(d time.Duration) string { return strconv.FormatFloat(d.Seconds(), 'f', -1, 64) }

// onPathNotExecutable is a file named name on PATH that exec could not run: Python's exec walks
// PATH and reports EACCES there, where Go's LookPath only says "not found".
func onPathNotExecutable(name string) bool {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			dir = "."
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return true
		}
	}
	return false
}

// startReason is the reason a command could not start, as _run_or_reason words it.
func startReason(err error, argv []string, cwd string) string {
	if errors.Is(err, exec.ErrNotFound) && onPathNotExecutable(argv[0]) {
		return "not executable: " + pyOSError(syscall.EACCES, argv[0])
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		// Python names the cwd when chdir failed, else argv[0].
		if _, statErr := os.Stat(cwd); statErr != nil {
			return "not found: " + cwd
		}
		return "not found: " + argv[0]
	}
	return "not executable: " + pyOSError(err, argv[0])
}

// capped is the outcome of runCapped: (code, output), or a reason the row is not measurable.
type capped struct {
	code   int
	output string
	reason string
}

// exitCode is Python's returncode: the exit code, or minus the signal that ended the process.
func exitCode(state *os.ProcessState) int {
	if ws, ok := state.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return -int(ws.Signal())
	}
	return state.ExitCode()
}

// runCapped runs argv (no shell) in cwd with a time limit and an output limit. Stdout and stderr
// go through one pipe, read in chunks of 64 KiB: `cat /dev/zero` must not fill up the host. On
// EOF, on the cap and at the deadline the whole process group goes (`uv run pytest` starts
// children), so a child in the background that holds the pipe cannot keep the row going.
func runCapped(argv []string, cwd string, timeout time.Duration) capped {
	r, w, err := os.Pipe()
	if err != nil {
		return capped{reason: "not executable: " + err.Error()}
	}
	defer func() { _ = r.Close() }()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), subEnv...)
	cmd.Stdout, cmd.Stderr = w, w
	deadline.InOwnGroup(cmd)
	err = cmd.Start()
	_ = w.Close()
	if err != nil {
		return capped{reason: startReason(err, argv, cwd)}
	}
	end := time.Now().Add(timeout)
	waited := false
	defer func() {
		deadline.KillGroup(cmd)
		if !waited {
			_ = cmd.Wait()
		}
	}()
	timedOut := capped{reason: "timeout after " + seconds(timeout) + " s"}

	chunks := make(chan []byte)
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		defer close(chunks)
		for {
			buf := make([]byte, readChunk)
			n, err := r.Read(buf)
			if n > 0 {
				select {
				case chunks <- buf[:n]:
				case <-stop:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var out []byte
read:
	for {
		if !time.Now().Before(end) {
			return timedOut
		}
		select {
		case chunk, ok := <-chunks:
			if !ok {
				break read
			}
			out = append(out, chunk...)
			if len(out) > maxOutput {
				return capped{reason: fmt.Sprintf("output larger than %d bytes", maxOutput)}
			}
		case <-timer.C:
			return timedOut
		}
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	select {
	case <-done:
		waited = true
	case <-timer.C:
		deadline.KillGroup(cmd)
		<-done
		waited = true
		return timedOut
	}
	return capped{code: exitCode(cmd.ProcessState), output: decodeReplace(out)}
}

// configTestDir reads `[test_gate] evidence_test_dir` from the repo's lighthouse.toml.
func configTestDir() (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	c, err := config.Load(path)
	if err != nil {
		return "", err
	}
	v, err := config.Lookup(c, "test_gate.evidence_test_dir")
	if err != nil {
		return "", err
	}
	return fmt.Sprint(v), nil
}

// rowArgv is the argv of a valid row, or the reason the row is invalid. One check for before and
// after the PR: a row that entry C calls invalid (outside the allowlist, or an empty expectation
// such as `na PR: `) must not pass as `after PR, not measured` under --before-pr.
func rowArgv(row prbody.Row) ([]string, string, error) {
	argv, reason := SplitCommand(row.Command)
	if reason != "" {
		return nil, reason, nil
	}
	reason, err := check(argv, configTestDir)
	if err != nil {
		return nil, "", err
	}
	if reason == "" {
		fragment, _, err := expected(row.Result)
		if err != nil {
			return nil, "", err
		}
		if fragment == "" {
			reason = "empty Result cell"
		}
	}
	if reason != "" {
		return nil, reason, nil
	}
	return argv, "", nil
}

// runRow is RunRow with the configuration error that Python raises out of run_row.
func runRow(row prbody.Row, cwd string, ghOK bool, timeout time.Duration) (string, string, error) {
	argv, reason, err := rowArgv(row)
	if err != nil {
		return "", "", err
	}
	if reason != "" {
		return Invalid, "Evidence outside the allowlist: " + reason, nil
	}
	fragment, _, err := expected(row.Result)
	if err != nil {
		return "", "", err
	}
	if argv[0] == "gh" && !ghOK {
		return Unmeasurable, "gh not reachable", nil
	}
	got := runCapped(argv, cwd, timeout)
	if got.reason != "" {
		return Unmeasurable, got.reason, nil
	}
	var parts []string
	if got.code != 0 {
		parts = append(parts, fmt.Sprintf("exit code %d", got.code))
	}
	if !strings.Contains(got.output, fragment) {
		parts = append(parts, "missing from output: "+pyRepr(fragment))
	}
	if len(parts) > 0 {
		return Fails, strings.Join(parts, "; "), nil
	}
	return Holds, "exit code 0, `" + fragment + "` found", nil
}

// RunRow runs one Evidence row: (outcome, detail). The outcome is Holds, Fails, Invalid (outside
// the allowlist, does not run) or Unmeasurable (binary missing, timeout, gh unreachable). A
// configuration that cannot be read, where Python raises, folds into Unmeasurable with
// `COULD NOT CHECK: <error>`: a caller that aggregates outcomes must not use RunRow, because that
// would turn a configuration error into exit 4 instead of exit 3. Run uses runRow, which returns
// the error.
func RunRow(row prbody.Row, cwd string, ghOK bool, timeout time.Duration) (outcome, detail string) {
	outcome, detail, err := runRow(row, cwd, ghOK, timeout)
	if err != nil {
		return Unmeasurable, "COULD NOT CHECK: " + err.Error()
	}
	return outcome, detail
}

// ghReachable is `gh api rate_limit` answering with exit 0 within GHProbeTimeoutS.
func ghReachable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), GHProbeTimeoutS*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "api", "rate_limit", "--jq", ".rate.remaining")
	cmd.Env = append(os.Environ(), subEnv...)
	deadline.InOwnGroup(cmd)
	cmd.Cancel = func() error { deadline.KillGroup(cmd); return nil }
	cmd.WaitDelay = probeWaitDelay
	return cmd.Run() == nil
}

// cell is text as one table cell: whitespace runs to one space, `|` escaped.
func cell(text string) string {
	return strings.ReplaceAll(strings.Join(strings.FieldsFunc(text, pyIsSpace), " "), "|", `\|`)
}

// runsGH is a row that counts for the gh probe: valid, a gh row, and not skipped.
func runsGH(row prbody.Row, beforePR bool) (bool, error) {
	argv, reason, err := rowArgv(row)
	if err != nil || reason != "" || argv[0] != "gh" {
		return false, err
	}
	_, afterPR, err := expected(row.Result)
	return !beforePR || !afterPR, err
}

// readBody is the body file as Python's text mode reads it: strict UTF-8, universal newlines.
func readBody(bodyFile string) (string, string) {
	raw, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", "body file unreadable (" + pyOSError(err, bodyFile) + ")"
	}
	if !utf8.Valid(raw) {
		// A deliberate deviation (ruling of the task 3 review, #279): Python's strict open() raises
		// UnicodeDecodeError here, outside its OSError branch, which is a traceback with rc 1. rc 1
		// means FAILS/INVALID, so Go says COULD NOT CHECK with rc 3. Do not "restore" Python's rc.
		return "", "body file unreadable (not valid UTF-8)"
	}
	return strings.ReplaceAll(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\r", "\n"), ""
}

// result is one row of the outcome table.
type result struct {
	row             prbody.Row
	outcome, detail string
}

// run is Run with the configuration error that Python's main turns into exit 3.
func run(issue int, bodyFile, cwd string, beforePR bool, stdout io.Writer) (int, error) {
	text, problem := readBody(bodyFile)
	if problem != "" {
		_, _ = fmt.Fprintln(stdout, "COULD NOT CHECK: "+problem)
		return 3, nil
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		_, _ = fmt.Fprintf(stdout, "COULD NOT CHECK: --cwd %s is not a directory\n", cwd)
		return 3, nil
	}
	path, err := config.Path()
	if err != nil {
		return 0, err
	}
	c, err := config.Load(path)
	if err != nil {
		return 0, err
	}
	heading := func(key string) (string, error) { return config.Heading(key, c.Language) }
	rows, reason := prbody.EvidenceRows(text, issue)
	if reason != "" {
		head, err := heading("evidence_closes_without_block")
		if err != nil {
			return 0, err
		}
		_, _ = fmt.Fprintln(stdout, head+" "+reason)
		return 2, nil
	}
	ghOK := false
	for _, row := range rows {
		ok, err := runsGH(row, beforePR)
		if err != nil {
			return 0, err
		}
		if ok {
			ghOK = ghReachable()
			break
		}
	}
	var results []result
	for _, row := range rows {
		afterPR := false
		if beforePR {
			if _, afterPR, err = expected(row.Result); err != nil {
				return 0, err
			}
		}
		if afterPR {
			_, reason, err := rowArgv(row)
			switch {
			case err != nil:
				return 0, err
			case reason != "":
				results = append(results, result{row, Invalid, "Evidence outside the allowlist: " + reason})
			default:
				results = append(results, result{row, AfterPRUnmeasured, "runs only after the PR"})
			}
			continue
		}
		outcome, detail, err := runRow(row, cwd, ghOK, rowTimeout)
		if err != nil {
			return 0, err
		}
		results = append(results, result{row, outcome, detail})
	}
	head, err := heading("evidence_table_head")
	if err != nil {
		return 0, err
	}
	lines := []string{head, "|---|---|---|"}
	seen := map[string]bool{}
	for _, r := range results {
		text, err := heading(r.outcome)
		if err != nil {
			return 0, err
		}
		lines = append(lines, "| "+cell(r.row.AC)+" | "+text+" | "+cell(r.detail)+" |")
		seen[r.outcome] = true
	}
	_, _ = io.WriteString(stdout, strings.Join(lines, "\n")+"\n")
	switch {
	case seen[Fails] || seen[Invalid]:
		return 1, nil
	case seen[Unmeasurable]:
		return 4, nil
	}
	return 0, nil
}

// couldNotCheck prints a configuration error as Python's main does: exit 3.
func couldNotCheck(stdout io.Writer, err error) int {
	_, _ = fmt.Fprintln(stdout, "COULD NOT CHECK: "+err.Error())
	return 3
}

// Run is `evidence run`: the Evidence rows of #issue in bodyFile, run in cwd, as a table on
// stdout. Exit 0 all hold, 1 a row fails or is invalid, 2 no Evidence block, 3 could not check,
// 4 a row is not measurable. With beforePR a row with an after-PR Result does not run.
func Run(issue int, bodyFile, cwd string, beforePR bool, stdout io.Writer) int {
	rc, err := run(issue, bodyFile, cwd, beforePR, stdout)
	if err != nil {
		return couldNotCheck(stdout, err)
	}
	return rc
}

// CheckCell is `evidence check`: exit 0 without output when the Command cell is allowed, exit 1
// with the reason on stdout when it is not, exit 3 when the configuration cannot be read.
func CheckCell(cell string, stdout io.Writer) int {
	argv, reason := SplitCommand(cell)
	if reason == "" {
		var err error
		if reason, err = check(argv, configTestDir); err != nil {
			return couldNotCheck(stdout, err)
		}
	}
	if reason == "" {
		return 0
	}
	_, _ = fmt.Fprintln(stdout, reason)
	return 1
}
