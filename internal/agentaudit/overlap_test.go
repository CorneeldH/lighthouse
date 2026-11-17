package agentaudit

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/pack"
)

// coreGuards are the guards of the real core and go packages.
func coreGuards(t *testing.T) []packs.Guard {
	t.Helper()
	var guards []packs.Guard
	for _, name := range []string{"core", "go"} {
		sub, err := fs.Sub(pack.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		p, err := packs.Load(sub, name, "dev")
		if err != nil {
			t.Fatal(err)
		}
		guards = append(guards, p.Guards...)
	}
	return guards
}

func fixture(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "main-commit-guard.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOverlapMainCommitGuard(t *testing.T) {
	guard := fixture(t)
	inv, _ := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/main-commit-guard.sh"},
			[3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/check-branch.sh`},
			// Lighthouse's own hook is never a candidate, whatever its script says.
			[3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh main-commit-gate`},
		),
		".claude/hooks/main-commit-guard.sh": guard,
		".claude/hooks/check-branch.sh":      guard,
		".claude/hooks/lighthouse/run.sh":    guard,
	}, nil)
	got := Overlap(inv, coreGuards(t))
	requireKeys(t, got, []string{
		"overlap:main-commit-gate|project/.claude/hooks/check-branch.sh",
		"overlap:main-commit-gate|project/.claude/hooks/main-commit-guard.sh",
	})
	assertFinding(t, got[0], Finding{
		Key:  "overlap:main-commit-gate|project/.claude/hooks/check-branch.sh",
		Kind: "overlap",
		Text: []string{
			"project .claude/hooks/check-branch.sh (PreToolUse Bash) lijkt op lighthouse main-commit-gate",
			"lighthouse: Blocks a git commit or push on the default branch.",
			`treffers: "git commit", "main"`,
		},
		Sources: []string{"lighthouse", "project"},
	})
}

func TestNoOverlapUnrelated(t *testing.T) {
	inv, _ := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/shellcheck.sh"},
			[3]string{"PostToolUse", "Bash", ".claude/hooks/log-commands.sh"},
			[3]string{"PostToolUse", "Edit|Write", ".claude/hooks/format-md.sh"},
		),
		// A linter on the same event as main-commit-gate.
		".claude/hooks/shellcheck.sh": `#!/usr/bin/env bash
# Run shellcheck on a shell script before Bash executes it.
command=$(jq -r '.tool_input.command // empty')
case "$command" in
  *.sh) script=${command##* } ;;
  *) exit 0 ;;
esac
if ! shellcheck --severity=error "$script" >&2; then
  echo "shellcheck reports errors in $script" >&2
  exit 2
fi
`,
		// A command log: bash and tee, but it checks no writes.
		".claude/hooks/log-commands.sh": `#!/usr/bin/env bash
# Keep a log of every Bash command, also the git commit ones.
jq -r '.tool_input.command' | tee -a "$HOME/.claude/bash-commands.log" >/dev/null
`,
		// A formatter for markdown: no link check.
		".claude/hooks/format-md.sh": `#!/usr/bin/env bash
file=$(jq -r '.tool_input.file_path // empty')
case "$file" in
  *.md) prettier --write "$file" >/dev/null ;;
esac
`,
	}, nil)
	if got := Overlap(inv, coreGuards(t)); len(got) != 0 {
		t.Errorf("overlap %q; want none", keys(got))
	}
}

func TestOverlapCoreGuards(t *testing.T) {
	cases := []struct {
		name, event, matcher, script string
		want                         []string
	}{
		{
			name: "tests before a commit", event: "PreToolUse", matcher: "Bash",
			script: "#!/bin/sh\ncmd=$(jq -r .tool_input.command)\ncase \"$cmd\" in *\"git commit\"*) go test ./... || exit 2 ;; esac\n",
			want:   []string{"overlap:commit-test-gate|project/.claude/hooks/x.sh"},
		},
		{
			name: "files a Bash call changed", event: "PostToolUse", matcher: "Bash",
			script: "#!/bin/sh\nfor f in $(git status --porcelain | cut -c4-); do echo \"modified: $f\"; done\n",
			want:   []string{"overlap:bash-write-check|project/.claude/hooks/x.sh"},
		},
		{
			name: "dead links in markdown", event: "PostToolUse", matcher: "Write",
			script: "#!/bin/sh\nf=$(jq -r .tool_input.file_path)\ncase \"$f\" in *.md) lychee --offline \"$f\" || exit 2 ;; esac\n",
			want:   []string{"overlap:md-link-check|project/.claude/hooks/x.sh"},
		},
		{
			name: "golangci-lint at stop", event: "Stop", matcher: "",
			script: "#!/bin/sh\n[ -f go.mod ] || exit 0\ngolangci-lint run ./... >&2 || exit 2\n",
			want:   []string{"overlap:go-lint-gate|project/.claude/hooks/x.sh"},
		},
		{
			name: "a commit message check", event: "PreToolUse", matcher: "Bash",
			script: "#!/bin/sh\n# Conventional Commits only on git commit.\ngrep -qE '^(feat|fix)' || exit 2\n",
			want:   []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			inv, _ := collected(t, map[string]string{
				".claude/settings.json": registered([3]string{c.event, c.matcher, ".claude/hooks/x.sh"}),
				".claude/hooks/x.sh":    c.script,
			}, nil)
			assertRows(t, "keys", keys(Overlap(inv, coreGuards(t))), c.want)
		})
	}
}

