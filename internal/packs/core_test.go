package packs

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/pack"
)

func TestCorePack(t *testing.T) {
	core, err := fs.Sub(pack.Files, "core")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Load(core, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	kinds := map[Kind]int{}
	for _, u := range p.Units {
		kinds[u.Kind]++
		if u.Kind == KindFile {
			files = append(files, u.Path)
		}
	}
	sort.Strings(files)
	want := []string{
		".agents/skills/lighthouse-architecture-scan/SKILL.md",
		".agents/skills/lighthouse-audit-agent-config/SKILL.md",
		".agents/skills/lighthouse-audit-worktrees/SKILL.md",
		".agents/skills/lighthouse-close-issue/SKILL.md",
		".agents/skills/lighthouse-fix-pr/SKILL.md",
		".agents/skills/lighthouse-forge/SKILL.md",
		".agents/skills/lighthouse-forge/github.md",
		".agents/skills/lighthouse-forge/gitlab.md",
		".agents/skills/lighthouse-map-planned/SKILL.md",
		".agents/skills/lighthouse-place-instruction/SKILL.md",
		".agents/skills/lighthouse-review-plan/SKILL.md",
		".agents/skills/lighthouse-review-plan/findings.schema.json",
		".agents/skills/lighthouse-review-plan/reviewer-prompt.md",
		".agents/skills/lighthouse-review-plan/verification.schema.json",
		".agents/skills/lighthouse-review-plan/verifier-prompt.md",
		".agents/skills/lighthouse-review-spec/SKILL.md",
		".agents/skills/lighthouse-review-spec/findings.schema.json",
		".agents/skills/lighthouse-review-spec/reviewer-prompt.md",
		".agents/skills/lighthouse-review-spec/verification.schema.json",
		".agents/skills/lighthouse-review-spec/verifier-prompt.md",
		".agents/skills/lighthouse-route-issue/SKILL.md",
		".agents/skills/lighthouse-start-issue/SKILL.md",
		".agents/skills/lighthouse-test-standard/SKILL.md",
		".agents/skills/lighthouse-upgrade/SKILL.md",
		".agents/skills/lighthouse-write-handoff/SKILL.md",
		".agents/skills/lighthouse-write-issue/SKILL.md",
		".agents/skills/lighthouse-write-pr/SKILL.md",
		".claude/agents/lighthouse-issue-impact-scout.md",
		".claude/hooks/lighthouse/blocking",
		".claude/hooks/lighthouse/run.sh",
		".claude/skills/lighthouse-architecture-scan/SKILL.md",
		".claude/skills/lighthouse-audit-agent-config/SKILL.md",
		".claude/skills/lighthouse-audit-worktrees/SKILL.md",
		".claude/skills/lighthouse-close-issue/SKILL.md",
		".claude/skills/lighthouse-fix-pr/SKILL.md",
		".claude/skills/lighthouse-forge/SKILL.md",
		".claude/skills/lighthouse-forge/github.md",
		".claude/skills/lighthouse-forge/gitlab.md",
		".claude/skills/lighthouse-map-planned/SKILL.md",
		".claude/skills/lighthouse-place-instruction/SKILL.md",
		".claude/skills/lighthouse-review-plan/SKILL.md",
		".claude/skills/lighthouse-review-plan/findings.schema.json",
		".claude/skills/lighthouse-review-plan/reviewer-prompt.md",
		".claude/skills/lighthouse-review-plan/verification.schema.json",
		".claude/skills/lighthouse-review-plan/verifier-prompt.md",
		".claude/skills/lighthouse-review-spec/SKILL.md",
		".claude/skills/lighthouse-review-spec/findings.schema.json",
		".claude/skills/lighthouse-review-spec/reviewer-prompt.md",
		".claude/skills/lighthouse-review-spec/verification.schema.json",
		".claude/skills/lighthouse-review-spec/verifier-prompt.md",
		".claude/skills/lighthouse-route-issue/SKILL.md",
		".claude/skills/lighthouse-start-issue/SKILL.md",
		".claude/skills/lighthouse-test-standard/SKILL.md",
		".claude/skills/lighthouse-upgrade/SKILL.md",
		".claude/skills/lighthouse-write-handoff/SKILL.md",
		".claude/skills/lighthouse-write-issue/SKILL.md",
		".claude/skills/lighthouse-write-pr/SKILL.md",
		".github/pull_request_template.md",
		".github/workflows/lighthouse-unblock-dependents.yml",
		".lighthouse/headings.toml",
	}
	if strings.Join(files, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s", strings.Join(files, "\n"))
	}
	if kinds[KindBlock] != 1 || kinds[KindHook] != 20 {
		t.Fatalf("kinds = %v", kinds)
	}
	var tools []string
	for _, r := range p.Requires {
		tools = append(tools, r.Tool)
	}
	if strings.Join(tools, ",") != "codex,gh,git,jq,superpowers" {
		t.Fatalf("tools = %v", tools)
	}
}

// TestLanguagePacks loads each language package from the binary and pins its units.
// Each package PR of M0 (c) adds its row.
func TestLanguagePacks(t *testing.T) {
	cases := []struct {
		name     string
		files    []string
		detect   bool
		tools    string
		blocks   int
		hooks    int
		fragment string
	}{
		{
			name: "expo",
			files: []string{
				".agents/skills/lighthouse-refresh-expo-lockfiles/SKILL.md",
				".claude/skills/lighthouse-refresh-expo-lockfiles/SKILL.md",
			},
			detect: true,
			tools:  "git,superpowers",
		},
		{
			name: "go",
			files: []string{
				".agents/skills/lighthouse-go-standard/SKILL.md",
				".claude/hooks/lighthouse/go-imports-gate.sh",
				".claude/hooks/lighthouse/go-lint-gate.sh",
				".claude/hooks/lighthouse/gofmt-check.sh",
				".claude/skills/lighthouse-go-standard/SKILL.md",
			},
			detect: true,
			tools:  "git,go,gofmt,golangci-lint,jq",
			blocks: 1,
			hooks:  3,
		},
		{
			name: "node",
			files: []string{
				".claude/hooks/lighthouse/node-imports-gate.sh",
			},
			detect: true,
			tools:  "node,npm,npx",
			blocks: 1,
			hooks:  1,
		},
		{
			name: "python",
			files: []string{
				".agents/skills/lighthouse-python-standard/SKILL.md",
				".claude/hooks/lighthouse/python-imports-gate.sh",
				".claude/hooks/lighthouse/ruff-format-check.sh",
				".claude/hooks/lighthouse/ty-gate.sh",
				".claude/skills/lighthouse-python-standard/SKILL.md",
				".lighthouse/scripts/gate_python.py",
				".lighthouse/scripts/quality/__init__.py",
				".lighthouse/scripts/quality/baseline.py",
				".lighthouse/scripts/quality/diffcov.py",
				".lighthouse/scripts/quality/gitenv.py",
				".lighthouse/scripts/quality/prcov.py",
				".lighthouse/scripts/quality/prcov_issues.py",
				".lighthouse/scripts/quality/ratchet.py",
			},
			detect:   true,
			tools:    "gh,jq,python3,tomllib,uv",
			blocks:   1,
			hooks:    3,
			fragment: "test_gate",
		},
		{
			name: "shell",
			files: []string{
				".agents/skills/lighthouse-shell-standard/SKILL.md",
				".claude/hooks/lighthouse/shell-lint-check.sh",
				".claude/skills/lighthouse-shell-standard/SKILL.md",
				".lighthouse/scripts/lib/shellcheck-bin.sh",
				".lighthouse/scripts/shellcheck-repo.sh",
			},
			detect: true,
			tools:  "curl,git,jq,tar,xz",
			blocks: 0,
			hooks:  1,
		},
		{
			name: "powershell",
			files: []string{
				".claude/hooks/lighthouse/powershell-lint-check.sh",
				".lighthouse/scripts/psscriptanalyzer-repo.ps1",
			},
			detect: true,
			tools:  "PSScriptAnalyzer,git,jq,pwsh",
			blocks: 0,
			hooks:  1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sub, err := fs.Sub(pack.Files, c.name)
			if err != nil {
				t.Fatal(err)
			}
			p, err := Load(sub, c.name, "dev")
			if err != nil {
				t.Fatal(err)
			}
			var files []string
			kinds := map[Kind]int{}
			for _, u := range p.Units {
				kinds[u.Kind]++
				if u.Kind == KindFile {
					files = append(files, u.Path)
				}
			}
			if kinds[KindBlock] != c.blocks || kinds[KindHook] != c.hooks || kinds[KindPermission] != 0 {
				t.Fatalf("kinds = %v, want %d block(s) and %d hook(s)", kinds, c.blocks, c.hooks)
			}
			sort.Strings(files)
			if strings.Join(files, "\n") != strings.Join(c.files, "\n") {
				t.Fatalf("files:\n%s", strings.Join(files, "\n"))
			}
			var tables []string
			for _, tb := range p.Config {
				tables = append(tables, tb.Name)
			}
			if strings.Join(tables, ",") != c.fragment {
				t.Fatalf("fragment tables = %v, want %q", tables, c.fragment)
			}
			if (p.Detect != nil) != c.detect {
				t.Fatalf("detect = %v, want %v", p.Detect, c.detect)
			}
			var tools []string
			for _, r := range p.Requires {
				tools = append(tools, r.Tool)
			}
			if strings.Join(tools, ",") != c.tools {
				t.Fatalf("tools = %v", tools)
			}
		})
	}
}
