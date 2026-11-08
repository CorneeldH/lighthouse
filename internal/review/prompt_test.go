package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	skillsDir   = "../../pack/core/skills"
	fixturesDir = "../../tests/pack/fixtures/review_run"
)

func readFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestBuildPromptByteIdentical compares BuildPrompt with the prompts that the old review.sh
// built (fixtures generated once with that script; @REPO@ and @OUT@ stand for the temp paths).
func TestBuildPromptByteIdentical(t *testing.T) {
	out := t.TempDir()
	repo := t.TempDir()
	spec := readFile(t, skillsDir, "review-spec", "reviewer-prompt.md")
	specVerify := readFile(t, skillsDir, "review-spec", "verifier-prompt.md")
	plan := readFile(t, skillsDir, "review-plan", "reviewer-prompt.md")
	planVerify := readFile(t, skillsDir, "review-plan", "verifier-prompt.md")
	prior := filepath.Join(out, "round-1.json")
	diff := filepath.Join(out, "x.diff")

	cases := []struct {
		fixture  string
		template []byte
		doc      string
		round    string
		prior    string
		contexts []string
		sidecar  bool
	}{
		{"prompt-spec-plain.txt", spec, "docs/doc.md", "2", "none", nil, false},
		{"prompt-spec-prior.txt", spec, "docs/doc.md", "2", prior, nil, false},
		{"prompt-spec-sidecar.txt", spec, "docs/doc.md", "2", prior, nil, true},
		{"prompt-spec-space.txt", spec, "docs/my doc.md", "1", "none", nil, false},
		{"prompt-plan-nocontext.txt", plan, "docs/doc.md", "1", "none", nil, false},
		{"prompt-plan-context.txt", plan, "docs/doc.md", "1", "none", []string{"docs/spec.md", diff}, false},
		{"prompt-verify-spec.txt", specVerify, "docs/doc.md", "2", prior, []string{diff}, false},
		{"prompt-verify-plan.txt", planVerify, "docs/doc.md", "2", "none", []string{"docs/spec.md"}, false},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			sidecar := filepath.Join(out, "round-1.decisions.json")
			if c.sidecar {
				if err := os.WriteFile(sidecar, []byte("[]"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Remove(sidecar) })
			}
			want := strings.NewReplacer("@REPO@", repo, "@OUT@", out).Replace(string(readFile(t, fixturesDir, c.fixture)))
			got, err := BuildPrompt(c.template, c.doc, repo, c.round, c.prior, c.contexts)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("prompt differs from the old review.sh.\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestBuildPromptContextNeedsPlaceholder(t *testing.T) {
	spec := readFile(t, skillsDir, "review-spec", "reviewer-prompt.md")
	_, err := BuildPrompt(spec, "docs/doc.md", "/repo", "1", "none", []string{"docs/spec.md"})
	if err == nil || err.Error() != "--context given, but the template has no {{CONTEXT_PATHS}}" {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildPromptUnsubstitutedPlaceholder(t *testing.T) {
	tmpl := []byte("x\n---BEGIN PROMPT---\nhello {{NOPE}}\n---END PROMPT---\n")
	_, err := BuildPrompt(tmpl, "d", "/r", "1", "none", nil)
	if err == nil || err.Error() != "unsubstituted placeholder in the prompt template" {
		t.Fatalf("err = %v", err)
	}
}

func TestBuildPromptDocPathIsSubstitutedVerbatim(t *testing.T) {
	tmpl := []byte("---BEGIN PROMPT---\nDoc: {{DOC_PATH}} round {{ROUND}}\n---END PROMPT---")
	got, err := BuildPrompt(tmpl, "docs/a b.md", "/r", "3", "none", nil)
	if err != nil || got != "Doc: docs/a b.md round 3" {
		t.Fatalf("got %q, %v", got, err)
	}
}