// guardOnBash is a guard that watches Bash before and after it runs.
var guardOnBash = packs.Guard{
	Name: "g", Events: []string{"PreToolUse", "PostToolUse"}, Matcher: "Bash", Does: "Guards.",
	Terms: [][]string{{"alpha", "Beta"}, {"gamma"}},
}

func TestOverlapOnePerGuardAndScript(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/twice.sh"},
			[3]string{"PostToolUse", "Bash", ".claude/hooks/twice.sh"},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/twice.sh"},
		),
		".claude/hooks/twice.sh": "#!/bin/sh\n# BETA and GAMMA\n",
	}, nil)
	got := Overlap(inv, []packs.Guard{guardOnBash})
	requireKeys(t, got, []string{"overlap:g|project/.claude/hooks/twice.sh"})
	assertRows(t, "text", got[0].Text, []string{
		"project .claude/hooks/twice.sh (PreToolUse Bash, PostToolUse Bash) lijkt op lighthouse g",
		"lighthouse: Guards.",
		`treffers: "Beta", "gamma"`,
	})
	// Registered twice, the script is registered: no unregistered-script either.
	if c := Conflicts(inv, env); len(c) != 0 {
		t.Errorf("conflicts %q; want none", keys(c))
	}
}

func TestOverlapEventAndMatcher(t *testing.T) {
	inline := "echo alpha gamma"
	cases := []struct {
		event, matcher string
		want           []string
	}{
		{"PreToolUse", "Bash", []string{"overlap:g|project/echo alpha gamma"}},
		{"PreToolUse", "", []string{"overlap:g|project/echo alpha gamma"}},
		{"PreToolUse", "*", []string{"overlap:g|project/echo alpha gamma"}},
		{"PreToolUse", "Edit|Bash", []string{"overlap:g|project/echo alpha gamma"}},
		{"PreToolUse", "Edit|Write", []string{}},
		{"Stop", "", []string{}},
	}
	for _, c := range cases {
		t.Run(c.event+" "+c.matcher, func(t *testing.T) {
			inv, _ := collected(t, map[string]string{
				".claude/settings.json": registered([3]string{c.event, c.matcher, inline}),
			}, nil)
			assertRows(t, "keys", keys(Overlap(inv, []packs.Guard{guardOnBash})), c.want)
		})
	}
	t.Run("one group without a hit", func(t *testing.T) {
		inv, _ := collected(t, map[string]string{
			".claude/settings.json": registered([3]string{"PreToolUse", "Bash", "echo alpha beta"}),
		}, nil)
		assertRows(t, "keys", keys(Overlap(inv, []packs.Guard{guardOnBash})), []string{})
	})
}

func TestMatcherHits(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Bash", "Bash", true},
		{"Bash", "", true},
		{"", "Edit", true},
		{"Bash", "*", true},
		{"Edit|Write", " Write ", true},
		{"Edit|Write", "MultiEdit|Write", true},
		{"Edit|Write", "Bash", false},
		{"Bash", "BashOutput", false},
	}
	for _, c := range cases {
		if got := matcherHits(c.a, c.b); got != c.want {
			t.Errorf("matcherHits(%q, %q) = %t; want %t", c.a, c.b, got, c.want)
		}
	}
}

func TestOverlapSkipsUnreadableHook(t *testing.T) {
	// Content the collector never sets for an unreadable script, so only the Unreadable
	// check keeps this hook out.
	inv := &Inventory{Hooks: []Hook{{
		Source: SourceProject, Event: "PreToolUse", Matcher: "Bash", Command: "x.sh",
		Script: "/r/x.sh", Shown: "x.sh", Content: []byte("alpha gamma"), Unreadable: "permission denied",
	}}}
	if got := Overlap(inv, []packs.Guard{guardOnBash}); len(got) != 0 {
		t.Errorf("overlap %q; want none for an unreadable hook", keys(got))
	}
}
