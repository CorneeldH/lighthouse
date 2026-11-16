package agentaudit

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// collected runs Collect on a repository tree and a home tree (both slash paths to
// content), with HOME and CLAUDE_CONFIG_DIR on temporary directories.
func collected(t *testing.T, repo, homeFiles map[string]string) (*Inventory, Env) {
	t.Helper()
	return collectedWith(t, repo, homeFiles, nil)
}

// collectedWith is collected with installed packages.
func collectedWith(t *testing.T, repo, homeFiles map[string]string, installed []packs.Pack) (*Inventory, Env) {
	t.Helper()
	home, configDir := hostless(t)
	root := tree(t, repo)
	writeFiles(t, home, homeFiles)
	env := Env{Root: root, MainRoot: root, Home: home, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json"), Packs: installed}
	inv, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}
	return inv, env
}

// registered is a settings file registering each command under its event and matcher.
func registered(regs ...[3]string) string {
	var b strings.Builder
	b.WriteString(`{"hooks": {`)
	for i, r := range regs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(strconv.Quote(r[0]) + `: [{"matcher": ` + strconv.Quote(r[1]) +
			`, "hooks": [{"type": "command", "command": ` + strconv.Quote(r[2]) + `}]}]`)
	}
	b.WriteString("}}")
	return b.String()
}

// registeredMerged registers every command in one hooks object; regs with the same event
// share its array.
func registeredMerged(regs ...[3]string) string {
	byEvent := map[string][]string{}
	var events []string
	for _, r := range regs {
		if _, ok := byEvent[r[0]]; !ok {
			events = append(events, r[0])
		}
		byEvent[r[0]] = append(byEvent[r[0]], `{"matcher": `+strconv.Quote(r[1])+
			`, "hooks": [{"type": "command", "command": `+strconv.Quote(r[2])+`}]}`)
	}
	var parts []string
	for _, e := range events {
		parts = append(parts, strconv.Quote(e)+": ["+strings.Join(byEvent[e], ", ")+"]")
	}
	return `{"hooks": {` + strings.Join(parts, ", ") + "}}"
}

const blockSh = "#!/bin/sh\necho no >&2\nexit 2\n"

func keys(fs []Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Key)
	}
	return out
}

// requireKeys stops the test when the keys of got are not want.
func requireKeys(t *testing.T, got []Finding, want []string) {
	t.Helper()
	if k := keys(got); !reflect.DeepEqual(k, want) {
		t.Fatalf("keys:\ngot  %q\nwant %q", k, want)
	}
}

func findingOf(t *testing.T, fs []Finding, key string) Finding {
	t.Helper()
	for _, f := range fs {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no finding %q in %q", key, keys(fs))
	return Finding{}
}

func assertFinding(t *testing.T, got, want Finding) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("finding:\ngot  %#v\nwant %#v", got, want)
	}
}

func TestBlockingPairProjectOnly(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/check-branch.sh"},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/a-guard.sh"},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/quiet.sh"},      // does not block
			[3]string{"PreToolUse", "Edit", ".claude/hooks/edit-guard.sh"}, // other matcher
			[3]string{"PostToolUse", "Bash", ".claude/hooks/post.sh"},      // other event
		),
		".claude/hooks/check-branch.sh": blockSh,
		".claude/hooks/a-guard.sh":      blockSh,
		".claude/hooks/quiet.sh":        "#!/bin/sh\nexit 0\n",
		".claude/hooks/edit-guard.sh":   blockSh,
		".claude/hooks/post.sh":         blockSh,
	}, map[string]string{
		".claude/settings.json":  registered([3]string{"PreToolUse", "Bash", "~/.claude/hooks/guard.sh"}),
		".claude/hooks/guard.sh": blockSh,
	})
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{
		"blocking-pair:PreToolUse:Bash:gebruiker/~/.claude/hooks/guard.sh|project/.claude/hooks/a-guard.sh",
		"blocking-pair:PreToolUse:Bash:gebruiker/~/.claude/hooks/guard.sh|project/.claude/hooks/check-branch.sh",
		"blocking-pair:PreToolUse:Bash:project/.claude/hooks/a-guard.sh|project/.claude/hooks/check-branch.sh",
	})
	assertFinding(t, findingOf(t, got, "blocking-pair:PreToolUse:Bash:gebruiker/~/.claude/hooks/guard.sh|project/.claude/hooks/check-branch.sh"), Finding{
		Key:     "blocking-pair:PreToolUse:Bash:gebruiker/~/.claude/hooks/guard.sh|project/.claude/hooks/check-branch.sh",
		Kind:    "blocking-pair",
		Text:    []string{"PreToolUse Bash: gebruiker ~/.claude/hooks/guard.sh en project .claude/hooks/check-branch.sh blokkeren allebei"},
		Sources: []string{"gebruiker", "project"},
	})
}

