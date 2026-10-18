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
		".claude/hooks/lighthouse/main-commit-guard.sh",
		".github/workflows/lighthouse-unblock-dependents.yml",
		".lighthouse/headings.toml",
		".lighthouse/lib/lighthouse_lib/__init__.py",
		".lighthouse/lib/lighthouse_lib/artefacts.py",
		".lighthouse/lib/lighthouse_lib/config.py",
		".lighthouse/lib/lighthouse_lib/datadir.py",
		".lighthouse/lib/lighthouse_lib/git_invocations.py",
		".lighthouse/lib/lighthouse_lib/headings.py",
		".lighthouse/lib/lighthouse_lib/protocol.py",
		".lighthouse/scripts/board_start.py",
		".lighthouse/scripts/codex_usage.py",
		".lighthouse/scripts/measure_fix_pr_kpi.py",
		".lighthouse/scripts/measure_start_issue_kpi.py",
		".lighthouse/scripts/plan_review_record.py",
		".lighthouse/scripts/post_artefact.py",
		".lighthouse/scripts/review_pair.py",
		".lighthouse/scripts/unblock_dependents.py",
		".lighthouse/scripts/worktree_audit.py",
	}
	if strings.Join(files, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s", strings.Join(files, "\n"))
	}
	if kinds[KindBlock] != 1 || kinds[KindHook] != 1 {
		t.Fatalf("kinds = %v", kinds)
	}
	var tools []string
	for _, r := range p.Requires {
		tools = append(tools, r.Tool)
	}
	if strings.Join(tools, ",") != "codex,gh,git,jq,python3,tomllib" {
		t.Fatalf("tools = %v", tools)
	}
}
