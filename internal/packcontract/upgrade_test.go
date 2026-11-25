package packcontract

import (
	"strings"
	"testing"
)

// upgrade: what lives only in skill text. Step 1 reads both versions and the channel, compares
// versions without the `v` and with `lighthouse version compare` (never `sort -V`), picks the
// newest release per channel with jq, stops on an empty, `dev` or older version, and fetches the release
// notes from the one lighthouse repo the literal allow list names. The binary comes only through
// `lighthouse self-update`, `--yes` only after consent, a `refused:` stops, the evidence runs in
// order and the commit holds only what `init` wrote. The frontmatter is TestCorePack (name) and
// TestEverySkillHasOrigin (origin).

const upgrade = "core/skills/upgrade"

// upgradeStep is the text of one numbered step under "## Workflow", up to the next level-2 or
// level-3 heading.
func upgradeStep(t *testing.T, body string, n string) string {
	t.Helper()
	s := section(t, body, "\n### "+n+".", "\n## ")
	if i := strings.Index(s[1:], "\n### "); i >= 0 {
		return s[:i+1]
	}
	return s
}

// Classification: fails when step 1 stops reading the repo version from the manifest or the
// binary version from `lighthouse version`, or the binary loses the subcommand.
func TestUpgradeReadsBothVersions(t *testing.T) {
	cliFlags(t, []string{"version"})
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, upgradeStep(t, body, "1"),
				`M=$(sed -n 's/^version = "\(.*\)"$/\1/p' .lighthouse/manifest.toml | head -n 1)`,
				`B=$(lighthouse version | awk '{print $NF}')`)
		})
	}
}

// Classification: fails when the versions are compared with their `v` prefix, the newest
// release is picked without `lighthouse version compare`, or the channel filter of jq changes
// (draft dropped on both channels, prerelease only on `release`, `-dev.N` and `-rc.N` only on
// `dev`, never `-test.N`).
func TestUpgradeComparesVersionsWithoutTheVPrefix(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			s1 := upgradeStep(t, body, "1")
			wantAll(t, s1, "M=${M#v}; B=${B#v}; N=${N#v}", "C=$(lighthouse channel)",
				`select((.prerelease or .draft)|not)|.tag|select(test("^v[0-9]+\\.[0-9]+\\.[0-9]+$"))`,
				`select(.draft|not)`,
				`^v[0-9]+\\.[0-9]+\\.[0-9]+(-(dev|rc)\\.[0-9]+)?$`,
				`lighthouse version compare "$t" "$N"`)
			wantNone(t, s1, "-test")
			wantAll(t, flat(upgradeStep(t, body, "3")),
				"Strip the `v` with `${x#v}`, and keep a tag when `lighthouse version compare` says `M < tag <= B`, never on the order `lighthouse releases` returns them. On the `dev` channel the prereleases of step 1 count too.")
		})
	}
}

// Classification: fails when a skill goes back to `sort -V` or loses `lighthouse channel` or
// `lighthouse version compare`.
func TestUpgradeSkillUsesVersionCompare(t *testing.T) {
	cliFlags(t, []string{"channel"})
	cliFlags(t, []string{"version", "compare"})
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, body, "lighthouse version compare", "lighthouse channel")
			wantNone(t, body, "sort -V")
		})
	}
}

// Classification: fails when a `dev` build no longer stops the skill before any comparison.
func TestUpgradeDevBuildStops(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, upgradeStep(t, body, "1"), "- `M` or `B` is `dev`: say so and stop. Never compare a `dev` build.")
		})
	}
}

// Classification: fails when a binary older than the repo no longer stops the skill before
// `init --upgrade`.
func TestUpgradeOlderBinaryStops(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "1")),
				"- `B` is older than the repo (`lighthouse version compare \"$M\" \"$B\"` prints `1`): say \"the binary is older than the repo\" and stop. Never run `init --upgrade` then; it would downgrade.")
		})
	}
}

