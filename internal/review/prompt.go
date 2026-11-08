// Package review runs one adversarial review round through an external Codex session. It is the
// port of the review.sh and verify.sh of the review-spec and review-plan skills.
package review

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	beginMarker = "---BEGIN PROMPT---"
	endMarker   = "---END PROMPT---"
)

// BuildPrompt builds the prompt from a template: the text between the two markers, with the
// placeholders substituted. The document text is never read, so a "{{" in the document cannot
// reach the prompt; only the template is substituted. A "--context" on a template without
// {{CONTEXT_PATHS}}, or a placeholder that stays, is an error.
func BuildPrompt(template []byte, doc, repo, round, prior string, contexts []string) (string, error) {
	// Python's read_text() reads in universal-newline mode.
	text := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(template))
	_, rest, found := strings.Cut(text, beginMarker)
	if !found {
		return "", fmt.Errorf("the prompt template has no %s marker", beginMarker)
	}
	body, _, _ := strings.Cut(rest, endMarker)
	body = strings.TrimSpace(body)

	var paths []string
	for _, c := range contexts {
		if c != "" {
			paths = append(paths, c)
		}
	}

	priorBlock := ""
	if prior != "none" {
		sidecar := sidecarPath(prior)
		if info, err := os.Stat(sidecar); err == nil && info.Mode().IsRegular() {
			priorBlock = "Previous review: `" + prior + "`. The author's decisions are in `" + sidecar + "`:\n" +
				"one entry per finding, keyed by `finding_id` = `r<round>-f<index>` (index =\n" +
				"1-based position in that review's `findings` array), with `decision` and\n" +
				"`reason`. Check specifically which accepted findings were answered in prose\n" +
				"but not actually resolved in the document. Do not re-raise a finding the\n" +
				"author rejected with a stated reason — instead say whether that reasoning is\n" +
				"wrong, and why."
		} else {
			priorBlock = "Previous review: `" + prior + "`. The author's responses are recorded there.\n" +
				"Check specifically which findings were answered in prose but not actually\n" +
				"resolved in the spec. Do not re-raise a finding the author rejected with a\n" +
				"stated reason — instead say whether that reasoning is wrong, and why."
		}
	}

	if len(paths) > 0 && !strings.Contains(body, "{{CONTEXT_PATHS}}") {
		return "", errors.New("--context given, but the template has no {{CONTEXT_PATHS}}")
	}
	contextBlock := "(none)"
	if len(paths) > 0 {
		contextBlock = "- " + strings.Join(paths, "\n- ")
	}

	for _, r := range [][2]string{
		{"{{SPEC_PATH}}", doc},
		{"{{DOC_PATH}}", doc},
		{"{{CONTEXT_PATHS}}", contextBlock},
		{"{{REPO}}", repo},
		{"{{ROUND}}", round},
		{"{{PRIOR_REVIEW_BLOCK}}", priorBlock},
	} {
		body = strings.ReplaceAll(body, r[0], r[1])
	}
	if strings.Contains(body, "{{") {
		return "", errors.New("unsubstituted placeholder in the prompt template")
	}
	return body, nil
}

// sidecarPath is `<name without .json>.decisions.json` next to prior, the way pathlib's
// with_name writes it: a leading "./" and a doubled slash are gone.
func sidecarPath(prior string) string {
	dir, name := "", prior
	if i := strings.LastIndex(prior, "/"); i >= 0 {
		dir, name = prior[:i+1], prior[i+1:]
	}
	for strings.HasPrefix(dir, "./") {
		dir = dir[2:]
	}
	for strings.Contains(dir, "//") {
		dir = strings.ReplaceAll(dir, "//", "/")
	}
	return dir + strings.TrimSuffix(name, ".json") + ".decisions.json"
}
