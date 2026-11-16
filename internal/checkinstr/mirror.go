package checkinstr

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// reportWalkError names the directory that could not be read on stderr, as find did.
func reportWalkError(errOut io.Writer, err error) {
	_, _ = fmt.Fprintf(errOut, "check-instructions: %v\n", err)
}

// errCompare is "git could not compare a pair": could not measure, never "no difference".
var errCompare = errors.New("diff could not compare a skill pair")

// hunkHeader is a unified hunk header; git can add a function name after it, diff -u does not.
var hunkHeader = regexp.MustCompile(`^@@ -\S+ \+\S+ @@`)

// Check 3: `.claude/skills/` and `.agents/skills/` carry the same names.
func checkMirror(rp *repo, r *report) {
	claude, agents := skillDirs(rp.claudeSkills), skillDirs(rp.agentsSkills)
	both := map[string]bool{}
	for _, name := range intersect(claude, agents) {
		both[name] = true
	}
	var missing []string
	for _, name := range append(claude, agents...) {
		if !both[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, name := range missing {
		r.fail("mirror missing: '" + name + "' is not in both .claude/skills/ and .agents/skills/")
	}
	if len(missing) == 0 {
		r.ok("mirror .claude/skills <-> .agents/skills matches")
	}
}

// mirrorDifferences prints a header and the deviating lines per differing file (`<` = .claude,
// `>` = .agents), without line numbers: an equal edit on both sides shifts the differences but
// does not change them. A skill whose SKILL.md carries the lighthouse marker (`metadata.lighthouse`)
// is left out: the package manages its two variants itself. errCompare when git could not
// compare a pair; the text is then what came before it. git's stderr goes to errOut.
func mirrorDifferences(rp *repo, errOut io.Writer) (string, error) {
	var b strings.Builder
	for _, name := range intersect(skillDirs(rp.claudeSkills), skillDirs(rp.agentsSkills)) {
		left, right := rp.claudeSkills+"/"+name, rp.agentsSkills+"/"+name
		if skillMeta(left, "lighthouse") != "" || skillMeta(right, "lighthouse") != "" {
			continue
		}
		// An unreadable directory gives an incomplete list, and that must not read as "no
		// difference".
		leftFiles, err := files(left)
		if err != nil {
			reportWalkError(errOut, err)
			return b.String(), errCompare
		}
		rightFiles, err := files(right)
		if err != nil {
			reportWalkError(errOut, err)
			return b.String(), errCompare
		}
		for _, rel := range union(leftFiles, rightFiles) {
			out, differ, err := gitDiffU0(sideOf(left+"/"+rel), sideOf(right+"/"+rel), errOut)
			if err != nil {
				return b.String(), errCompare
			}
			if !differ {
				continue
			}
			b.WriteString("=== " + name + "/" + rel + "\n")
			b.WriteString(mirrorForm(out))
		}
	}
	return b.String(), nil
}

// sideOf is path, or /dev/null when that side has no such file.
func sideOf(path string) string {
	if isFile(path) {
		return path
	}
	return os.DevNull
}

// union is the sorted, unique names of a and b.
func union(a, b []string) []string {
	seen := map[string]bool{}
	var all []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			all = append(all, s)
		}
	}
	sort.Strings(all)
	return all
}

// gitDiffU0 compares two files with git and a fixed algorithm: GNU diff and the BSD diff of
// macOS line up the same hunks differently, git is the same on both. differ is git's exit 1;
// any exit other than 0 or 1 is an error.
func gitDiffU0(left, right string, errOut io.Writer) (out string, differ bool, err error) {
	cmd := exec.Command("git", "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"-U0", "--diff-algorithm=myers", "--no-indent-heuristic", "--", left, right)
	cmd.Stderr = errOut
	data, err := cmd.Output()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return "", false, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return string(data), true, nil
	}
	return "", false, err
}

// mirrorForm turns a unified diff into `<`/`>` lines; the header before the first `@@` is
// dropped. A binary difference has no hunks: then it is the diff itself.
func mirrorForm(diff string) string {
	diff = strings.TrimRight(diff, "\n")
	var b strings.Builder
	hunk := false
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			hunk = true
		case !hunk:
		case strings.HasPrefix(l, "-"):
			b.WriteString("< " + l[1:] + "\n")
		case strings.HasPrefix(l, "+"):
			b.WriteString("> " + l[1:] + "\n")
		}
	}
	if b.Len() == 0 {
		return diff + "\n"
	}
	return b.String()
}

// Check 11: check 3 sees only names; this one compares every file of every pair. What
// deliberately differs per harness is in the file of `[paths] mirror_exceptions`, in the form
// mirrorDifferences prints.
func checkMirrorContent(rp *repo, r *report, errOut io.Writer) {
	current, err := mirrorDifferences(rp, errOut)
	if err != nil {
		r.skip("mirror-content: diff could not compare a skill pair, so the content was not measured")
		return
	}
	current = strings.TrimRight(current, "\n")
	expected := ""
	if rp.mirrorExceptions != "" && isFile(rp.mirrorExceptions) {
		data, _ := os.ReadFile(rp.mirrorExceptions)
		expected = strings.TrimRight(string(data), "\n")
	}
	if current == expected {
		r.ok("mirror-content matches, apart from the recorded exceptions")
		return
	}

	rel, origin := "<file>", "no recorded exceptions ([paths] mirror_exceptions is not set)"
	if rp.mirrorExceptions != "" {
		rel = strings.TrimPrefix(rp.mirrorExceptions, rp.root+"/")
		origin = rel
	}
	r.fail("mirror-content differs from " + origin + " (- recorded, + now; < .claude, > .agents):")
	r.line(indent(display(expected, current)))
	r.line("      Bring the other side in line. If the difference is deliberate per harness, record it with\n")
	if rp.mirrorExceptions == "" {
		r.line("      [paths] mirror_exceptions = \"<file>\" in lighthouse.toml, and then\n")
	}
	r.line("      lighthouse check-instructions --print-mirror-exceptions > " + rel + "\n")
}

// display is the unified diff from expected to current, from the first hunk on. It is only
// shown: the verdict rests on comparing the strings. "" when git cannot run.
func display(expected, current string) string {
	dir, err := os.MkdirTemp("", "check-instructions-")
	if err != nil {
		return ""
	}
	defer func() { _ = os.RemoveAll(dir) }()
	recorded, now := dir+"/recorded", dir+"/now"
	if os.WriteFile(recorded, []byte(expected+"\n"), 0o600) != nil ||
		os.WriteFile(now, []byte(current+"\n"), 0o600) != nil {
		return ""
	}
	// No external diff or textconv of the user: either could empty or rewrite the body.
	out, _ := exec.Command("git", "diff", "--no-index", "--no-color", "--no-ext-diff", "--no-textconv",
		"-U3", "--diff-algorithm=myers", "--no-indent-heuristic", recorded, now).Output()
	start := bytes.Index(out, []byte("\n@@"))
	if start < 0 {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.SplitAfter(string(out[start+1:]), "\n") {
		if loc := hunkHeader.FindStringIndex(l); loc != nil {
			l = l[:loc[1]] + "\n"
		}
		b.WriteString(l)
	}
	return b.String()
}

// indent puts six spaces before every line of text.
func indent(text string) string {
	if text == "" {
		return ""
	}
	return "      " + strings.ReplaceAll(strings.TrimSuffix(text, "\n"), "\n", "\n      ") + "\n"
}
