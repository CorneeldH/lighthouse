package agentaudit

import (
	"os"
	"path/filepath"
	"regexp"
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
// `"$CLAUDE_PROJECT_DIR"/x.sh` stays one word. An unbalanced quote runs to the end. A run
// of ;, | and & outside quotes ends the word before it and is a word of its own, so
// `x.sh;` and `a&&./b.sh` split as the shell splits them. A redirection (`>`, `2>&1`,
// `&>>file`, `<in`) starts a word of its own, with its target glued to it when the
// command glues it: its & is no separator.
func words(command string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	started := false
	inOp := false    // cur holds a run of ;, | and &
	inRedir := false // cur holds a redirection
	flush := func() {
		if started {
			out = append(out, cur.String())
		}
		cur.Reset()
		started = false
		inOp = false
		inRedir = false
	}
	write := func(r rune) {
		cur.WriteRune(r)
		started = true
	}
	runes := []rune(command)
	for i, r := range runes {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			continue
		}
		prev, next := rune(0), rune(0)
		if i > 0 {
			prev = runes[i-1]
		}
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		switch {
		case r == '"' || r == '\'':
			if inOp {
				flush()
			}
			quote = r
			started = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		case r == '>' || r == '<':
			if inOp || (!inRedir && !allDigits(cur.String())) {
				flush()
			}
			write(r)
			inRedir = true
		case r == '&' && inRedir && (prev == '>' || prev == '<'):
			write(r) // >&2, 2>&1
		case r == '&' && next == '>':
			flush()
			write(r) // &>file, &>>file
			inRedir = true
		case r == ';' || r == '|' || r == '&':
			if !inOp {
				flush()
			}
			write(r)
			inOp = true
		default:
			if inOp {
				flush()
			}
			write(r)
		}
	}
	flush()
	return out
}

// allDigits reports whether s is a file descriptor number, such as the 2 of 2>.
func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// redirect matches a redirection word; bareRedirect one whose target is the next word.
var (
	redirect     = regexp.MustCompile(`^(\d+|&)?[<>]`)
	bareRedirect = regexp.MustCompile(`^(\d+|&)?[<>]+&?$`)
)

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

// codeFlag reports whether an interpreter flag takes the code to run as its next word:
// -c alone, or short flags combined with it, such as -lc, -ec or -euc.
func codeFlag(word string) bool {
	letters, ok := strings.CutPrefix(word, "-")
	if !ok || letters == "" || !strings.Contains(letters, "c") {
		return false
	}
	for _, r := range letters {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

// knownVars matches $CLAUDE_PROJECT_DIR and $HOME, braced or followed by a character
// that cannot continue a variable name, so $HOMEBREW_PREFIX stays a variable of its own.
var knownVars = regexp.MustCompile(`\$\{(CLAUDE_PROJECT_DIR|HOME)\}|\$(CLAUDE_PROJECT_DIR|HOME)\b`)

// expand replaces $CLAUDE_PROJECT_DIR by root and $HOME by home (when known) in word;
// every other $VAR stays as it is.
func expand(word, root, home string) string {
	return knownVars.ReplaceAllStringFunc(word, func(v string) string {
		if strings.Contains(v, "CLAUDE_PROJECT_DIR") {
			return root
		}
		if home == "" {
			return v
		}
		return home
	})
}

// separators end one simple command and start the next.
var separators = map[string]bool{"&&": true, "||": true, ";": true, "|": true, "&": true, "|&": true, ";;": true}

// scriptPath is the script a hook command runs, "" for an inline command. Words are
// read with quotes dropped, $CLAUDE_PROJECT_DIR replaced by root, $HOME and ~ by home,
// and a relative path against root (or the directory of a `cd` in command position).
// A word still holding an unexpanded $VAR is skipped. A word counts only when it
// contains a "/", is no flag or URL, and either names an existing regular file that
// looks like a script or lies under root or home, or does not exist, looks like a
// script and stands in command position (first word, after a separator or `cd <dir> &&`,
// or right after an interpreter): that one is returned for missing-script, so an
// argument such as `> /tmp/out.log` never counts. An existing file wins over a missing one.
// The code after an interpreter's -c is a command line of its own: it is read the same
// way, from the directory of a `cd` before it, and never taken as a path itself.
func scriptPath(command, root, home string) string {
	found, missing := scan(command, root, root, home)
	if found != "" {
		return found
	}
	return missing
}

// scan is scriptPath on command with relative paths against base: the first existing
// script, and else the first missing one in command position.
func scan(command, base, root, home string) (found, missing string) {
	cd := false
	code := false   // the previous word was the -c of an interpreter
	target := false // the previous word was a redirection without its target
	cmdPos := true
	for _, raw := range words(command) {
		if code {
			code = false
			cmdPos = false
			f, m := scan(raw, base, root, home)
			if f != "" {
				return f, ""
			}
			if missing == "" {
				missing = m
			}
			continue
		}
		if target {
			target = false
			continue
		}
		if separators[raw] {
			cmdPos = true
			continue
		}
		if redirect.MatchString(raw) {
			// A redirection and its target are never the script, and keep the position.
			target = bareRedirect.MatchString(raw)
			continue
		}
		word := expand(raw, root, home)
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
			code = codeFlag(word)
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
				return word, ""
			}
		case err != nil && isCmd && missing == "" && looksLikeScript(word):
			missing = word
		}
	}
	return "", missing
}