func TestBlockingPairNormalizesMatcher(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PostToolUse", "Edit|Write", ".claude/hooks/a.sh"},
			[3]string{"PostToolUse", " Write | Edit ", ".claude/hooks/b.sh"},
			[3]string{"Stop", "", ".claude/hooks/c.sh"},
			[3]string{"Stop", "*", ".claude/hooks/d.sh"},
		),
		".claude/hooks/a.sh": blockSh,
		".claude/hooks/b.sh": blockSh,
		".claude/hooks/c.sh": blockSh,
		".claude/hooks/d.sh": blockSh,
	}, nil)
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{
		"blocking-pair:PostToolUse:Edit|Write:project/.claude/hooks/a.sh|project/.claude/hooks/b.sh",
		"blocking-pair:Stop::project/.claude/hooks/c.sh|project/.claude/hooks/d.sh",
	})
	assertRows(t, "stop text", findingOf(t, got, "blocking-pair:Stop::project/.claude/hooks/c.sh|project/.claude/hooks/d.sh").Text,
		[]string{"Stop: project .claude/hooks/c.sh en project .claude/hooks/d.sh blokkeren allebei"})
}

func TestNoBlockingPairWithLighthouse(t *testing.T) {
	core := packs.Pack{Name: "core", Units: []packs.Unit{{Kind: packs.KindFile, Path: packs.HookDir + "blocking", Content: []byte("main-commit-gate\npr-create-gate\n")}}}
	inv, env := collectedWith(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh main-commit-gate`},
			[3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh pr-create-gate`},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/check-branch.sh"},
		),
		".claude/hooks/lighthouse/run.sh": blockSh,
		".claude/hooks/check-branch.sh":   blockSh,
	}, nil, []packs.Pack{core})
	env.HookNames = []string{"main-commit-gate", "pr-create-gate"}
	var blocking []string
	for _, h := range inv.Hooks {
		blocking = append(blocking, h.Source+" "+strconv.FormatBool(h.Blocking))
	}
	assertRows(t, "blocking", blocking, []string{"lighthouse true", "lighthouse true", "project true"})
	if got := Conflicts(inv, env); len(got) != 0 {
		t.Errorf("conflicts %q; want none", keys(got))
	}
}

func TestPermissionLighthouseAllowVsUserDeny(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": `{"permissions": {"allow": ["Bash(lighthouse review-run:*)"]}}`,
	}, map[string]string{
		".claude/settings.json": `{"permissions": {"deny": ["Bash(lighthouse review-run:*)"]}}`,
	})
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{"permission:Bash(lighthouse review-run:*)"})
	assertFinding(t, got[0], Finding{
		Key:     "permission:Bash(lighthouse review-run:*)",
		Kind:    "permission",
		Text:    []string{"lighthouse allow Bash(lighthouse review-run:*) tegenover gebruiker deny Bash(lighthouse review-run:*)"},
		Sources: []string{"gebruiker", "lighthouse"},
	})
}

func TestPermissionProjectDenyOnLighthouseAllow(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": `{"permissions": {"allow": ["Bash(lighthouse review-run:*)"], "ask": ["Bash(lighthouse review-run:*)"]}}`,
	}, nil)
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{"permission:Bash(lighthouse review-run:*)"})
	assertRows(t, "text", got[0].Text, []string{"lighthouse allow Bash(lighthouse review-run:*) tegenover project ask Bash(lighthouse review-run:*)"})
}

func TestPermissionPrefixCovers(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": `{"permissions": {"allow": ["Bash(git push:*)", "Bash(git:*)", "Bash(npm run *)", "Read"]}}`,
	}, map[string]string{
		".claude/settings.json": `{"permissions": {"ask": ["Bash(git push origin main)", "Bash(git-lfs push)", "Bash(npm run build)", "Read(./secret.txt)", "Bash(make)"]}}`,
	})
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{
		"permission:Bash(git push origin main)",
		"permission:Bash(npm run build)",
		"permission:Read(./secret.txt)",
	})
	assertFinding(t, findingOf(t, got, "permission:Bash(git push origin main)"), Finding{
		Key:  "permission:Bash(git push origin main)",
		Kind: "permission",
		Text: []string{
			"project allow Bash(git push:*) tegenover gebruiker ask Bash(git push origin main)",
			"project allow Bash(git:*) tegenover gebruiker ask Bash(git push origin main)",
		},
		Sources: []string{"gebruiker", "project"},
	})
}

