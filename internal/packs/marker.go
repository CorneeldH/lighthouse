package packs

import (
	"fmt"
	"path"
	"strings"
)

// commentPrefix is the version marker as a comment line, in the formats that allow one.
const commentPrefix = "# lighthouse: "

// Mark puts the version marker `<pkg>@<version>` into content, in the form the format
// of path allows (spec, Versiemarker en manifest):
//
//   - shell, Python and YAML: a line `# lighthouse: <pkg>@<version>`, under the shebang;
//   - Markdown with frontmatter: `metadata.lighthouse: "<pkg>@<version>"`;
//   - everything else, JSON and pull_request_template.md included: unchanged.
//
// An existing marker is replaced, so marking twice gives the same result.
func Mark(p string, content []byte, pkg, version string) ([]byte, error) {
	marker := pkg + "@" + version
	base := path.Base(p)
	switch path.Ext(p) {
	case ".sh", ".bash", ".py", ".yml", ".yaml":
		return markComment(string(content), marker), nil
	case ".md":
		if base == "pull_request_template.md" || !strings.HasPrefix(string(content), "---\n") {
			return content, nil
		}
		out, err := markFrontmatter(string(content), marker)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		return []byte(out), nil
	default:
		return content, nil
	}
}

func markComment(text, marker string) []byte {
	line := commentPrefix + marker + "\n"
	head := ""
	if strings.HasPrefix(text, "#!") {
		end := strings.IndexByte(text, '\n')
		if end < 0 {
			return []byte(text + "\n" + line)
		}
		head, text = text[:end+1], text[end+1:]
	}
	if strings.HasPrefix(text, commentPrefix) {
		if end := strings.IndexByte(text, '\n'); end >= 0 {
			text = text[end+1:]
		} else {
			text = ""
		}
	}
	return []byte(head + line + text)
}

func markFrontmatter(text, marker string) (string, error) {
	lines := strings.SplitAfter(text, "\n")
	closing := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r\n") == "---" {
			closing = i
			break
		}
	}
	if closing < 0 {
		return "", fmt.Errorf("frontmatter has no closing ---")
	}
	entry := fmt.Sprintf("  lighthouse: %q\n", marker)
	for i := 1; i < closing; i++ {
		trimmed := strings.TrimRight(lines[i], "\r\n")
		if !strings.HasPrefix(trimmed, "metadata:") {
			continue
		}
		if strings.TrimSpace(strings.TrimPrefix(trimmed, "metadata:")) != "" {
			return "", fmt.Errorf("frontmatter metadata must be a block mapping, not %q", trimmed)
		}
		rest := lines[i+1:]
		if len(rest) > 0 && strings.HasPrefix(rest[0], "  lighthouse:") {
			rest = rest[1:]
		}
		out := append(append(append([]string{}, lines[:i+1]...), entry), rest...)
		return strings.Join(out, ""), nil
	}
	out := append(append(append([]string{}, lines[:closing]...), "metadata:\n", entry), lines[closing:]...)
	return strings.Join(out, ""), nil
}
