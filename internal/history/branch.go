package history

import (
	"regexp"
	"strings"
)

var (
	cdRe   = regexp.MustCompile(`(?:^|[;&|(]\s*)cd\s+("[^"]*"|'[^']*'|[^\s;&|]+)`)
	gitCRe = regexp.MustCompile(`\bgit\s+-C\s+("[^"]*"|'[^']*'|\S+)`)
	pushRe = regexp.MustCompile(`\bgit\s+(?:-C\s+(?:"[^"]*"|'[^']*'|\S+)\s+)?push\b([^;&|]*)`)
)

func unquote(s string) string { return strings.Trim(s, `"'`) }

// ResolveBranch gives the branch a git command acted on: the target of a push refspec,
// else unknown when the command moves to another directory that no session cwd explains,
// else the gitBranch of the record.
func ResolveBranch(command, gitBranch string, knownCwds map[string]string) (branch string, known bool) {
	if m := pushRe.FindStringSubmatch(command); m != nil {
		var pos []string
		for _, a := range strings.Fields(m[1]) {
			if !strings.HasPrefix(a, "-") {
				pos = append(pos, a)
			}
		}
		if len(pos) >= 2 {
			t := strings.TrimPrefix(pos[1], "+")
			if i := strings.LastIndex(t, ":"); i >= 0 {
				t = t[i+1:]
			}
			t = strings.TrimPrefix(strings.TrimPrefix(t, "+"), "refs/heads/")
			if t != "" && t != "HEAD" {
				return t, true
			}
		}
	}
	var paths []string
	for _, m := range cdRe.FindAllStringSubmatch(command, -1) {
		paths = append(paths, unquote(m[1]))
	}
	for _, m := range gitCRe.FindAllStringSubmatch(command, -1) {
		paths = append(paths, unquote(m[1]))
	}
	if len(paths) > 0 {
		p := paths[len(paths)-1]
		if b := knownCwds[p]; b != "" {
			return b, true
		}
		return "", false
	}
	if gitBranch == "" || gitBranch == "HEAD" {
		return "", false
	}
	return gitBranch, true
}
