package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	types      = []string{"added", "changed", "fixed", "removed", "internal"}
	releasable = types[:4]
)

const (
	marker      = ".lighthouse"
	fragmentDir = "changelog.d"
)

var (
	tagRe      = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
	orderRe    = regexp.MustCompile(`^(.+)-(\d+)$`)
	groupTypes = []string{"added", "changed", "fixed", "removed"}
)

// fragment is one parsed changelog fragment.
type fragment struct{ path, typ, text string }

// fragmentError is a fragment that cannot be used (exit 1). The message starts with its path.
type fragmentError struct{ msg string }

func (e *fragmentError) Error() string { return e.msg }

// releaseError is a release that must not happen (exit 1).
type releaseError struct{ msg string }

func (e *releaseError) Error() string { return e.msg }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// parseFragment parses one fragment: UTF-8 (BOM allowed), CRLF allowed, front matter with one
// `type:` line.
func parseFragment(path string) (fragment, error) {
	refuse := func(format string, a ...any) error {
		return &fragmentError{msg: path + ": " + fmt.Sprintf(format, a...)}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fragment{}, refuse("cannot read: %v", err)
	}
	if !utf8.Valid(data) {
		return fragment{}, refuse("cannot read: not valid UTF-8")
	}
	raw := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	if lines[0] != "---" {
		return fragment{}, refuse("first line must be exactly `---`")
	}
	closeAt := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			closeAt = i
			break
		}
	}
	if closeAt < 0 {
		return fragment{}, refuse("front matter is not closed by a `---` line")
	}
	var entries []string
	for _, line := range lines[1:closeAt] {
		if strings.TrimSpace(line) != "" {
			entries = append(entries, line)
		}
	}
	if len(entries) != 1 {
		return fragment{}, refuse("front matter must hold exactly one line `type: <type>`, found %d", len(entries))
	}
	key, value, found := strings.Cut(entries[0], ":")
	if !found || strings.TrimSpace(key) != "type" {
		return fragment{}, refuse("unknown front matter line '%s', only `type:` is allowed", entries[0])
	}
	typ := strings.TrimSpace(value)
	if !contains(types, typ) {
		return fragment{}, refuse("unknown type '%s', expected one of %s", typ, strings.Join(types, ", "))
	}
	text := strings.TrimSpace(strings.Join(lines[closeAt+1:], "\n"))
	if text == "" {
		return fragment{}, refuse("empty text")
	}
	return fragment{path: path, typ: typ, text: text}, nil
}

// fragmentOrder is the sort key: the stem without a trailing `-<n>`, then n (no suffix is 1).
func fragmentOrder(name string) (string, int) {
	stem := strings.TrimSuffix(name, ".md")
	if m := orderRe.FindStringSubmatch(stem); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			n = int(^uint(0) >> 1)
		}
		return m[1], n
	}
	return stem, 1
}

// readFragments reads all fragments in dir, by name with `<name>-<n>.md` after `<name>.md` in
// numeric order (`x.md`, `x-2.md`, `x-10.md`). The marker is skipped, any other item that is not
// a regular `*.md` file is refused. A missing directory holds no fragments.
func readFragments(dir string) ([]fragment, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.SliceStable(names, func(i, j int) bool {
		si, ni := fragmentOrder(names[i])
		sj, nj := fragmentOrder(names[j])
		if si != sj {
			return si < sj
		}
		return ni < nj
	})
	var fragments []fragment
	for _, name := range names {
		if name == marker {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(name, ".md") || name == ".md" {
			return nil, &fragmentError{msg: p + ": not a `*.md` fragment"}
		}
		f, err := parseFragment(p)
		if err != nil {
			return nil, err
		}
		fragments = append(fragments, f)
	}
	return fragments, nil
}

// addedFragments lists the paths of fragments added in base...head. A rename counts as an add
// (`--no-renames`). A failure is a plain error: the caller cannot check (exit 2).
func addedFragments(root, base, head string) ([]string, error) {
	rng := base + "..." + head
	cmd := exec.Command("git", "-C", root, "diff", "--name-only", "--diff-filter=A", "--no-renames",
		"-z", rng, "--", fragmentDir+"/")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			detail = err.Error()
		}
		return nil, fmt.Errorf("git diff %s failed: %s", rng, detail)
	}
	if !utf8.Valid(out) {
		return nil, fmt.Errorf("git diff %s failed: output is not valid UTF-8", rng)
	}
	var added []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && path.Base(p) != marker {
			added = append(added, p)
		}
	}
	return added, nil
}