func TestPermissionAllowNarrowerThanDeny(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.local.json": `{"permissions": {"allow": ["Bash(rm -rf build)"]}}`,
	}, map[string]string{
		".claude/settings.json": `{"permissions": {"deny": ["Bash(rm:*)"]}}`,
	})
	assertRows(t, "keys", keys(Conflicts(inv, env)), []string{"permission:Bash(rm -rf build)"})
}

func TestNoPermissionWithinOneSource(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": `{"permissions": {"allow": ["Bash(git push:*)"], "deny": ["Bash(git push origin main)"]}}`,
	}, nil)
	if got := Conflicts(inv, env); len(got) != 0 {
		t.Errorf("conflicts %q; want none", keys(got))
	}
}

func TestMissingScript(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/gone.sh`},
			[3]string{"PostToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/gone.sh`},
			[3]string{"Stop", "", "echo inline; exit 2"},
			[3]string{"Stop", "", ".claude/hooks/here.sh"},
		),
		".claude/hooks/here.sh": "#!/bin/sh\n",
	}, nil)
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{`missing-script:project/"$CLAUDE_PROJECT_DIR"/.claude/hooks/gone.sh`})
	assertFinding(t, got[0], Finding{
		Key:  `missing-script:project/"$CLAUDE_PROJECT_DIR"/.claude/hooks/gone.sh`,
		Kind: "missing-script",
		Text: []string{
			"PreToolUse Bash: project .claude/hooks/gone.sh (in .claude/settings.json) bestaat niet",
			"PostToolUse Bash: project .claude/hooks/gone.sh (in .claude/settings.json) bestaat niet",
		},
		Sources: []string{"project"},
	})
}

func TestMissingRunShName(t *testing.T) {
	cmd := func(name string) string {
		return `"$CLAUDE_PROJECT_DIR"/.claude/hooks/lighthouse/run.sh ` + name
	}
	t.Run("unknown name", func(t *testing.T) {
		inv, env := collected(t, map[string]string{
			".claude/settings.json": registeredMerged(
				[3]string{"PreToolUse", "Bash", cmd("onbekend")},
				[3]string{"PreToolUse", "Bash", cmd("main-commit-gate")},
			),
			".claude/hooks/lighthouse/run.sh": "#!/bin/sh\n",
		}, nil)
		env.HookNames = []string{"main-commit-gate", "pr-create-gate"}
		got := Conflicts(inv, env)
		requireKeys(t, got, []string{"missing-script:lighthouse/" + cmd("onbekend")})
		assertFinding(t, got[0], Finding{
			Key:     "missing-script:lighthouse/" + cmd("onbekend"),
			Kind:    "missing-script",
			Text:    []string{"PreToolUse Bash: lighthouse .claude/hooks/lighthouse/run.sh (in .claude/settings.json): de binary kent geen hook onbekend"},
			Sources: []string{"lighthouse"},
		})
	})
	t.Run("no run.sh", func(t *testing.T) {
		inv, env := collected(t, map[string]string{
			".claude/settings.json": registered([3]string{"PreToolUse", "Bash", cmd("main-commit-gate")}),
		}, nil)
		env.HookNames = []string{"main-commit-gate"}
		assertRows(t, "keys", keys(Conflicts(inv, env)), []string{"missing-script:lighthouse/" + cmd("main-commit-gate")})
	})
}

func TestUnregisteredScript(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/used.sh"},
			[3]string{"PostToolUse", "Bash", ".claude/hooks/used.sh"},
		),
		".claude/hooks/used.sh":             "#!/bin/sh\n",
		".claude/hooks/oud.sh":              "#!/bin/sh\nexit 2\n",
		".claude/hooks/lighthouse/stray.sh": "#!/bin/sh\n", // lighthouse's directory: not reported
	}, map[string]string{
		".claude/hooks/forgotten.sh": "#!/bin/sh\n",
	})
	got := Conflicts(inv, env)
	requireKeys(t, got, []string{
		"unregistered-script:gebruiker/~/.claude/hooks/forgotten.sh",
		"unregistered-script:project/.claude/hooks/oud.sh",
	})
	assertFinding(t, findingOf(t, got, "unregistered-script:project/.claude/hooks/oud.sh"), Finding{
		Key:     "unregistered-script:project/.claude/hooks/oud.sh",
		Kind:    "unregistered-script",
		Text:    []string{"project .claude/hooks/oud.sh: geen hook noemt dit script"},
		Sources: []string{"project"},
	})
}