// Classification: fails when the skill gets a binary any other way than `lighthouse
// self-update`, stops warning that it replaces the binary for the whole host, or runs it before
// the user agrees.
func TestUpgradeBinaryOnlyThroughSelfUpdateAfterConsent(t *testing.T) {
	cliFlags(t, []string{"self-update"})
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			s2 := flat(upgradeStep(t, body, "2"))
			wantAll(t, s2,
				"propose `lighthouse self-update` and warn that it replaces the binary for the whole host.",
				"Run it only after the user agrees")
			wantNone(t, body, "go install")
		})
	}
}

// Classification: fails when a declined self-update goes on without a binary ahead of the repo,
// or an equal version no longer stops with "already current".
func TestUpgradeDeclinedSelfUpdateContinuesOnlyWhenAhead(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, upgradeStep(t, body, "2"),
				"If the user declines, or nothing newer exists, compare `B` with `M`:",
				"- `B` = `M`: say \"already current\" and stop.",
				"- `B` is ahead of `M`: continue with `B`, the binary the user has.")
		})
	}
}

// Classification: fails when the release notes come from another repo than the lighthouse repo,
// the skill names that repo more than once (the literal allow list holds exactly that one line
// per variant), or `lighthouse releases` loses `--repo`.
func TestUpgradeReleaseNotesComeFromTheLighthouseRepo(t *testing.T) {
	cliFlags(t, []string{"releases"}, "repo")
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			lines := strings.Split(body, "\n")
			exact, named := 0, 0
			for _, l := range lines {
				if l == "L=CorneeldH/lighthouse" {
					exact++
				}
				if strings.Contains(l, "CorneeldH") {
					named++
				}
			}
			if exact != 1 || named != 1 {
				t.Errorf("L=CorneeldH/lighthouse %d times, CorneeldH on %d lines; want 1 and 1", exact, named)
			}
			wantAll(t, upgradeStep(t, body, "1"), `lighthouse releases --repo "$L" > "$T/releases.json"`)
		})
	}
}

// Classification: fails when a `refused:` from init no longer stops the skill, or a line names
// `--force` without forbidding it.
func TestUpgradeNeverForcesAConflict(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "5")),
				"`refused:` on stderr (exit not 0), for example `refused: N locally modified unit(s)`: report it and stop. Never `--force`.")
			hits := 0
			for line := range strings.SplitSeq(body, "\n") {
				if !strings.Contains(line, "--force") {
					continue
				}
				hits++
				if !strings.Contains(strings.ToLower(line), "never") {
					t.Errorf("names --force without never: %s", line)
				}
			}
			if hits == 0 {
				t.Error("no line names --force")
			}
		})
	}
}

// Classification: fails when the evidence of step 6 changes order (test gate, check-instructions,
// mirror exceptions, `No changes.`), stops at the first red no longer, or the binary loses
// `check-instructions --print-mirror-exceptions`.
func TestUpgradeEvidenceInOrder(t *testing.T) {
	cliFlags(t, []string{"check-instructions"}, "print-mirror-exceptions")
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			s6 := upgradeStep(t, body, "6")
			wantAll(t, s6, "In this order; at the first red, stop, report what it says and do not commit.")
			wantOrder(t, s6,
				"1. The test gate. `lighthouse config test_gate.command`",
				"2. `lighthouse check-instructions`.",
				"run `lighthouse check-instructions --print-mirror-exceptions` and",
				"4. `lighthouse init --upgrade </dev/null` once more must print exactly `No changes.`.")
		})
	}
}

// Classification: fails when step 7 stops checking that `git status` holds only what init wrote,
// stops on anything else no longer, or the commit subject changes.
func TestUpgradeCommitHoldsOnlyWhatInitWrote(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "7")),
				"`git status` must hold only what `init --upgrade` wrote, plus the mirror file. Anything else (the test gate writes files too): stop and report it.",
				"One commit, subject `chore(lighthouse): upgrade to <B>`")
		})
	}
}

