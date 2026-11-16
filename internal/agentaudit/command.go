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

// separators end one simple command and start the next.
var separators = map[string]bool{"&&": true, "||": true, ";": true, "|": true}

// scriptPath is the script a hook command runs, "" for an inline command. Words are
// read with quotes dropped, $CLAUDE_PROJECT_DIR replaced by root, $HOME and ~ by home,
// and a relative path against root (or the directory of a `cd` in command position).
// A word still holding an unexpanded $VAR is skipped. A word counts only when it
// contains a "/", is no flag or URL, and either names an existing regular file that
// looks like a script or lies under root or home, or does not exist, looks like a
// script and stands in command position (first word, after a separator or `cd <dir> &&`,
// or right after an interpreter): that one is returned for missing-script, so an
// argument such as `> /tmp/out.log` never counts. An existing file wins over a missing one.
// The code after an interpreter's -c that holds whitespace is a command line of its own:
// it is read the same way, and never taken as a path itself.
func scriptPath(command, root, home string) string {
	pairs := []string{"${CLAUDE_PROJECT_DIR}", root, "$CLAUDE_PROJECT_DIR", root}
	if home != "" {
		pairs = append(pairs, "${HOME}", home, "$HOME", home)
	}
	replacer := strings.NewReplacer(pairs...)
	base := root
	missing := ""
	cd := false
	code := false // the previous word was the -c of an interpreter
	cmdPos := true
	for _, raw := range words(command) {
		if code {
			code = false
			if strings.ContainsAny(raw, " \t\n") {
				cmdPos = false
				if p := scriptPath(raw, root, home); p != "" {
					return p
				}
				continue
			}
		}
		if separators[raw] {
			cmdPos = true
			continue
		}
		word := replacer.Replace(raw)
		if cd {
			cd = false
			if filepath.IsAbs(word) {
				base = word
			} else {
				base = filepath.Join(base, word)
			}
			continue
		}
		isCmd := cmdPos
		if strings.HasPrefix(word, "-") && cmdPos {
			code = word == "-c"
			continue // a flag of an interpreter keeps the script position
		}
		cmdPos = false
		if isCmd && word == "cd" {
			cd = true
			continue
		}
		if interpreters[filepath.Base(word)] {
			cmdPos = true
			continue
		}
		if word == "" || strings.HasPrefix(word, "-") || strings.Contains(word, "$") ||
			strings.Contains(word, "://") || !strings.Contains(word, "/") {
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
		case err != nil && isCmd && missing == "" && looksLikeScript(word):
			missing = word
		}
	}
	return missing
}