func TestHelperIsNotUnregistered(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/main.sh"},
			[3]string{"Stop", "", `echo "$(cat .claude/hooks/banner.txt)"`},
		),
		".claude/hooks/main.sh":       "#!/bin/sh\n. \"$(dirname \"$0\")/lib/common.sh\"\n",
		".claude/hooks/lib/common.sh": "check() { :; }\n",
		".claude/hooks/banner.txt":    "hello\n",
		".claude/hooks/lib/mon.sh":    "#!/bin/sh\n", // a suffix of common.sh, named nowhere
	}, nil)
	assertRows(t, "keys", keys(Conflicts(inv, env)), []string{
		"unregistered-script:project/.claude/hooks/lib/mon.sh",
	})
}

func TestHelperRequiredWithoutExtension(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json":           registered([3]string{"PreToolUse", "Bash", "node .claude/hooks/main.js"}),
		".claude/hooks/main.js":           "const cfg = require('./caveman-config');\nconst lib = require(\"./lib/util\")\n",
		".claude/hooks/caveman-config.js": "module.exports = {};\n",
		".claude/hooks/lib/util.js":       "module.exports = {};\n",
		".claude/hooks/caveman.js":        "//\n", // a prefix of caveman-config, named nowhere
		".claude/hooks/config.js":         "//\n", // a suffix of caveman-config, named nowhere
	}, nil)
	assertRows(t, "keys", keys(Conflicts(inv, env)), []string{
		"unregistered-script:project/.claude/hooks/caveman.js",
		"unregistered-script:project/.claude/hooks/config.js",
	})
}

func TestUnreadableHookIsNotClean(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every file")
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "main-commit-guard.sh"))
	if err != nil {
		t.Fatal(err)
	}
	home, configDir := hostless(t)
	root := tree(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/locked.sh"},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/other.sh"},
			[3]string{"PreToolUse", "Bash", ".claude/hooks/gone.sh"},
		),
		".claude/hooks/locked.sh": string(fixture),
		".claude/hooks/other.sh":  blockSh,
	})
	locked := filepath.Join(root, ".claude", "hooks", "locked.sh")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	env := Env{Root: root, MainRoot: root, Home: home, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json")}
	inv, err := Collect(env)
	if err != nil {
		t.Fatal(err)
	}

	if got := Overlap(inv, coreGuards(t)); len(got) != 0 {
		t.Errorf("overlap %q; want none for an unreadable script", keys(got))
	}
	// Neither a blocking pair with other.sh (unknown is not blocking) nor silence.
	assertRows(t, "conflicts", keys(Conflicts(inv, env)), []string{`missing-script:project/.claude/hooks/gone.sh`})
	statuses := HookStatuses(inv)
	if len(statuses) != 1 {
		t.Fatalf("hook statuses %+v; want one for locked.sh", statuses)
	}
	s := statuses[0]
	if s.Source != "project/.claude/hooks/locked.sh" || s.State != StateCouldNotCh || !strings.Contains(s.Reason, "permission denied") {
		t.Errorf("status %+v; want project/.claude/hooks/locked.sh COULD NOT CHECK with the read error", s)
	}
}

func TestNoMissingScriptForInlineCode(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"Stop", "", `bash -c 'source .claude/hooks/gone.sh && check'`},
			[3]string{"Stop", "", `sh -c "cd x && ./y.sh"`},
		),
		"x/y.sh": "#!/bin/sh\n",
	}, nil)
	if got := Conflicts(inv, env); len(got) != 0 {
		t.Errorf("conflicts %q; want none for code after -c", keys(got))
	}
}

func TestNoBlockingPairWithItself(t *testing.T) {
	inv, env := collected(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/guard.sh"},
			[3]string{"Stop", "", "echo no; exit 2"},
			[3]string{"Stop", "", "echo no; exit 2"},
		),
		".claude/settings.local.json": registered([3]string{"PreToolUse", "Bash", `"$CLAUDE_PROJECT_DIR"/.claude/hooks/guard.sh`}),
		".claude/hooks/guard.sh":      blockSh,
	}, nil)
	if got := Conflicts(inv, env); len(got) != 0 {
		t.Errorf("conflicts %q; want none for one script registered twice", keys(got))
	}
}
