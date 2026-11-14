// Package commitgate holds the ports of the helpers of commit-test-gate and the gate itself.
package commitgate

import (
	"strings"

	"github.com/CorneeldH/lighthouse/internal/gitinvocations"
)

// Long options that demand a value: without "=" the next token is the value, not a flag.
var valueLongOpts = map[string]bool{
	"--message": true, "--file": true, "--reuse-message": true, "--reedit-message": true,
	"--template": true, "--author": true, "--date": true, "--cleanup": true,
	"--fixup": true, "--squash": true,
}

// A short option that demands a value; at the end of a cluster the next token is that value.
const valueShortLetters = "mFCct"

// -S and -u take their value attached (-Skeyid), so they stop the scan of the cluster and
// consume nothing from the next token.
const attachedOnlyShortLetters = "uS"

// scanShortCluster reads a cluster of short options (`-anm` is `anm`). Scanning stops at the first
// letter that takes a value: what follows is that value, not a flag.
func scanShortCluster(letters string) (hasN, swallowsNext bool) {
	rs := []rune(letters)
	for idx, ch := range rs {
		switch {
		case ch == 'n':
			return true, false
		case strings.ContainsRune(valueShortLetters, ch):
			return false, idx == len(rs)-1
		case strings.ContainsRune(attachedOnlyShortLetters, ch):
			return false, false
		}
	}
	return false, false
}

func hasNoVerify(tokens []string) bool {
	for i := 0; i < len(tokens); {
		tok := tokens[i]
		switch {
		case tok == "--":
			return false
		case tok == "--no-verify":
			return true
		case strings.HasPrefix(tok, "--"):
			name, _, hasEq := strings.Cut(tok, "=")
			if valueLongOpts[name] && !hasEq {
				i += 2
			} else {
				i++
			}
		case strings.HasPrefix(tok, "-") && len(tok) > 1:
			found, consumes := scanShortCluster(tok[1:])
			if found {
				return true
			}
			if consumes {
				i += 2
			} else {
				i++
			}
		default:
			i++
		}
	}
	return false
}

// CommandHasNoVerify reports whether a `git ... commit` invocation in command carries
// --no-verify or -n. Only the words after the subcommand count. An open quote gives false.
func CommandHasNoVerify(command, cwd string) bool {
	// An open quote is no block here: the gate refuses an unparseable commit earlier. The steps
	// found before it still count, as in the Python generator.
	steps, _ := gitinvocations.Walk(command, cwd)
	for _, step := range steps {
		if gitinvocations.Basename(step.Words[step.Idx]) != "git" {
			continue
		}
		_, _, _, _, action := gitinvocations.GitTarget(step)
		if action < len(step.Words) && step.Words[action] == "commit" && hasNoVerify(step.Words[action+1:]) {
			return true
		}
	}
	return false
}
