package agentaudit

import (
	"os"
	"path/filepath"
	"strings"
)

// interpreters are programs a hook command runs its script with; a word naming one is
// never the script itself, even when /bin/bash exists.
var interpreters = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "env": true,
	"node": true, "deno": true, "bun": true, "python": true, "python3": true, "uv": true,
}

var unquote = strings.NewReplacer(`"`, "", `'`, "")

// words splits a command line on whitespace outside quotes and drops the quotes, so
// `"$CLAUDE_PROJECT_DIR"/x.sh` stays one word. An unbalanced quote runs to the end.
func words(command string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	started := false
	flush := func() {
		if started {
			out = append(out, cur.String())
		}
		cur.Reset()
		started = false
	}
	for _, r := range command {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}

// looksLikeScript reports whether a path reads as a script: it has an extension or lies
// under a .claude directory. A bare binary such as /usr/bin/jq does not.
func looksLikeScript(path string) bool {
	slashed := filepath.ToSlash(path)
	return filepath.Ext(slashed) != "" ||
		strings.HasPrefix(slashed, ".claude/") || strings.Contains(slashed, "/.claude/")
}

// within reports whether path lies under dir.
func within(path, dir string) bool {
	if dir == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// scriptPath is the script a hook command runs, "" for an inline command. Words are
// read with quotes dropped, $CLAUDE_PROJECT_DIR replaced by root, ~ by home, and a
// relative path against root (or the directory of a preceding `cd`). A word counts only
// when it contains a "/", is no flag or URL, and either names an existing regular file
// that looks like a script or lies under root or home, or does not exist but looks like
// a script (returned anyway, for missing-script). An existing file wins over a missing
// one.
func scriptPath(command, root, home string) string {
	replacer := strings.NewReplacer("${CLAUDE_PROJECT_DIR}", root, "$CLAUDE_PROJECT_DIR", root)
	base := root
	missing := ""
	cd := false
	for _, word := range words(command) {
		word = replacer.Replace(word)
		if cd {
			cd = false
			if filepath.IsAbs(word) {
				base = word
			} else {
				base = filepath.Join(base, word)
			}
			continue
		}
		if word == "cd" {
			cd = true
			continue
		}
		if word == "" || strings.HasPrefix(word, "-") || strings.Contains(word, "://") ||
			!strings.Contains(word, "/") || interpreters[filepath.Base(word)] {
			continue
		}
		switch {
		case strings.HasPrefix(word, "~/"):
			if home == "" {
				continue
			}
			word = filepath.Join(home, word[2:])
		case !filepath.IsAbs(word):
			word = filepath.Join(base, word)
		}
		info, err := os.Stat(word)
		switch {
		case err == nil && info.Mode().IsRegular():
			if looksLikeScript(word) || within(word, root) || within(word, home) {
				return word
			}
		case err != nil && missing == "" && looksLikeScript(word):
			missing = word
		}
	}
	return missing
}
