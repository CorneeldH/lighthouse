package scheduled

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type secretPattern struct {
	kind string
	re   *regexp.Regexp
}

// secretPatterns starts with SECRET_PATTERN from the paia scripts, one alternative per entry so a
// finding can name its kind. Origin: CorneeldH/personal_ai_agent, scripts/codebase_map_refresh.py
// (line 142, commit c742152f1d267cf256fcc89749603a8a50dac3c4). Python's re.search on the
// alternation matches a line when any alternative matches it, which is what scanning these in
// turn does; every alternative is valid RE2 with the same meaning (`.` does not cross a newline).
// The entries after the paia set are additions of lighthouse's own: token shapes the paia set
// misses (an Anthropic key has hyphens after sk-, fine-grained and app tokens of GitHub).
var secretPatterns = []secretPattern{
	// paia SECRET_PATTERN
	{"secret key (sk-)", regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)},
	{"Stripe live key", regexp.MustCompile(`sk_live_[a-zA-Z0-9]+`)},
	{"Stripe test key", regexp.MustCompile(`sk_test_[a-zA-Z0-9]+`)},
	{"GitHub personal token", regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`)},
	{"GitHub OAuth token", regexp.MustCompile(`gho_[a-zA-Z0-9]{36}`)},
	{"GitLab token", regexp.MustCompile(`glpat-[a-zA-Z0-9_-]+`)},
	{"AWS access key", regexp.MustCompile(`AKIA[A-Z0-9]{16}`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9-]+`)},
	{"private key", regexp.MustCompile(`-----BEGIN.*PRIVATE KEY`)},
	{"JSON web token", regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.`)},
	// lighthouse additions
	{"Anthropic key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_-]{20,}`)},
	{"GitHub fine-grained token", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`)},
	{"GitHub token", regexp.MustCompile(`gh[osur]_[A-Za-z0-9]{36,}`)},
}

// secretKind is the kind of the first pattern that matches b, "" when none does.
func secretKind(b []byte) string {
	for _, p := range secretPatterns {
		if p.re.Match(b) {
			return p.kind
		}
	}
	return ""
}

// showPath renders a path for a finding or a run reason: quoted (so a newline cannot forge a log
// line), or withheld when the name itself matches a secret pattern.
func showPath(p string) string {
	if kind := secretKind([]byte(p)); kind != "" {
		return "(file name withheld: " + kind + ")"
	}
	return strconv.Quote(p)
}

// redact withholds every secret-pattern match in s and escapes line breaks, for text that goes
// into a run reason or a log.
func redact(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllLiteralString(s, "(withheld: "+p.kind+")")
	}
	return strings.NewReplacer("\n", `\n`, "\r", `\r`).Replace(s)
}

// scanBody returns the findings for one file: one for a name that matches a pattern (without the
// name), one per matching line of body as "<path>:<line>: <kind>".
func scanBody(name string, body []byte) []string {
	var findings []string
	if kind := secretKind([]byte(name)); kind != "" {
		findings = append(findings, "file name contains a "+kind)
	}
	for i, line := range bytes.Split(body, []byte("\n")) {
		if kind := secretKind(line); kind != "" {
			findings = append(findings, fmt.Sprintf("%s:%d: %s", showPath(name), i+1, kind))
		}
	}
	return findings
}

// ScanSecrets scans files (relative to dir), their names included, for credentials. A finding
// never contains the matched value, so it can go into a log or a run reason: a content finding
// is "<quoted file>:<line>: <kind>", a name that matches is reported without the name. A file
// that no longer exists (a deletion) has only its name scanned; a symlink is scanned on its
// target text, which is what git commits for it; a file that cannot be read is a finding of its
// own ("<quoted file>: cannot read"), never a silent pass. Unlike the paia script, a file that
// is not UTF-8 is scanned too.
func ScanSecrets(dir string, files []string) []string {
	var findings []string
	for _, f := range files {
		body, err := committedContent(filepath.Join(dir, f))
		switch {
		case errors.Is(err, fs.ErrNotExist):
			body = nil
		case err != nil:
			findings = append(findings, scanBody(f, nil)...)
			findings = append(findings, showPath(f)+": cannot read")
			continue
		}
		findings = append(findings, scanBody(f, body)...)
	}
	return findings
}

// committedContent is what git stores for path: the target text of a symlink, the bytes of
// any other file.
func committedContent(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), err
	}
	return os.ReadFile(path)
}

// ShowPath is showPath for the jobs in the subpackages: a path in a run reason or an error text
// goes through it, so a newline cannot forge a log line and a secret-looking name is withheld.
func ShowPath(p string) string { return showPath(p) }
