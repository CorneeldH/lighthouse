package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// agentAuditTree writes files (slash paths) under a new repository root, points
// LIGHTHOUSE_REPO_ROOT at it and HOME and CLAUDE_CONFIG_DIR at a temporary home, and
// returns the root.
func agentAuditTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("LIGHTHOUSE_REPO_ROOT", root)
	t.Setenv("LIGHTHOUSE_TOML", filepath.Join(root, "lighthouse.toml"))
	return root
}

func runAgentAudit(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	top := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	top.AddCommand(newAgentAuditCommand())
	top.SetOut(&out)
	top.SetErr(&errOut)
	top.SetArgs(append([]string{"agent-audit"}, args...))
	code = execute(top, &errOut)
	return code, out.String(), errOut.String()
}

func TestAgentAuditNoRepoRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
	t.Setenv("LIGHTHOUSE_TOML", "")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	t.Setenv("HOME", t.TempDir())
	t.Chdir(dir)
	code, out, errOut := runAgentAudit(t)
	if code != 2 || out != "" {
		t.Errorf("exit %d, stdout %q; want 2 and no stdout", code, out)
	}
	if !strings.HasPrefix(errOut, "agent-audit: COULD NOT CHECK — ") {
		t.Errorf("stderr = %q, want agent-audit: COULD NOT CHECK — …", errOut)
	}
}

func TestAgentAuditBrokenConfig(t *testing.T) {
	agentAuditTree(t, map[string]string{"lighthouse.toml": "[agent_audit]\ntop = \"ten\"\n"})
	code, out, errOut := runAgentAudit(t)
	if code != 2 || out != "" {
		t.Errorf("exit %d, stdout %q; want 2 and no stdout", code, out)
	}
	if !strings.HasPrefix(errOut, "agent-audit: COULD NOT CHECK — ") || !strings.Contains(errOut, "agent_audit.top") {
		t.Errorf("stderr = %q, want the COULD NOT CHECK line naming agent_audit.top", errOut)
	}
}

const auditOldKey = "unregistered-script:project/.claude/hooks/oud.sh"

func TestAgentAuditFlags(t *testing.T) {
	repo := map[string]string{
		".claude/settings.json":     `{}`,
		".claude/hooks/oud.sh":      "#!/bin/sh\nexit 0\n",
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: aaaa\n---\n",
		".claude/skills/b/SKILL.md": "---\nname: b\ndescription: bb\n---\n",
	}
	tests := []struct {
		name     string
		toml     string
		args     []string
		want     int
		has, not []string
	}{
		{name: "default exit 0 with a finding", args: nil, want: 0,
			has: []string{"[" + auditOldKey + "]", "Inventaris", "  gebruiker   afwezig"}},
		{name: "strict with a finding", args: []string{"--repo-only", "--strict"}, want: 1,
			has: []string{"  gebruiker   overgeslagen (--repo-only)"}},
		{name: "ignore from lighthouse.toml", toml: "[agent_audit]\nignore = [\"" + auditOldKey + "\"]\n",
			args: []string{"--repo-only", "--strict"}, want: 0, not: []string{auditOldKey}},
		{name: "top and description_max_chars from lighthouse.toml", toml: "[agent_audit]\ntop = 1\ndescription_max_chars = 3\n",
			args: []string{"--repo-only", "--only", "surface"}, want: 0,
			has: []string{"  Grootste posten\n  project  a  5 tekens\n  Description langer dan 3 tekens\n  project  a  4 tekens\n"}},
		{name: "only repeated", args: []string{"--repo-only", "--only", "surface", "--only", "conflicts"}, want: 0,
			has: []string{"Conflicten (1)", "Oppervlak"}, not: []string{"Overlap", "Inventaris"}},
		{name: "only unknown part", args: []string{"--only", "hooks"}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{}
			for k, v := range repo {
				files[k] = v
			}
			if tt.toml != "" {
				files["lighthouse.toml"] = tt.toml
			}
			agentAuditTree(t, files)
			code, out, errOut := runAgentAudit(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit %d, want %d\nstdout %s\nstderr %s", code, tt.want, out, errOut)
			}
			for _, s := range tt.has {
				if !strings.Contains(out, s) {
					t.Errorf("stdout lacks %q:\n%s", s, out)
				}
			}
			for _, s := range tt.not {
				if strings.Contains(out, s) {
					t.Errorf("stdout has %q:\n%s", s, out)
				}
			}
		})
	}
}

// TestAgentAuditKnowsHookNames: the command fills Env.HookNames from the hook register, so
// run.sh with a known hook is no missing-script and one with an unknown name is.
func TestAgentAuditKnowsHookNames(t *testing.T) {
	agentAuditTree(t, map[string]string{
		".claude/settings.json": `{"hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [` +
			`{"type": "command", "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh main-commit-gate"},` +
			`{"type": "command", "command": "\"$CLAUDE_PROJECT_DIR\"/.claude/hooks/lighthouse/run.sh onbekend"}]}]}}`,
		".claude/hooks/lighthouse/run.sh": "#!/bin/sh\nexec lighthouse hook \"$1\"\n",
	})
	_, out, _ := runAgentAudit(t, "--repo-only", "--only", "conflicts")
	want := `Conflicten (1)
  [missing-script:lighthouse/"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh onbekend]
`
	if !strings.Contains(out, want) {
		t.Errorf("stdout lacks %q:\n%s", want, out)
	}
}
