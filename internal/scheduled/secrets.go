package scheduled

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// secretPatterns is SECRET_PATTERN from the paia scripts, one alternative per entry so a finding
// can name its kind. Origin: CorneeldH/personal_ai_agent, scripts/codebase_map_refresh.py
// (line 142, commit c742152f1d267cf256fcc89749603a8a50dac3c4). Python's re.search on the
// alternation matches a line when any alternative matches it, which is what scanning these in
// turn does; every alternative is valid RE2 with the same meaning (`.` does not cross a newline).
var secretPatterns = []struct {
	kind string
	re   *regexp.Regexp
}{
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
}

// ScanSecrets scans files (relative to dir) for credentials and returns one finding per
// matching line as "<file>:<line>: <kind>". A finding never contains the matched value, so it
// can go into a log or a run reason. A file that no longer exists (a deletion) is skipped; a
// symlink is scanned on its target text, which is what git commits for it; a file that cannot
// be read is a finding of its own ("<file>: cannot read"), never a silent pass. Unlike the paia
// script, a file that is not UTF-8 is scanned too.
func ScanSecrets(dir string, files []string) []string {
	var findings []string
	for _, f := range files {
		body, err := committedContent(filepath.Join(dir, f))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			findings = append(findings, f+": cannot read")
			continue
		}
		for i, line := range bytes.Split(body, []byte("\n")) {
			for _, p := range secretPatterns {
				if p.re.Match(line) {
					findings = append(findings, fmt.Sprintf("%s:%d: %s", f, i+1, p.kind))
					break
				}
			}
		}
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
