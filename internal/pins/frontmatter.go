// Package pins holds what the pin checks share about SKILL.md files.
package pins

import "strings"

// SkillMetadata returns the key/value pairs of the `metadata:` block in the YAML
// frontmatter of a SKILL.md. Only the block nested under a top-level `metadata:` counts:
// a top-level key of the same name as a metadata key (`origin:` outside `metadata:`) is
// ignored. Surrounding single or double quotes are stripped from a value. Frontmatter is
// the text between a first line `---` and the next line `---`; a document without it, or
// without a `metadata:` block, gives an empty, non-nil map.
func SkillMetadata(content string) map[string]string {
	meta := map[string]string{}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return meta
	}
	inMeta := false
	for _, line := range lines[1:] {
		if strings.TrimRight(line, " \t") == "---" {
			break
		}
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			inMeta = strings.TrimRight(line, " \t") == "metadata:"
			continue
		}
		if !inMeta {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		meta[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
	}
	return meta
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}