func parseTag(tag string) ([3]int, bool) {
	m := tagRe.FindStringSubmatch(tag)
	if m == nil {
		return [3]int{}, false
	}
	var v [3]int
	for i := range v {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		v[i] = n
	}
	return v, true
}

// renderSection renders a CHANGELOG.md section: groups in a fixed order, `internal` never.
func renderSection(version, date string, frags []fragment) string {
	blocks := []string{fmt.Sprintf("## [%s] - %s", version, date)}
	for _, typ := range groupTypes {
		var items []string
		for _, f := range frags {
			if f.typ == typ {
				items = append(items, "- "+strings.ReplaceAll(f.text, "\n", "\n  "))
			}
		}
		if len(items) > 0 {
			title := strings.ToUpper(typ[:1]) + typ[1:]
			blocks = append(blocks, "### "+title+"\n\n"+strings.Join(items, "\n"))
		}
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

// splitLines splits s on "\r\n", "\r" and "\n". Without keepEnds the terminators are dropped,
// and a trailing terminator does not add an empty last line.
func splitLines(s string, keepEnds bool) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\r' && s[i] != '\n' {
			continue
		}
		end := i + 1
		if s[i] == '\r' && end < len(s) && s[end] == '\n' {
			end++
		}
		if keepEnds {
			lines = append(lines, s[start:end])
		} else {
			lines = append(lines, s[start:i])
		}
		start = end
		i = end - 1
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// insertSection puts section above the first `## ` line, or at the end when there is none.
func insertSection(changelog, section string) string {
	lines := splitLines(changelog, true)
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			return strings.Join(lines[:i], "") + section + "\n" + strings.Join(lines[i:], "")
		}
	}
	sep := ""
	if changelog != "" {
		if !strings.HasSuffix(changelog, "\n") {
			changelog += "\n"
		}
		sep = "\n"
	}
	return changelog + sep + section
}

// extractSection returns the body under `## [<tag>] - ...` up to the next `## ` line, stripped;
// false when the section is absent.
func extractSection(changelog, tag string) (string, bool) {
	prefix := "## [" + tag + "] - "
	var body []string
	in := false
	for _, line := range splitLines(changelog, false) {
		if !in {
			in = strings.HasPrefix(line, prefix)
		} else if strings.HasPrefix(line, "## ") {
			break
		} else {
			body = append(body, line)
		}
	}
	if !in {
		return "", false
	}
	return strings.TrimSpace(strings.Join(body, "\n")), true
}

// release writes the section of version for the open fragments into CHANGELOG.md and removes
// them. A fragment named in released went out in an earlier release: it is removed without
// entering the section and reported on notes (nil is silent). It returns version. A version that
// already has a tag, or no releasable fragment left, is refused (releaseError) and leaves the
// tree alone.
func release(root, version, date string, tags []string, released map[string]bool, notes io.Writer) (string, error) {
	if contains(tags, version) {
		return "", &releaseError{msg: version + " already has a tag"}
	}
	all, err := readFragments(filepath.Join(root, fragmentDir))
	if err != nil {
		return "", err
	}
	var frags, skipped []fragment
	for _, f := range all {
		if released[filepath.Base(f.path)] {
			skipped = append(skipped, f)
		} else {
			frags = append(frags, f)
		}
	}
	releasableFound := false
	for _, f := range frags {
		if contains(releasable, f.typ) {
			releasableFound = true
		}
	}
	if !releasableFound {
		return "", &releaseError{msg: "nothing to release"}
	}
	p := filepath.Join(root, "CHANGELOG.md")
	text, err := os.ReadFile(p)
	if err != nil {
		return "", &releaseError{msg: fmt.Sprintf("%s: cannot read: %v", p, err)}
	}
	out := insertSection(string(text), renderSection(version, date, frags))
	if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
		return "", err
	}
	for _, f := range all {
		if err := os.Remove(f.path); err != nil {
			return "", err
		}
	}
	if notes != nil {
		for _, f := range skipped {
			say(notes, "skipped "+filepath.Base(f.path)+": already released")
		}
	}
	return version, nil
}
