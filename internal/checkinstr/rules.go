package checkinstr

import (
	"os"
	"sort"
	"strings"
)

// Check 7: a rule without `paths:` loads every session and so costs the same as CLAUDE.md.
func checkScoping(rp *repo, r *report) {
	rules := rp.root + "/.claude/rules"
	if !isDir(rules) {
		r.skip("scoping: '" + rules + "' does not exist, so there is no rule to check here")
		return
	}
	found := false
	for _, name := range ruleFiles(rules) {
		if !pathScoped(lines(rules + "/" + name)) {
			found = true
			r.fail("scoping: '" + name + "' has no paths: block with a non-empty value in its frontmatter")
		}
	}
	if !found {
		r.ok("every rule is path-scoped")
	}
}

// ruleFiles are the names of the regular `*.md` files directly in dir, sorted. Like `find`, a
// symlinked dir or file is not followed.
func ruleFiles(dir string) []string {
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		return nil
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}

// pathScoped is true when the frontmatter carries `paths:` with a non-empty value: inline
// (`paths: src/**`), or as a list item after it. Empty lines and comment lines between `paths:`
// and the list are valid YAML. A bare `paths:` or `paths: []` matches no path, so it counts as
// missing.
func pathScoped(ls []string) bool {
	if len(ls) == 0 || ls[0] != "---" {
		return false
	}
	for i := 1; i < len(ls); i++ {
		if ls[i] == "---" {
			return false
		}
		rest, found := strings.CutPrefix(ls[i], "paths:")
		if !found {
			continue
		}
		if rest = strings.Trim(rest, " \t"); rest != "" && rest != "[]" {
			return true
		}
		for _, next := range ls[i+1:] {
			trimmed := strings.Trim(next, " \t")
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			// "- " and not a bare "-": the closing "---" is no list item.
			return strings.HasPrefix(trimmed, "- ")
		}
		return false
	}
	return false
}
