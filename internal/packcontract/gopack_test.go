package packcontract

import (
	"regexp"
	"strings"
	"testing"
)

// go pack: what lives only in the text of the skill go-standard and the instruction block of
// pack/go. The name, the English, the config keys and the section order are covered or not
// protected (the triage of #368); the KPI commands are run by the e2e tests, not here.

const (
	goSkill = "go/skills/go-standard"

	// openCodeGoDescription is the one sentence only the agents variant adds to the description.
	openCodeGoDescription = " In OpenCode the enforcing hooks do not run — run the KPI commands by hand."
)

// regexpItems splits a text into paragraphs and list items.
var regexpItems = regexp.MustCompile(`\n\s*\n|\n(?:- )`)

// kpiRows returns the rows of the table that starts at the `| Norm |` header.
func kpiRows(t *testing.T, body string) []string {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "| Norm |") {
			continue
		}
		var rows []string
		for _, l := range lines[i+2:] {
			if !strings.HasPrefix(l, "|") {
				break
			}
			rows = append(rows, l)
		}
		return rows
	}
	t.Fatal("no KPI table")
	return nil
}

// sectionUntilNext returns the text under a level-2 heading, up to the next one.
func sectionUntilNext(t *testing.T, body, heading string) string {
	t.Helper()
	return section(t, body+"\n## ", heading, "\n## ")
}

// stripOpenCode returns the paragraphs of the agents text without OpenCode: the description
// sentence removed, every paragraph that names OpenCode dropped.
func stripOpenCode(agents, descriptionSentence string) []string {
	var kept []string
	for _, p := range paragraphSplit.Split(strings.ReplaceAll(agents, descriptionSentence, ""), -1) {
		if strings.TrimSpace(p) != "" && !strings.Contains(p, "OpenCode") {
			kept = append(kept, strings.TrimSpace(p))
		}
	}
	return kept
}

// textParagraphs returns the non-empty paragraphs of text, trimmed.
func textParagraphs(text string) []string {
	var out []string
	for _, p := range paragraphSplit.Split(text, -1) {
		if strings.TrimSpace(p) != "" {
			out = append(out, strings.TrimSpace(p))
		}
	}
	return out
}

// Classification: fails when the two variants of go-standard drift apart outside the OpenCode
// sentence and paragraphs. Only the agents variant may say the hooks do not run; check 11 of
// check-instructions skips skills with lighthouse metadata, so nothing else compares them.
func TestGoStandardVariantsKeepTheirDifference(t *testing.T) {
	v := variants(t, goSkill)
	claude, agents := v["claude"], v["agents"]
	wantNone(t, claude, "OpenCode")
	wantAll(t, agents, "OpenCode does not run these hooks")
	wantAll(t, frontmatter(t, agents), openCodeGoDescription)
	got := stripOpenCode(agents, openCodeGoDescription)
	want := textParagraphs(claude)
	if len(got) != len(want) {
		t.Fatalf("agents without OpenCode has %d paragraphs, claude %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("paragraph %d differs:\nagents: %q\nclaude: %q", i, got[i], want[i])
		}
	}
}

// Classification: fails when the agents variant stops naming the commands to run by hand
// (gofmt, golangci-lint) or names `go vet` there, since OpenCode runs no hook and nothing else
// tells it what the hooks would have run.
func TestGoStandardOpenCodeNamesTheHookCommands(t *testing.T) {
	agents := variants(t, goSkill)["agents"]
	var note string
	for _, p := range textParagraphs(sectionUntilNext(t, agents, "## The pairing")) {
		if strings.Contains(p, "OpenCode does not run these hooks") {
			note = p
		}
	}
	if note == "" {
		t.Fatal("no OpenCode note in the pairing")
	}
	wantAll(t, note, "`gofmt -w <file>`", "`golangci-lint run ./...`")
	wantNone(t, note, "go vet")
}

// Classification: fails when the description loses what keeps Go work from pulling in the
// Python or shell skill (descriptions stay in context and the model picks by likeness), or the
// triggers on planning.
func TestGoStandardDescription(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			front := frontmatter(t, body)
			wantAll(t, front, "NOT for", "lighthouse-python-standard", "lighthouse-shell-standard",
				"writing or changing Go", "_test.go", "planning a refactor", "before a file is open")
		})
	}
}

