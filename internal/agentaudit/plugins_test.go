package agentaudit

import (
	"path/filepath"
	"strings"
	"testing"
)

// installed is an installed_plugins.json (version 2) with one entry per plugin key.
func installed(entries map[string]string) string {
	var parts []string
	for key, entry := range entries {
		parts = append(parts, `"`+key+`": [`+entry+`]`)
	}
	return `{"version": 2, "plugins": {` + strings.Join(parts, ", ") + `}}`
}

func userEntry(installPath string) string {
	return `{"scope": "user", "installPath": "` + installPath + `", "version": "1.0.0"}`
}

// pluginStatuses are the status rows of every plugin:* source.
func pluginStatuses(inv *Inventory) []string {
	var rows []string
	for _, row := range statusRows(inv) {
		if strings.HasPrefix(row, "plugin:") {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestPluginPrecedence(t *testing.T) {
	cases := []struct {
		name                  string
		user, project, local  string // enabledPlugins objects, "" for no settings file
		wantStatus, wantSkill []string
	}{
		{
			name: "user only", user: `{"p@m": true}`,
			wantStatus: []string{"plugin:p ok"},
			wantSkill:  []string{"claude plugin:p skill ps ~/plug/p/skills/ps/SKILL.md name=2 desc=3"},
		},
		{
			name: "project false over user true", user: `{"p@m": true}`, project: `{"p@m": false}`,
		},
		{
			name: "local true over project false", user: `{"p@m": false}`, project: `{"p@m": false}`, local: `{"p@m": true}`,
			wantStatus: []string{"plugin:p ok"},
			wantSkill:  []string{"claude plugin:p skill ps ~/plug/p/skills/ps/SKILL.md name=2 desc=3"},
		},
		{
			name: "local false over user true", user: `{"p@m": true}`, local: `{"p@m": false}`,
		},
		{
			name: "project true, not named elsewhere", project: `{"p@m": true}`,
			wantStatus: []string{"plugin:p ok"},
			wantSkill:  []string{"claude plugin:p skill ps ~/plug/p/skills/ps/SKILL.md name=2 desc=3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, configDir := hostless(t)
			root := tree(t, nil)
			files := map[string]string{
				".claude/plugins/installed_plugins.json": installed(map[string]string{"p@m": userEntry(filepath.Join(home, "plug", "p"))}),
				"plug/p/skills/ps/SKILL.md":              "---\nname: ps\ndescription: abc\n---\n",
			}
			if tc.user != "" {
				files[".claude/settings.json"] = `{"enabledPlugins": ` + tc.user + `}`
			}
			writeFiles(t, home, files)
			if tc.project != "" {
				writeFiles(t, root, map[string]string{".claude/settings.json": `{"enabledPlugins": ` + tc.project + `}`})
			}
			if tc.local != "" {
				writeFiles(t, root, map[string]string{".claude/settings.local.json": `{"enabledPlugins": ` + tc.local + `}`})
			}
			inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
			if err != nil {
				t.Fatal(err)
			}
			assertRows(t, "plugin statuses", pluginStatuses(inv), tc.wantStatus)
			assertRows(t, "skills", skillRows(inv), tc.wantSkill)
		})
	}
}

func TestPluginProjectScopeFromWorktree(t *testing.T) {
	home, configDir := hostless(t)
	mainRoot := t.TempDir()
	root := tree(t, map[string]string{".claude/settings.json": `{"enabledPlugins": {"p@m": true}}`})
	install := filepath.Join(home, "plug", "p")
	writeFiles(t, home, map[string]string{
		".claude/plugins/installed_plugins.json": installed(map[string]string{"p@m": `
  {"scope": "project", "projectPath": "/somewhere/else", "installPath": "/gone"},
  {"scope": "project", "projectPath": "` + mainRoot + `", "installPath": "` + install + `"}`}),
		"plug/p/skills/ps/SKILL.md": "---\nname: ps\ndescription: abc\n---\n",
		"plug/p/commands/go.md":     "---\ndescription: run it\n---\n",
		"plug/p/agents/rev.md":      "---\nname: reviewer\ndescription: ab\n---\n",
		"plug/p/hooks/guard.sh":     "#!/bin/sh\nexit 2\n",
		"plug/p/hooks/hooks.json": `{"description": "x", "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [
  {"type": "command", "command": "bash \"${CLAUDE_PLUGIN_ROOT}\"/hooks/guard.sh"},
  {"type": "command", "command": "$CLAUDE_PLUGIN_ROOT/hooks/gone.sh"}
]}]}}`,
		"plug/p/.mcp.json": `{"mcpServers": {"pm": {"command": "x"}}}`,
	})
	env := Env{Root: root, MainRoot: mainRoot, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home}
	before := treeHash(t, root, home)

	inv, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}

	if after := treeHash(t, root, home); after != before {
		t.Error("Collect changed the tree")
	}
	assertRows(t, "plugin statuses", pluginStatuses(inv), []string{"plugin:p ok"})
	assertRows(t, "skills", skillRows(inv), []string{
		"claude plugin:p skill ps ~/plug/p/skills/ps/SKILL.md name=2 desc=3",
		"claude plugin:p command go ~/plug/p/commands/go.md name=2 desc=6",
		"claude plugin:p agent reviewer ~/plug/p/agents/rev.md name=8 desc=2",
	})
	assertRows(t, "hooks", hookRows(inv), []string{
		`plugin:p PreToolUse "Bash" bash "${CLAUDE_PLUGIN_ROOT}"/hooks/guard.sh`,
		`plugin:p PreToolUse "Bash" $CLAUDE_PLUGIN_ROOT/hooks/gone.sh`,
	})
	if len(inv.Hooks) != 2 {
		t.Fatalf("want 2 hooks, got %d", len(inv.Hooks))
	}
	guard, gone := inv.Hooks[0], inv.Hooks[1]
	if guard.Script != filepath.Join(install, "hooks", "guard.sh") || !guard.Blocking || guard.File != "~/plug/p/hooks/hooks.json" {
		t.Errorf("guard hook: Script %q, Blocking %t, File %q; want %q, true, ~/plug/p/hooks/hooks.json",
			guard.Script, guard.Blocking, guard.File, filepath.Join(install, "hooks", "guard.sh"))
	}
	if gone.Script != filepath.Join(install, "hooks", "gone.sh") || gone.Unreadable == "" {
		t.Errorf("gone hook: Script %q, Unreadable %q; want %q and a reason", gone.Script, gone.Unreadable, filepath.Join(install, "hooks", "gone.sh"))
	}
	assertRows(t, "mcp", mcpRows(inv), []string{"plugin:p pm ~/plug/p/.mcp.json"})
	assertRows(t, "scripts", inv.Scripts, nil)
}

func TestPluginScopeMismatch(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{".claude/settings.json": `{"enabledPlugins": {"p@m": true}}`})
	writeFiles(t, home, map[string]string{
		".claude/plugins/installed_plugins.json": installed(map[string]string{"p@m": `{"scope": "local", "projectPath": "/other", "installPath": "` + home + `"}`}),
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "plugin statuses", pluginStatuses(inv), []string{"plugin:p COULD NOT CHECK — p@m is not installed for this project in ~/.claude/plugins/installed_plugins.json"})
}

func TestPluginNotInstalled(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/settings.json":     `{"enabledPlugins": {"q@m": true, "p@m": true}}`,
		".claude/skills/b/SKILL.md": "---\nname: bee\ndescription: ab\n---\n",
	})
	writeFiles(t, home, map[string]string{
		".claude/plugins/installed_plugins.json": installed(map[string]string{"p@m": userEntry(filepath.Join(home, "plug", "p"))}),
		"plug/p/skills/ps/SKILL.md":              "---\nname: ps\ndescription: abc\n---\n",
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "plugin statuses", pluginStatuses(inv), []string{
		"plugin:p ok",
		"plugin:q COULD NOT CHECK — q@m is not in ~/.claude/plugins/installed_plugins.json",
	})
	if got := status(inv, "project").State; got != "ok" {
		t.Errorf("project status %q; want ok", got)
	}
	assertRows(t, "skills", skillRows(inv), []string{
		"claude project skill bee .claude/skills/b/SKILL.md name=3 desc=2",
		"claude plugin:p skill ps ~/plug/p/skills/ps/SKILL.md name=2 desc=3",
	})
}

func TestPluginInstallProblems(t *testing.T) {
	cases := []struct {
		name, installedJSON, want string
	}{
		{"no installed_plugins.json", "", "plugin:p COULD NOT CHECK — p@m is not in ~/.claude/plugins/installed_plugins.json"},
		{"invalid JSON", "{x", "plugin:p COULD NOT CHECK — ~/.claude/plugins/installed_plugins.json: not valid JSON"},
		{"plugins not an object", `{"plugins": []}`, `plugin:p COULD NOT CHECK — ~/.claude/plugins/installed_plugins.json: "plugins" is not an object`},
		{"no installPath", installed(map[string]string{"p@m": `{"scope": "user"}`}), "plugin:p COULD NOT CHECK — p@m has no installPath"},
		{"installPath gone", installed(map[string]string{"p@m": userEntry("/nonexistent/p")}), "plugin:p COULD NOT CHECK — installPath /nonexistent/p does not exist"},
		{"version 1 object entry", `{"version": 1, "plugins": {"p@m": {"installPath": "/nonexistent/p"}}}`, "plugin:p COULD NOT CHECK — installPath /nonexistent/p does not exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, configDir := hostless(t)
			root := tree(t, nil)
			files := map[string]string{".claude/settings.json": `{"enabledPlugins": {"p@m": true}}`}
			if tc.installedJSON != "" {
				files[".claude/plugins/installed_plugins.json"] = tc.installedJSON
			}
			writeFiles(t, home, files)
			inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
			if err != nil {
				t.Fatal(err)
			}
			got := pluginStatuses(inv)
			if len(got) != 1 || !strings.HasPrefix(got[0], tc.want) {
				t.Errorf("plugin statuses %q; want one starting with %q", got, tc.want)
			}
		})
	}
}