// Classification: fails when `init --upgrade --yes` runs before the dry run and the user's
// consent, the binary loses `--upgrade` or `--yes`, or the test gate runs without the variable
// the commit gate sets.
func TestUpgradeInitAppliesOnlyAfterConsentWithYes(t *testing.T) {
	cliFlags(t, []string{"init"}, "upgrade", "yes")
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantOrder(t, upgradeStep(t, body, "5"),
				"lighthouse init --upgrade </dev/null",
				"Then ask the user to agree",
				"lighthouse init --upgrade --yes")
			wantAll(t, upgradeStep(t, body, "6"),
				"`LIGHTHOUSE_GATE_TMP=$(mktemp -d) bash -c \"$(lighthouse config test_gate.command)\"`")
		})
	}
}

// Classification: fails when push and PR no longer wait for consent or stop going through
// /lighthouse-write-pr.
func TestUpgradePushAndPRAfterConsentThroughWritePR(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "7")),
				"Push and open the PR only after the user agrees, through `/lighthouse-write-pr`.")
		})
	}
}

// upgradeSandbox is how each variant tells the agent to run a network call.
var upgradeSandbox = map[string]string{
	"claude": "run it with `dangerouslyDisableSandbox: true`.",
	"agents": "run it outside any sandbox that blocks the network.",
}

// Classification: fails when step 2 or 3 stops saying its call needs the network and how to run
// it outside the sandbox, or step 3 stops reporting a failed fetch.
func TestUpgradeNetworkStepsNameTheSandboxAndReportAFailedFetch(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			sandbox, ok := upgradeSandbox[variant]
			if !ok {
				t.Fatalf("no sandbox wording for variant %s", variant)
			}
			wantAll(t, flat(upgradeStep(t, body, "2")), "It needs the network: "+sandbox)
			s3 := flat(upgradeStep(t, body, "3"))
			wantAll(t, s3,
				"The `lighthouse releases` call needs the network: "+sandbox,
				"If step 1 printed `releases: FETCH FAILED` or a fetch fails, tell the user \"could not fetch release notes\" and why; do not go on as if there were none.")
		})
	}
}

// Classification: fails when an empty repo or binary version no longer stops the skill before
// any comparison.
func TestUpgradeEmptyVersionStopsBeforeComparing(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "1")),
				"- `M` or `B` is empty (the manifest has no version, or `lighthouse version` printed nothing): say so and stop, before any comparison.")
		})
	}
}

// Classification: fails when an unset or empty test gate counts as evidence, or `bash -c ""`
// as green.
func TestUpgradeEmptyTestGateIsNoEvidence(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(upgradeStep(t, body, "6")),
				"`lighthouse config test_gate.command` exits 1 when none is set, or prints nothing when it is empty: say so and skip it, it is no evidence; never count `bash -c \"\"` as green.")
		})
	}
}

// Classification: fails when a binary from before the channels (no `version compare`, no
// `channel`) or a broken channel file no longer stops the skill, so the skill fails open on
// `release`.
func TestUpgradeOldBinaryAndBadChannelStop(t *testing.T) {
	for variant, body := range variants(t, upgrade) {
		t.Run(variant, func(t *testing.T) {
			s1 := flat(upgradeStep(t, body, "1"))
			wantAll(t, s1,
				`[ "$(lighthouse version compare 0.0.0 0.0.0 2>/dev/null)" = 0 ] || echo "binary too old for this skill"`,
				"- Step 1 printed `binary too old for this skill` (`lighthouse version compare` exits non-zero or prints nothing): say so and stop; the binary predates the channel.",
				"- `C` is empty or not one of the two channel names (`lighthouse channel` failed, or the channel file is broken): show its error and stop. Never fall back to a default channel.")
		})
	}
}
