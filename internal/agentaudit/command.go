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

// scriptPath is the script a hook command runs: the first word that, with
// $CLAUDE_PROJECT_DIR replaced by root, ~ by home, quotes dropped and a relative path
// read against root, names an existing regular file. "" means an inline command.
func scriptPath(command, root, home string) string {
	for _, v := range []string{"${CLAUDE_PROJECT_DIR}", "$CLAUDE_PROJECT_DIR"} {
		command = strings.ReplaceAll(command, v, root)
	}
	for _, word := range strings.Fields(command) {
		word = unquote.Replace(word)
		if word == "" || interpreters[filepath.Base(word)] {
			continue
		}
		switch {
		case strings.HasPrefix(word, "~/"):
			if home == "" {
				continue
			}
			word = filepath.Join(home, word[2:])
		case !filepath.IsAbs(word):
			word = filepath.Join(root, word)
		}
		if info, err := os.Stat(word); err == nil && info.Mode().IsRegular() {
			return word
		}
	}
	return ""
}