// Classification: fails when a KPI row has no command (no command, no KPI), when the test row
// loses -shuffle=on or gets -count=1, or when one of the five KPI commands is gone.
func TestGoStandardKPIRows(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			rows := kpiRows(t, body)
			if len(rows) < 6 {
				t.Errorf("KPI table has %d rows, want at least 6", len(rows))
			}
			for _, row := range rows {
				cells := strings.Split(row, "|")
				if len(cells) < 3 || !strings.Contains(cells[2], "`") {
					t.Errorf("KPI row without a command: %s", row)
				}
			}
			all := strings.Join(rows, "\n")
			wantAll(t, all,
				"`go test -race -shuffle=on ./...`",
				"`git ls-files -z '*.go' \\| xargs -0r gofmt -l`",
				"`go mod tidy -diff`", "`go vet ./...`", "`golangci-lint run ./...`",
				"test_gate.command")
			wantNone(t, all, "-count=1")
		})
	}
}

// Classification: fails when the skill stops telling that the selector's exit 3 belongs in
// `unchecked_exit_codes`, or when the gate command gets -shuffle=on (it turns off Go's test
// cache; CI keeps it).
func TestGoStandardGateCommand(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			text := flat(body)
			wantAll(t, text,
				"list 3 in `[test_gate] unchecked_exit_codes`",
				"`[test_gate] command = \"lighthouse gate go -- go test -race\"`")
			wantNone(t, text, "command = \"go test -shuffle=on")
		})
	}
}

// Classification: fails when the skill grows a class table of its own, or loses the pointer to
// lighthouse-test-standard and the classification sentence it asks for.
func TestGoStandardPointsToTheTestingSkill(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "lighthouse-test-standard", "Classify first",
				"Class: <class>. At risk: <the observable behaviour that could change>.")
			wantNone(t, body, "`pure-logic`")
		})
	}
}

// Classification: fails when the report of done loses its rule that a row left out reads as
// passed, or one of the four things each row reports.
func TestGoStandardReportingDone(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, sectionUntilNext(t, body, "## Reporting done"),
				"A row left out of the report reads as passed.",
				"command", "outcome", "environment", "not measurable")
		})
	}
}

// Classification: fails when the non-negotiable rules lose the environment of a measurement
// (GOOS, -race, GOWORK) or the third outcome: a zero without its environment is not a zero.
func TestGoStandardNamesTheEnvironment(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, sectionUntilNext(t, body, "## Non-negotiable"),
				"Name GOOS, the build tags, `-race` on or off,", "`GOWORK` on or off",
				"not measurable from here")
		})
	}
}

// Classification: fails when the pairing row of go-lint-gate.sh names `go vet` (the hook runs
// only golangci-lint, whose standard set includes govet), or loses the gofmt hook and the
// pointer to lighthouse-test-standard.
func TestGoStandardPairingLintGateRow(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			pairing := sectionUntilNext(t, body, "## The pairing")
			wantAll(t, pairing, "lighthouse-test-standard", ".claude/hooks/lighthouse/gofmt-check.sh")
			var rows []string
			for line := range strings.SplitSeq(pairing, "\n") {
				if strings.HasPrefix(line, "|") && strings.Contains(line, "go-lint-gate.sh") {
					rows = append(rows, line)
				}
			}
			if len(rows) != 1 {
				t.Fatalf("want one go-lint-gate.sh row, got %d", len(rows))
			}
			wantAll(t, rows[0], "golangci-lint", "govet")
			wantNone(t, rows[0], "go vet")
		})
	}
}

// Classification: fails when the caveat on `issues.new-from-rev` goes: with it the lint row
// measures only new findings, but the KPI row `go vet ./...` does not read .golangci.yml, and
// the report must say so.
func TestGoStandardVetRowCaveatWithNewFromRev(t *testing.T) {
	for variant, body := range variants(t, goSkill) {
		t.Run(variant, func(t *testing.T) {
			found := false
			for _, item := range regexpItems.Split(body, -1) {
				if !strings.Contains(item, "issues.new-from-rev") {
					continue
				}
				if strings.Contains(item, "`go vet ./...`") && strings.Contains(item, ".golangci.yml") {
					found = true
				}
			}
			if !found {
				t.Error("no item names issues.new-from-rev with the go vet ./... caveat from .golangci.yml")
			}
		})
	}
}

// Classification: fails when the instruction block of pack/go stops naming the skill, gofmt,
// go-lint-gate and govet, or says `go vet` (the hook runs govet through golangci-lint).
func TestGoInstructionBlock(t *testing.T) {
	text := readPack(t, "go/instructions/AGENTS.md")
	if !strings.HasPrefix(text, "## Lighthouse: go\n") {
		t.Error("the block does not start with its heading")
	}
	wantAll(t, text, "/lighthouse-go-standard", "gofmt", "go-lint-gate", "govet")
	wantNone(t, text, "go vet")
}
