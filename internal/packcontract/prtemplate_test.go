package packcontract

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/prbody"
)

// The PR template that write-pr fills in and init installs unchanged as
// .github/pull_request_template.md. The `## Open review points` heading is
// TestReviewPRTemplateKnowsTheOpenReviewPointsHeading; the `--repo` on its gh command is
// TestEveryWritingGhCommandNamesTheRepo.

const prTemplate = "core/templates/pull_request_template.md"

// writePRNameRe is a slash command named like write-pr, with any prefix.
var writePRNameRe = regexp.MustCompile(`/[a-z]+-write-pr\b`)

// Classification: fails when a section stops saying /lighthouse-write-pr writes it, or the
// template names write-pr under another prefix.
func TestPRTemplateNamesLighthouseWritePR(t *testing.T) {
	text := readPack(t, prTemplate)
	f := flat(text)
	wantAll(t, f,
		"<!-- Written by /lighthouse-write-pr. One of three forms:",
		"only when a person opens the PR without /lighthouse-write-pr: the merge closes the issue;",
		"<!-- Written by /lighthouse-write-pr from `lighthouse plan-review-record pr-open-points` when a spec or plan on this branch ends in an `Open after review` section")
	names := writePRNameRe.FindAllString(text, -1)
	if len(names) != 3 {
		t.Errorf("%d write-pr names, want 3: %v", len(names), names)
	}
	for _, n := range names {
		if n != "/lighthouse-write-pr" {
			t.Errorf("names %s", n)
		}
	}
}

// Classification: fails when an open criterion waits on a fixed host or machine instead of being
// a test CI cannot run.
func TestPRTemplateIsHostless(t *testing.T) {
	text := readPack(t, prTemplate)
	wantAll(t, flat(text),
		"each open criterion with what is still needed (a deploy, a measurement over days, a test that cannot run in CI);")
	wantNone(t, strings.ToLower(text), "host", "machine")
}

// Classification: fails when `Closes` comes before `Refs`, or `Closes` stops being only for a PR
// opened without write-pr.
func TestPRTemplatePutsRefsFirst(t *testing.T) {
	text := readPack(t, prTemplate)
	wantOrder(t, text, "`Refs #123 — <reason>`", "`Closes #123`")
	wantAll(t, flat(text),
		"`Closes #123` + a `### Evidence #123` table with one row per acceptance criterion, only when a person opens the PR without /lighthouse-write-pr: the merge closes the issue;")
}

// Classification: fails when the template names a subcommand the binary does not have.
func TestPRTemplateNamesTheEnglishSubcommand(t *testing.T) {
	wantAll(t, readPack(t, prTemplate), "`lighthouse plan-review-record pr-open-points`")
	cliFlags(t, []string{"plan-review-record", "pr-open-points"})
}

// Classification: fails when the template names the review section by another title than the
// English value of heading open_after_review.
func TestPRTemplateOpenAfterReviewIsTheEnglishHeading(t *testing.T) {
	t.Setenv("LIGHTHOUSE_HEADINGS", filepath.Join(packRoot, "core", "headings.toml"))
	heading, err := config.Heading("open_after_review", "en")
	if err != nil {
		t.Fatal(err)
	}
	title, ok := strings.CutPrefix(heading, "## ")
	if !ok {
		t.Fatalf("open_after_review en = %q, not a ## heading", heading)
	}
	wantAll(t, flat(readPack(t, prTemplate)), "ends in an `"+title+"` section")
}

// prTemplateReason is a reason long enough for the gate.
const prTemplateReason = "a reason the gate accepts"

// Classification: fails when the template loses one of the forms pr-create-gate reads, or writes
// it in a shape the gate's parser does not recognise.
func TestPRTemplateFormsAreWhatTheGateReads(t *testing.T) {
	text := readPack(t, prTemplate)
	forms := []struct {
		token string
		reads func(body string) bool
	}{
		{"`Closes #123`", func(b string) bool {
			return slices.Equal(prbody.ClosesTargets(b, prbody.Repo{Kind: "github"}), []int{123})
		}},
		{"`### Evidence #123`", func(b string) bool { return prbody.HasEvidenceBlock(b, 123) }},
		{"`Refs #123 — <reason>`", func(b string) bool { return slices.Equal(prbody.RefsIssues(b), []int{123}) }},
		{"`No issue: <reason>`", prbody.NoIssue},
	}
	for _, f := range forms {
		if !strings.Contains(text, f.token) {
			t.Errorf("missing %s", f.token)
			continue
		}
		body := strings.ReplaceAll(strings.Trim(f.token, "`"), "<reason>", prTemplateReason)
		if !f.reads(body) {
			t.Errorf("the gate does not read %s", f.token)
		}
	}
}

// prTemplateIssueRe is any issue number.
var prTemplateIssueRe = regexp.MustCompile(`#\d+`)

// Classification: fails when the template names an issue number other than the placeholder #123.
func TestPRTemplateHasNoProjectIssueNumbers(t *testing.T) {
	numbers := prTemplateIssueRe.FindAllString(readPack(t, prTemplate), -1)
	if len(numbers) == 0 {
		t.Fatal("no issue number at all: the placeholder #123 is gone")
	}
	for _, n := range numbers {
		if n != "#123" {
			t.Errorf("issue number %s", n)
		}
	}
}

// Classification: fails when the template names the toolchain of one language.
func TestPRTemplateNamesNoLanguageToolchain(t *testing.T) {
	wantNone(t, readPack(t, prTemplate), "ruff", "pytest", " ty/", "ty ")
}
