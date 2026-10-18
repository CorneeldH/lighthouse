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
		".lighthouse/headings.toml",
		".lighthouse/lib/lighthouse_lib/__init__.py",
		".lighthouse/lib/lighthouse_lib/bash_writes.py",
		".lighthouse/lib/lighthouse_lib/config.py",
		".lighthouse/lib/lighthouse_lib/datadir.py",
		".lighthouse/lib/lighthouse_lib/deadline.py",
		".lighthouse/lib/lighthouse_lib/evidence.py",
		".lighthouse/lib/lighthouse_lib/gate_slot.py",
		".lighthouse/lib/lighthouse_lib/git_invocations.py",
		".lighthouse/lib/lighthouse_lib/headings.py",
		".lighthouse/lib/lighthouse_lib/md_links.py",
		".lighthouse/lib/lighthouse_lib/no_verify.py",
		".lighthouse/lib/lighthouse_lib/pr_body_gate.py",
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
	if strings.Join(tools, ",") != "gh,git,jq,python3,tomllib" {
		t.Fatalf("tools = %v", tools)
	}
}
