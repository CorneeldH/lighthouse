package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var headingRe = regexp.MustCompile(`^## \[(v\d+\.\d+\.\d+)\] - `)

// fullSection returns the section of tag with its `## [tag] - date` heading, up to the next
// `## ` line, and false when the section is absent.
func fullSection(changelog, tag string) (string, bool) {
	prefix := "## [" + tag + "] - "
	var out []string
	in := false
	for _, line := range splitLines(changelog, true) {
		if !in {
			in = strings.HasPrefix(line, prefix)
		} else if strings.HasPrefix(line, "## ") {
			break
		}
		if in {
			out = append(out, line)
		}
	}
	if !in {
		return "", false
	}
	return strings.TrimRight(strings.Join(out, ""), "\r\n") + "\n", true
}

// versionLess reports whether tag a is a lower release than tag b.
func versionLess(a, b string) bool {
	va, _ := parseTag(a)
	vb, _ := parseTag(b)
	for i := range va {
		if va[i] != vb[i] {
			return va[i] < vb[i]
		}
	}
	return false
}

// insertSectionOrdered puts section above the first section of a lower version, or at the end
// of the sections when none is lower. A section for the same tag is never added twice.
func insertSectionOrdered(changelog, tag, section string) string {
	if _, ok := fullSection(changelog, tag); ok {
		return changelog
	}
	lines := splitLines(changelog, true)
	last := -1
	for i, line := range lines {
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		last = i
		if versionLess(m[1], tag) {
			return strings.Join(lines[:i], "") + section + "\n" + strings.Join(lines[i:], "")
		}
	}
	if last < 0 {
		return insertSection(changelog, section)
	}
	if !strings.HasSuffix(changelog, "\n") {
		changelog += "\n"
	}
	return changelog + "\n" + section
}

// syncTag copies the section of tag from the checkout source into root, and removes from root
// the fragments the release commit of tag removed. It changes nothing a second time.
func syncTag(root, source, tag string, stderr io.Writer) error {
	srcText, err := os.ReadFile(filepath.Join(source, "CHANGELOG.md"))
	if err != nil {
		return &releaseError{msg: filepath.Join(source, "CHANGELOG.md") + ": cannot read: " + err.Error()}
	}
	section, ok := fullSection(string(srcText), tag)
	if !ok {
		return &releaseError{msg: "no section for " + tag + " in " + filepath.Join(source, "CHANGELOG.md")}
	}
	p := filepath.Join(root, "CHANGELOG.md")
	text, err := os.ReadFile(p)
	if err != nil {
		return &releaseError{msg: p + ": cannot read: " + err.Error()}
	}
	if out := insertSectionOrdered(string(text), tag, section); out != string(text) {
		if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
			return err
		}
	}
	sha, err := releaseCommitOf(source, tag)
	if err != nil {
		return err
	}
	if sha == "" {
		return &releaseError{msg: "no `chore(release): " + tag + "` commit under " + tag}
	}
	names, err := removedFragments(source, sha)
	if err != nil {
		return err
	}
	for _, n := range names {
		err := os.Remove(filepath.Join(root, fragmentDir, n))
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove fragment %s: %w", n, err)
		}
		if err == nil {
			sayf(stderr, "removed fragment %s\n", n)
		}
	}
	return nil
}