func TestPluginEnabledNotBoolean(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{".claude/settings.json": `{"enabledPlugins": {"p@m": "yes"}}`})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "project status", []string{statusRows(&Inventory{Statuses: []Status{status(inv, "project")}})[0]},
		[]string{`project COULD NOT CHECK — .claude/settings.json: enabledPlugins["p@m"] is not a boolean`})
	assertRows(t, "plugin statuses", pluginStatuses(inv), nil)
}

func TestManagedSettingsNotRead(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, nil)
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home})
	if err != nil {
		t.Fatal(err)
	}
	want := Status{Source: "managed", State: "COULD NOT CHECK", Reason: "managed settings are not read"}
	if got := status(inv, SourceManaged); got != want {
		t.Errorf("managed status %+v; want %+v", got, want)
	}
}

func TestPluginsRepoOnly(t *testing.T) {
	home, configDir := hostless(t)
	root := tree(t, map[string]string{".claude/settings.json": `{"enabledPlugins": {"p@m": true}}`})
	writeFiles(t, home, map[string]string{
		".claude/plugins/installed_plugins.json": installed(map[string]string{"p@m": userEntry(filepath.Join(home, "plug", "p"))}),
		"plug/p/skills/ps/SKILL.md":              "---\nname: ps\ndescription: abc\n---\n",
	})
	inv, err := Collect(Env{Root: root, MainRoot: root, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Home: home, RepoOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	assertRows(t, "statuses", statusRows(inv), []string{
		"lighthouse afwezig", "project ok", "lokaal afwezig", "gebruiker overgeslagen (--repo-only)",
		"plugin:* overgeslagen (--repo-only)", "managed overgeslagen (--repo-only)",
	})
	assertRows(t, "skills", skillRows(inv), nil)
}
