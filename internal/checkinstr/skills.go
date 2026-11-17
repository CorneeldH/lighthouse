package checkinstr

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// blockScalar is the value of a YAML folded (`>`) or literal (`|`) block scalar header.
var blockScalar = regexp.MustCompile(`^[>|][-+0-9]*[ \t]*$`)

// metadataHeader opens the `metadata:` block of a frontmatter.
var metadataHeader = regexp.MustCompile(`^metadata:[ \t]*$`)

// lines splits a file into lines as awk reads records: a final newline ends the last line, it
// does not start an empty one. A path that is not a readable regular file has no lines.
func lines(path string) []string {
	if !isFile(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// frontmatterField is a field from the frontmatter of file: "" when the field is missing OR
// carries no value, so an empty `upstream:` counts as missing. A folded (`>`) or literal (`|`)
// block scalar is read from its indented continuation lines.
func frontmatterField(file, field string) string {
	ls := lines(file)
	if len(ls) == 0 || ls[0] != "---" {
		return ""
	}
	prefix := field + ": "
	collecting, fold := false, false
	var buf strings.Builder
	for _, l := range ls[1:] {
		if collecting {
			if l == "---" || (l != "" && !startsBlank(l)) {
				return strings.TrimRight(buf.String(), "\n")
			}
			line := strings.TrimLeft(l, " \t")
			text := buf.String()
			switch {
			case line == "":
				buf.WriteString("\n")
			case text == "" || strings.HasSuffix(text, "\n"):
				buf.WriteString(line)
			case fold:
				buf.WriteString(" " + line)
			default:
				buf.WriteString("\n" + line)
			}
			continue
		}
		if l == "---" {
			return ""
		}
		if value, found := strings.CutPrefix(l, prefix); found {
			if blockScalar.MatchString(value) {
				collecting, fold = true, value[0] == '>'
				continue
			}
			return value
		}
	}
	return strings.TrimRight(buf.String(), "\n")
}

// skillMeta is a key under the `metadata:` block of the SKILL.md in dir, where the Agent Skills
// spec puts everything outside its six top-level fields. "" when the key is missing OR empty.
func skillMeta(dir, field string) string {
	ls := lines(dir + "/SKILL.md")
	if len(ls) == 0 || ls[0] != "---" {
		return ""
	}
	key := regexp.MustCompile("^[ \t]+" + regexp.QuoteMeta(field) + ": ")
	inMeta := false
	for _, l := range ls[1:] {
		if l == "---" {
			return ""
		}
		if metadataHeader.MatchString(l) {
			inMeta = true
			continue
		}
		if inMeta && strings.Trim(l, " \t") != "" && !startsBlank(l) {
			inMeta = false
		}
		if loc := key.FindStringIndex(l); inMeta && loc != nil {
			return l[loc[1]:]
		}
	}
	return ""
}

func startsBlank(l string) bool { return l != "" && (l[0] == ' ' || l[0] == '\t') }

// skillDirs are the directory names (not paths) under dir, sorted by bytes. A directory that does
// not exist yields nothing. Like `find <dir>`, a symlinked dir is not followed, and neither is a
// symlinked entry.
func skillDirs(dir string) []string {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return nil
	}
	entries, _ := os.ReadDir(dir) // what was read before an error still counts, as with find
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// files are the regular files under dir, relative and sorted by bytes. err is set when a
// directory could not be read: then the list is incomplete.
func files(dir string) ([]string, error) {
	var rels []string
	var walkErr error
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			walkErr = err
			return nil
		}
		if d.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, path)
			rels = append(rels, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(rels)
	return rels, walkErr
}

// dirHash is a content hash over a skill directory: each relative path name plus the file
// content, in a fixed order. The path is in there on purpose, so a renamed file is a
// difference too. What cannot be read adds nothing.
func dirHash(dir string) string {
	h := sha256.New()
	rels, _ := files(dir)
	for _, rel := range rels {
		h.Write([]byte("./" + rel + "\n"))
		if data, err := os.ReadFile(dir + "/" + rel); err == nil {
			h.Write(data)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// intersect is the sorted names in both a and b.
func intersect(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, s := range b {
		in[s] = true
	}
	var both []string
	for _, s := range a {
		if in[s] {
			both = append(both, s)
		}
	}
	sort.Strings(both)
	return both
}

// duplicates are the keys that occur more than once, sorted, each with its values in order.
func duplicates(pairs [][2]string) (keys []string, values map[string][]string) {
	values = map[string][]string{}
	for _, p := range pairs {
		values[p[0]] = append(values[p[0]], p[1])
	}
	for k, v := range values {
		if len(v) > 1 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, values
}

// spaced is the list as the script prints it: each item followed by a space.
func spaced(items []string) string {
	var b strings.Builder
	for _, s := range items {
		b.WriteString(s + " ")
	}
	return b.String()
}

// Check 1: two directories that carry the same `name:`. Claude then loads one and stays silent
// about the other.
func checkDuplicateName(rp *repo, r *report) {
	if !isDir(rp.claudeSkills) {
		r.skip("duplicate names: no .claude/skills, so there is nothing to measure here")
		return
	}
	var pairs [][2]string
	for _, dir := range skillDirs(rp.claudeSkills) {
		if name := frontmatterField(rp.claudeSkills+"/"+dir+"/SKILL.md", "name"); name != "" {
			pairs = append(pairs, [2]string{name, dir})
		}
	}
	names, dirs := duplicates(pairs)
	for _, name := range names {
		r.fail("duplicate name '" + name + "' claimed by: " + spaced(dirs[name]))
	}
	if len(names) == 0 {
		r.ok("no names claimed twice")
	}
}

// Check 2: two skill directories with identical content within one root. Never across the
// mirror: `.claude/skills/x` and `.agents/skills/x` are a pair by design.
func checkByteIdentical(rp *repo, r *report) {
	if !isDir(rp.claudeSkills) {
		r.skip("byte-identical: no .claude/skills, so there is nothing to measure here")
		return
	}
	found := false
	for _, root := range [][2]string{{rp.claudeSkills, ".claude"}, {rp.agentsSkills, ".agents"}} {
		var pairs [][2]string
		for _, dir := range skillDirs(root[0]) {
			pairs = append(pairs, [2]string{dirHash(root[0] + "/" + dir), dir})
		}
		hashes, dirs := duplicates(pairs)
		for _, hash := range hashes {
			found = true
			r.fail("byte-identical pair in " + root[1] + "/skills: " + spaced(dirs[hash]))
		}
	}
	if !found {
		r.ok("no byte-identical pairs within a skill root")
	}
}

// Check 5: personal skills overshadow project skills of the same name, silently.
func checkShadow(rp *repo, r *report) {
	if !isDir(rp.personal) {
		r.skip("shadow: '" + rp.personal + "' does not exist on this machine, so there is nothing to measure here")
		return
	}
	shadowed := intersect(skillDirs(rp.personal), skillDirs(rp.claudeSkills))
	for _, name := range shadowed {
		r.fail("shadow: '" + name + "' is in " + rp.personal + " and in .claude/skills/; the personal version wins")
	}
	if len(shadowed) == 0 {
		r.ok("no personal skill shadows a project skill")
	}
}
