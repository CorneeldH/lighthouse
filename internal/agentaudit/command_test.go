package agentaudit

import (
	"path/filepath"
	"testing"
)

func TestScriptPath(t *testing.T) {
	files := map[string]string{
		".claude/hooks/a.sh": "#!/bin/sh\n",
		"x/y.sh":             "#!/bin/sh\n",
		"bin/guard":          "#!/bin/sh\n",
	}
	spaced := tree(t, files)
	spaced = filepath.Join(spaced, "my project")
	writeFiles(t, spaced, files)
	plain := tree(t, files)
	home := tree(t, map[string]string{".claude/hooks/b.js": "//\n"})

	tests := []struct {
		name, command, root, want string
	}{
		{"env var", "$CLAUDE_PROJECT_DIR/.claude/hooks/a.sh", plain, plain + "/.claude/hooks/a.sh"},
		{"braced env var", "${CLAUDE_PROJECT_DIR}/.claude/hooks/a.sh", plain, plain + "/.claude/hooks/a.sh"},
		{"interpreter and quoted var", `bash "$CLAUDE_PROJECT_DIR"/.claude/hooks/a.sh`, plain, plain + "/.claude/hooks/a.sh"},
		{"quoted var, root with space", `bash "$CLAUDE_PROJECT_DIR"/.claude/hooks/a.sh`, spaced, spaced + "/.claude/hooks/a.sh"},
		{"fully quoted, root with space", `"$CLAUDE_PROJECT_DIR/.claude/hooks/a.sh" --flag`, spaced, spaced + "/.claude/hooks/a.sh"},
		{"single quoted", `sh '$CLAUDE_PROJECT_DIR/x/y.sh'`, plain, plain + "/x/y.sh"},
		{"tilde", "node ~/.claude/hooks/b.js", plain, home + "/.claude/hooks/b.js"},
		{"cd and relative script", "cd x && ./y.sh", plain, plain + "/x/y.sh"},
		{"extensionless script in root", "./bin/guard", plain, plain + "/bin/guard"},
		{"inline command", "echo hi", plain, ""},
		{"empty", "", plain, ""},
		{"only quotes", `"" ''`, plain, ""},
		{"unbalanced quote", `bash "$CLAUDE_PROJECT_DIR/x/y.sh`, plain, plain + "/x/y.sh"},
		{"real binary is not the script", "/usr/bin/jq .x", plain, ""},
		{"real binary then script", "/bin/sh -c ./x/y.sh", plain, plain + "/x/y.sh"},
		{"missing script with extension", "bash $CLAUDE_PROJECT_DIR/hooks/gone.sh", plain, plain + "/hooks/gone.sh"},
		{"missing script under .claude", "$CLAUDE_PROJECT_DIR/.claude/hooks/gone", plain, plain + "/.claude/hooks/gone"},
		{"missing without extension elsewhere", "$CLAUDE_PROJECT_DIR/hooks/gone", plain, ""},
		{"missing tilde script", "~/.claude/hooks/gone.js", plain, home + "/.claude/hooks/gone.js"},
		{"existing wins over earlier missing", "./nope/z.sh && ./x/y.sh", plain, plain + "/x/y.sh"},
		{"first missing wins", "./a/one.sh && ./b/two.sh", plain, plain + "/a/one.sh"},
		{"flag with path is not a script", "jq --config=/etc/jq/conf.json .", plain, ""},
		{"url is not a script", "curl https://example.com/a/b.html", plain, ""},
		{"directory is not a script", "ls x/", plain, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scriptPath(tc.command, tc.root, home); got != tc.want {
				t.Errorf("scriptPath(%q) = %q, want %q", tc.command, got, tc.want)
			}
		})
	}
}

func TestScriptPathWithoutHome(t *testing.T) {
	root := tree(t, nil)
	if got := scriptPath("node ~/.claude/hooks/b.js", root, ""); got != "" {
		t.Errorf("got %q, want \"\" without a home directory", got)
	}
}
