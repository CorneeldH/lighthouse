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
		{"home var", "$HOME/.claude/hooks/b.js", plain, home + "/.claude/hooks/b.js"},
		{"braced home var quoted", `"${HOME}/.claude/hooks/b.js"`, plain, home + "/.claude/hooks/b.js"},
		{"plugin root var skipped", `"$CLAUDE_PLUGIN_ROOT/hooks/x.sh"`, plain, ""},
		{"braced plugin root skipped", "bash ${CLAUDE_PLUGIN_ROOT}/hooks/x.sh", plain, ""},
		{"redirect target is no script", "echo x > /tmp/out.log", plain, ""},
		{"argument file is no script", "jq . /tmp/in.json", plain, ""},
		{"mkdir argument is no script", "mkdir -p $CLAUDE_PROJECT_DIR/out/x.d", plain, ""},
		{"missing after semicolon", "echo a ; ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"missing after pipe", "echo a | ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"missing after interpreter flag", "bash -e ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"cd only in command position", "echo cd x && ./y.sh", plain, plain + "/y.sh"},
		{"directory is not a script", "ls x/", plain, ""},
		{"-c code sourcing a script", `bash -c 'source .claude/hooks/a.sh && check'`, plain, plain + "/.claude/hooks/a.sh"},
		{"-c code sourcing a missing file", `bash -c 'source .claude/hooks/gone.sh && check'`, plain, ""},
		{"-c code with cd", `sh -c "cd x && ./y.sh"`, plain, plain + "/x/y.sh"},
		{"-c code running a missing script", `sh -c "./gone/z.sh --fast"`, plain, plain + "/gone/z.sh"},
		{"-c code without a path", `bash -c 'echo hi && exit 2'`, plain, ""},
		{"combined -lc with quoted var", `bash -lc '"$CLAUDE_PROJECT_DIR"/.claude/hooks/a.sh arg'`, plain, plain + "/.claude/hooks/a.sh"},
		{"combined -ec with cd", `bash -ec 'cd "$CLAUDE_PROJECT_DIR" && ./.claude/hooks/a.sh'`, plain, plain + "/.claude/hooks/a.sh"},
		{"combined -euc missing script", `bash -euc './gone/z.sh --fast'`, plain, plain + "/gone/z.sh"},
		{"combined -xc code sourcing a missing file", `sh -xc 'source .claude/hooks/gone.sh && check'`, plain, ""},
		{"semicolon glued to script", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/a.sh; echo done`, plain, plain + "/.claude/hooks/a.sh"},
		{"and glued on both sides", "true&&./x/y.sh", plain, plain + "/x/y.sh"},
		{"or glued to script", "./x/y.sh||exit 0", plain, plain + "/x/y.sh"},
		{"pipe glued starts a command", "echo a|./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"background glued starts a command", "true&./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"background ampersand starts a command", "true & ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"quoted semicolon stays in the word", `echo 'a;./gone/z.sh'`, plain, ""},
		{"var starting with HOME skipped", "$HOMEBREW_PREFIX/bin/guard.sh", plain, ""},
		{"var starting with project dir skipped", "$CLAUDE_PROJECT_DIRX/.claude/hooks/a.sh", plain, ""},
		{"var with underscore suffix skipped", "bash $CLAUDE_PROJECT_DIR_OLD/x/y.sh", plain, ""},
		{"project dir var at end of cd", `cd $CLAUDE_PROJECT_DIR&&./x/y.sh`, plain, plain + "/x/y.sh"},
		{"cd carried into -c code", `cd x && bash -c './y.sh arg'`, plain, plain + "/x/y.sh"},
		{"cd inside -c code from a cd", `cd x && sh -c 'cd .. && ./x/y.sh'`, plain, plain + "/x/y.sh"},
		{"existing after -c code wins over its missing", `bash -c './gone/z.sh --fast' && ./x/y.sh`, plain, plain + "/x/y.sh"},
		{"-c code of one quoted word", `bash -c '"$CLAUDE_PROJECT_DIR"/x/y.sh'`, plain, plain + "/x/y.sh"},
		{"&> redirect target is no script", "jq -r .x &>/tmp/hook.log", plain, ""},
		{"&>> redirect target is no script", "jq -r .x &>>/tmp/x.log", plain, ""},
		{"spaced &> redirect target is no script", "jq -r .x &> /tmp/hook.sh", plain, ""},
		{">& redirect target is no script", "jq -r .x >&/tmp/hook.sh", plain, ""},
		{"redirect after separator is no script", "true; >/tmp/hook.sh", plain, ""},
		{"spaced redirect after separator is no script", "true; > /tmp/hook.sh", plain, ""},
		{"input redirect is no script", "./x/y.sh </tmp/in.sh", plain, plain + "/x/y.sh"},
		{"2>&1 then pipe to a script", "cmd 2>&1 | ./x/y.sh", plain, plain + "/x/y.sh"},
		{"2>&1 then pipe to a missing script", "cmd 2>&1 | ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{">&2 then semicolon", "echo no >&2; ./gone/z.sh", plain, plain + "/gone/z.sh"},
		{"redirect glued to a script", "./gone/z.sh>/tmp/log.sh", plain, plain + "/gone/z.sh"},
		{"&> glued to a word", "jq .x&>/tmp/h.sh", plain, ""},
		{"python -c code is opaque", `python3 -c "import json,sys; d=json.load(open('/tmp/x.json'))"`, plain, ""},
		{"python -c code then a script", `python3 -c "import os; os.system('./gone/z.sh')" && ./x/y.sh`, plain, plain + "/x/y.sh"},
		{"node -e code is opaque", `node -e "require('/tmp/gone.js')"`, plain, ""},
		{"node --eval code is opaque", `node --eval "require('./gone/z.js')"`, plain, ""},
		{"perl -e code is opaque", `perl -e 'exec "./gone/z.sh"'`, plain, ""},
		{"ruby -e code with a plain path is opaque", `ruby -e 'puts "lib/x.rb"'`, plain, ""},
		{"python -E keeps the script", "python3 -E ./gone/z.py", plain, plain + "/gone/z.py"},
		{"env bash -c is shell code", `env bash -c './gone/z.sh'`, plain, plain + "/gone/z.sh"},
		{"assignment with slash is no script", "FOO=bar/baz.sh; echo", plain, ""},
		{"call with slash is no script", "f(a/b.sh)", plain, ""},
		{"assignment keeps the script position", "FOO=1 ./gone/z.sh", plain, plain + "/gone/z.sh"},
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
	for _, command := range []string{"node ~/.claude/hooks/b.js", "$HOME/.claude/hooks/b.js"} {
		if got := scriptPath(command, root, ""); got != "" {
			t.Errorf("scriptPath(%q) = %q, want \"\" without a home directory", command, got)
		}
	}
}

func TestEvalFlag(t *testing.T) {
	tests := map[string]bool{
		"-c": true, "-e": true, "-p": true, "-Sc": true, "--eval": true, "--print": true,
		"-E": false, "-m": false, "-u": false, "--evaluate": false, "-e1": false, "-": false,
	}
	for word, want := range tests {
		if got := evalFlag(word); got != want {
			t.Errorf("evalFlag(%q) = %v, want %v", word, got, want)
		}
	}
}

func TestCodeFlag(t *testing.T) {
	tests := map[string]bool{
		"-c": true, "-lc": true, "-ec": true, "-euc": true, "-xc": true, "-cl": true,
		"-e": false, "-l": false, "-": false, "--rcfile": false, "--c": false, "-c1": false, "c": false,
	}
	for word, want := range tests {
		if got := codeFlag(word); got != want {
			t.Errorf("codeFlag(%q) = %v, want %v", word, got, want)
		}
	}
}

// FuzzScriptPath: no command line crashes scriptPath, and what it returns is "" or an
// absolute path.
func FuzzScriptPath(f *testing.F) {
	for _, seed := range []string{
		`bash -lc '"$CLAUDE_PROJECT_DIR"/x.sh arg'`, `cd "a b" && sh -c 'cd .. && ./y.sh'`,
		`a&&./b.sh;|&`, `"unbalanced`, `bash -c`, `-c`, `$HOMEBREW/x.sh`, `cd`, `~/`,
	} {
		f.Add(seed)
	}
	root := f.TempDir()
	f.Fuzz(func(t *testing.T, command string) {
		if got := scriptPath(command, root, "/home/u"); got != "" && !filepath.IsAbs(got) {
			t.Errorf("scriptPath(%q) = %q, not absolute", command, got)
		}
	})
}
