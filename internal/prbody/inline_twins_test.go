//go:build !windows

package prbody

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The twins of the inline tests of tests/pack/test_pr_body_gate.py that had no Go counterpart
// yet, and the jq-filter parity of tests/pack/test_close_issue_skill.py.

// twin of test_repo_comes_from_lighthouse_toml
func TestRepoComesFromLighthouseToml(t *testing.T) {
	withConfig(t, "Other/repo")
	repo, err := lazyRepo()()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "repo", repo, Repo{Kind: "github", Slug: "Other/repo"})
	eq(t, "own repo", ClosesTargets("Closes Other/repo#12", repo), []int{12})
	eq(t, "other repo", len(ClosesTargets("Closes Acme/widgets#12", repo)), 0)
}

// twin of test_without_forge_repo
func TestWithoutForgeRepo(t *testing.T) {
	withConfig(t, "")
	repo, err := lazyRepo()()
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "repo", repo, Repo{Kind: "github"})
	eq(t, "closes", ClosesTargets("Closes #4 and Closes x/y#5", repo), []int{4})
	eq(t, "fetch", fetch(t, context.Background(), repo.Slug, 4), fetched{0, false, "no forge.repo in lighthouse.toml"})
	dir := t.TempDir()
	eq(t, "scope", decision(t, pcAt(dir, "x/y"), dir, repo.Slug), "check")
}

// twin of test_ac_heading_any_language
func TestACHeadingAnyLanguage(t *testing.T) {
	withConfig(t, "Acme/widgets") // an English repo
	for _, heading := range []string{"### Acceptatiecriteria", "### Acceptance criteria"} {
		n, ok := CountACs(heading + "\n- [ ] a\n- [x] b\n### Next\n- [ ] c")
		eq(t, heading, [2]any{n, ok}, [2]any{2, true})
	}
}

var jqRefsTest = regexp.MustCompile(`test\("([^"]*Refs[^"]*)"; "i"\)`)

// jqStripComments is the comment filter both jq filters of close-issue apply first.
const jqStripComments = `gsub("<!--[\\s\\S]*?-->"; "")`

// skillSection is the text from heading up to the next line that starts with next.
func skillSection(t *testing.T, body, heading, next string) string {
	t.Helper()
	i := strings.Index(body, heading)
	if i < 0 {
		t.Fatalf("no %q in the skill", heading)
	}
	s := body[i:]
	if j := strings.Index(s[1:], next); j >= 0 {
		return s[:j+1]
	}
	return s
}

// TestRefsTargetsMatchesCloseIssueJqFilter runs the `Refs` test of both jq filters of close-issue
// (Step 0 and B2a) with jq: a PR the filter finds is a PR whose body RefsTargets gives #147.
// It replaces test_the_refs_filters_find_what_the_gate_parser_finds, which matched a Python
// regex taken from the filter against pr_body_gate.refs_targets.
func TestRefsTargetsMatchesCloseIssueJqFilter(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is missing")
	}
	template, err := os.ReadFile("../../pack/core/templates/pull_request_template.md")
	if err != nil {
		t.Fatal(err)
	}
	bodies := []string{
		strings.ReplaceAll(string(template), "#123", "#147"),
		"refs #147 — lowercase reason",
		"Refs\t#147: reason after a tab",
		"Refs #1470 — another issue",
		"Refs #147_x — not this number",
		"see a/Refs #147 in a path",
		"xRefs #147 glued to a word",
		"<!-- Refs #147 — only in a comment -->\nNo issue: nothing here at all",
	}
	input, err := json.Marshal(bodies)
	if err != nil {
		t.Fatal(err)
	}
	var want []bool
	for _, body := range bodies {
		found := false
		for _, nr := range RefsTargets(body) {
			found = found || nr == 147
		}
		want = append(want, found)
	}
	// Literal, so a filter and a parser that drift together still fail: only the two plain
	// lines count; the template's `Refs` sits in a comment.
	eq(t, "RefsTargets", want, []bool{false, true, true, false, false, false, false, false})
	for _, variant := range []string{"claude", "agents"} {
		raw, err := os.ReadFile("../../pack/core/skills/close-issue/" + variant + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		for _, section := range []string{
			skillSection(t, body, "## Step 0", "\n## "),
			skillSection(t, body, "### B2a.", "\n### "),
		} {
			if !strings.Contains(section, jqStripComments) {
				t.Fatalf("%s: a section without the comment filter %s", variant, jqStripComments)
			}
			m := jqRefsTest.FindStringSubmatch(section)
			if m == nil {
				t.Fatalf("%s: no case-insensitive Refs test in the jq filter", variant)
			}
			literal := strings.ReplaceAll(m[1], "<nr>", "147")
			program := `[.[] | ` + jqStripComments + ` | test("` + literal + `"; "i")]`
			cmd := exec.Command("jq", "-c", program)
			cmd.Stdin = strings.NewReader(string(input))
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("jq %s: %v", program, err)
			}
			var got []bool
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("jq output %q: %v", out, err)
			}
			eq(t, variant+" "+literal, got, want)
		}
	}
}
