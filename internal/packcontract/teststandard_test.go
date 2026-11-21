package packcontract

import (
	"strings"
	"testing"
)

// test-standard: what lives only in skill text. The description triggers on the work and on
// planning, bounds TDD and the language skills, and the skill stays model-invoked. The body
// carries the core question, the classification sentence with its classes, the mutation check,
// the contract-test rules and the red flags, in English and without one stack's terms. The name
// is TestCorePack, the presence of `metadata.origin` TestEverySkillHasOrigin.

const testStandard = "core/skills/test-standard"

// testStandardSection is the flattened text under a level-2 heading, up to the next one.
func testStandardSection(t *testing.T, body, heading string) string {
	t.Helper()
	s := body + "\n## "
	return flat(section(t, s, "\n"+heading+"\n", "\n## "))
}

// Classification: fails when the skill gets `disable-model-invocation`, so it no longer loads
// on the work itself.
func TestTestStandardIsModelInvoked(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, frontmatter(t, body), "disable-model-invocation")
		})
	}
}

// Classification: fails when the description stops triggering on writing, changing, fixing or
// reviewing a test, a mock or a fake, or on planning a test strategy.
func TestTestStandardDescriptionTriggersOnTheWorkAndOnPlanning(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, frontmatter(t, body),
				"Use when writing, changing, fixing or reviewing a test, a mock, a fake or an assert;",
				"and when planning a test strategy, before any file is open.")
		})
	}
}

// Classification: fails when the description stops sending one language's tooling to the
// Python, Go or shell standard.
func TestTestStandardDescriptionBoundsTheLanguageSkills(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, frontmatter(t, body),
				"NOTE — not for the tooling of one language: that is lighthouse-python-standard, lighthouse-go-standard or lighthouse-shell-standard, if present.")
		})
	}
}

// Classification: fails when the description stops making TDD a companion that carries when
// to write the test.
func TestTestStandardDescriptionMakesTDDACompanion(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, frontmatter(t, body),
				"Use it alongside superpowers:test-driven-development, which carries when to write the test.")
		})
	}
}

// Classification: fails when the core question changes or the mutation check before done is
// dropped.
func TestTestStandardCoreQuestionAndMutationCheck(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## The core question"),
				"> Which change to the production code turns this test red?")
			wantAll(t, testStandardSection(t, body, "## Mutation check"),
				"Before you report done, mutate the production code in your head. For every realistic mutation, at least one test must fail:")
		})
	}
}

// Classification: fails when the core question no longer says the answer goes in the chat.
func TestTestStandardCoreQuestionSaysWhereTheAnswerGoes(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## The core question"),
				"Before every test, answer it in one sentence in the chat:")
		})
	}
}

// Classification: fails when contract tests on instruction text lose their own rules, or a
// prohibition is allowed beyond a lasting invariant.
func TestTestStandardContractTestsAreAKindOfTheirOwn(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## Contract tests and code tests"),
				"For an instruction file the text *is* the product, so reading it is no red flag here.",
				"A prohibition in a contract test is fine when it guards a **lasting invariant** (\"a skill names no issue numbers\"). It is not fine when it only guards that something stays gone after a migration: that is a negative-existence test, in a contract test too.")
		})
	}
}

// Classification: fails when a hook message may be asserted from the script source instead of
// a run.
func TestTestStandardHookMessagesAreAssertedFromARun(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## Contract tests and code tests"),
				"A hook message is not instruction text: it lives in a script, so assert it from the output of a run, never from the source.")
		})
	}
}

// Classification: fails when the classification sentence changes shape or a class leaves the
// table.
func TestTestStandardClassificationSentenceAndClasses(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "\n## Classify first\n", "\n## ")
			wantAll(t, s, "Class: <class>. At risk: <the observable behaviour that could change>.")
			for _, c := range []string{"pure-logic", "io/filesystem", "cli-output", "hook/gate", "config", "test-only"} {
				if !strings.Contains(s, "\n| `"+c+"` | ") {
					t.Errorf("no table row for class %s", c)
				}
			}
		})
	}
}

// Classification: fails when the place of the classification sentence (before the first edit,
// in the chat) changes, or is said a second time.
func TestTestStandardClassificationSentencePlaceIsSaidOnce(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			s := testStandardSection(t, body, "## Classify first")
			wantAll(t, s, "Before the first edit, write one sentence in the chat, not in the commit message or the PR body:")
			if n := strings.Count(strings.ToLower(s), "before the first edit"); n != 1 {
				t.Errorf("\"before the first edit\" %d times under Classify first, want 1", n)
			}
		})
	}
}

// Classification: fails when a red flag for a hidden tautology leaves the list.
func TestTestStandardRedFlagsCatchHiddenTautologies(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## Red flags"),
				"- The expected value is built by a helper, loop or builder instead of written out.",
				"- Setup and expectation share one object, so they are equal by construction.",
				"- It only fails by crashing: an exception or a missing element, never a wrong value.",
				"- It restates the code line by line, so any change to the code is a change to the test.",
				"- It exists only for coverage: it runs the code and asserts nothing that matters.")
		})
	}
}

// Classification: fails when asserting a mock call becomes a red flag without the exception
// for a call that is the contract.
func TestTestStandardCalledMockIsFineWhenTheCallIsTheContract(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, testStandardSection(t, body, "## Red flags"),
				"- It asserts that a mock was called, and nothing about the result, unless the call itself is the contract.")
		})
	}
}

// Classification: fails when the skill names one stack's tools, which it does not carry: it is
// for every language and every repository.
func TestTestStandardIsLanguageNeutral(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, body, "require.", "Playwright", "PostgreSQL", "pgtest", "Svelte", "Huma", "SSE", "FTS")
		})
	}
}

// Classification: fails when a Dutch sentence enters the skill.
func TestTestStandardIsEnglish(t *testing.T) {
	for variant, body := range variants(t, testStandard) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, strings.ToLower(body), " het ", " niet ", " een ", " wordt ", " dat ", " je ")
		})
	}
}
