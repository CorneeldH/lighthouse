package agentaudit

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/pack"
)

// envFor writes a repository tree and a home tree, with HOME and CLAUDE_CONFIG_DIR on
// temporary directories, and returns the Env of them; Run is left to the test.
func envFor(t *testing.T, repo, homeFiles map[string]string) Env {
	t.Helper()
	home, configDir := hostless(t)
	root := tree(t, repo)
	writeFiles(t, home, homeFiles)
	return Env{Root: root, MainRoot: root, Home: home, ConfigDir: configDir, ClaudeJSON: filepath.Join(home, ".claude.json")}
}

// corePack is the real core package, for its guards.
func corePack(t *testing.T) packs.Pack {
	t.Helper()
	sub, err := fs.Sub(pack.Files, "core")
	if err != nil {
		t.Fatal(err)
	}
	p, err := packs.Load(sub, "core", "dev")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// run is Run with its output; it fails the test when Run writes to stderr unless wantErr.
func run(t *testing.T, env Env, o Options) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(env, o, &out, &errOut)
	return code, out.String(), errOut.String()
}

// headings are the lines of out that start in column 0: the section headings and the
// ignore lines.
func headings(out string) []string {
	var got []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != "" && !strings.HasPrefix(line, " ") {
			got = append(got, line)
		}
	}
	return got
}

const oldScript = ".claude/hooks/oud.sh"

// withFinding is a repository whose only finding is an unregistered hook script.
var withFinding = map[string]string{
	".claude/settings.json": `{}`,
	oldScript:               "#!/bin/sh\nexit 0\n",
}

const oldKey = "unregistered-script:project/.claude/hooks/oud.sh"

func TestRunSectionOrder(t *testing.T) {
	env := envFor(t, map[string]string{
		".claude/settings.json": registeredMerged(
			[3]string{"PreToolUse", "Bash", ".claude/hooks/check-branch.sh"},
		),
		".claude/hooks/check-branch.sh": fixture(t),
		oldScript:                       "#!/bin/sh\nexit 0\n",
		".claude/skills/a/SKILL.md":     "---\nname: a\ndescription: abc\n---\n",
	}, nil)
	env.Packs = []packs.Pack{corePack(t)}
	code, out, errOut := run(t, env, Options{Top: 10, MaxDesc: 1024})
	if code != 0 || errOut != "" {
		t.Fatalf("Run = %d, stderr %q; want 0 and no stderr", code, errOut)
	}
	want := []string{
		"Bronnen",
		"Conflicten (1)",
		"Overlap, kandidaten (1)",
		"Oppervlak (skills en agents, altijd geladen)",
		"Inventaris",
	}
	if got := headings(out); !reflect.DeepEqual(got, want) {
		t.Errorf("headings:\ngot  %q\nwant %q\n%s", got, want, out)
	}
	for _, key := range []string{"[" + oldKey + "]", "[overlap:main-commit-gate|project/.claude/hooks/check-branch.sh]"} {
		if !strings.Contains(out, key) {
			t.Errorf("output lacks %s:\n%s", key, out)
		}
	}
}

// TestRunOutput pins the whole text of a small repository with --repo-only.
func TestRunOutput(t *testing.T) {
	env := envFor(t, map[string]string{
		".claude/settings.json":        `{"permissions": {"allow": ["Bash(make:*)"]}, "hooks": {"PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": ".claude/hooks/guard.sh"}]}]}}`,
		".claude/hooks/guard.sh":       "#!/bin/sh\nexit 2\n",
		oldScript:                      "#!/bin/sh\nexit 0\n",
		".claude/skills/kort/SKILL.md": "---\nname: kort\ndescription: abcdefgh\n---\n",
		".claude/agents/lang.md":       "---\nname: lang\ndescription: " + strings.Repeat("x", 20) + "\n---\n",
		".claude/rules/go.md":          "---\npaths: [\"*.go\"]\n---\n",
		".mcp.json":                    `{"mcpServers": {"docs": {"command": "x"}}}`,
	}, nil)
	env.RepoOnly = true
	code, out, _ := run(t, env, Options{Top: 1, MaxDesc: 10})
	if code != 0 {
		t.Fatalf("Run = %d, want 0", code)
	}
	want := `Bronnen
  lighthouse  afwezig
  project     ok
  lokaal      afwezig
  gebruiker   overgeslagen (--repo-only)
  plugin:*    overgeslagen (--repo-only)
  managed     overgeslagen (--repo-only)

Conflicten (1)
  [unregistered-script:project/.claude/hooks/oud.sh]
    project .claude/hooks/oud.sh: geen hook noemt dit script

Overlap, kandidaten (0)
  geen

Oppervlak (skills en agents, altijd geladen)
  bron     aantal  tekens  ~tokens
  project  2       36      9
  Grootste posten
  project  lang  24 tekens
  Description langer dan 10 tekens
  project  lang  20 tekens

Inventaris
  hooks (1)
    project  PreToolUse  Bash  .claude/hooks/guard.sh  script .claude/hooks/guard.sh  blokkerend
  skills, commands en agents (2)
    project  skill  kort  .claude/skills/kort/SKILL.md
    project  agent  lang  .claude/agents/lang.md
  rules (1)
    project  .claude/rules/go.md  paths
  permissies (1)
    project  allow  Bash(make:*)
  mcp-servers (1)
    project  docs  .mcp.json
  hookscripts (2)
    project/.claude/hooks/guard.sh
    project/.claude/hooks/oud.sh
`
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestRunWritesNothing(t *testing.T) {
	env := envFor(t, withFinding, map[string]string{
		".claude/settings.json": `{"permissions": {"deny": ["Bash(make:*)"]}}`,
	})
	before := treeHash(t, env.Root, env.Home)
	for _, o := range []Options{{Top: 10, MaxDesc: 1024, Strict: true}, {Only: []string{"surface"}}} {
		run(t, env, o)
		var out, errOut bytes.Buffer
		Summary(env, o, &out, &errOut)
	}
	if after := treeHash(t, env.Root, env.Home); after != before {
		t.Error("Run or Summary changed the tree")
	}
}

func TestRunExitCodes(t *testing.T) {
	brokenUser := map[string]string{".claude/settings.json": "{not json"}
	unreadableHook := map[string]string{
		".claude/settings.json":   registered([3]string{"PreToolUse", "Bash", ".claude/hooks/secret.sh"}),
		".claude/hooks/secret.sh": "#!/bin/sh\nexit 0\n",
	}
	tests := []struct {
		name       string
		repo, home map[string]string
		repoOnly   bool
		strict     bool
		unreadable string // a repository path made unreadable
		want       int
	}{
		{name: "no finding, strict", repo: map[string]string{".claude/settings.json": `{}`}, repoOnly: true, strict: true, want: 0},
		{name: "finding without strict", repo: withFinding, want: 0},
		{name: "finding with strict", repo: withFinding, repoOnly: true, strict: true, want: 1},
		{name: "source COULD NOT CHECK with strict", repo: map[string]string{".claude/settings.json": `{}`}, home: brokenUser, strict: true, want: 2},
		{name: "source COULD NOT CHECK and a finding with strict", repo: withFinding, home: brokenUser, strict: true, want: 2},
		{name: "source COULD NOT CHECK without strict", repo: withFinding, home: brokenUser, want: 0},
		{name: "only managed COULD NOT CHECK, a finding, strict", repo: withFinding, strict: true, want: 1},
		{name: "only managed COULD NOT CHECK, no finding, strict", repo: map[string]string{".claude/settings.json": `{}`}, strict: true, want: 0},
		{name: "hook COULD NOT CHECK with strict", repo: unreadableHook, repoOnly: true, strict: true, unreadable: ".claude/hooks/secret.sh", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := envFor(t, tt.repo, tt.home)
			env.RepoOnly = tt.repoOnly
			if tt.unreadable != "" {
				p := filepath.Join(env.Root, filepath.FromSlash(tt.unreadable))
				if err := os.Chmod(p, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
				if _, err := os.ReadFile(p); err == nil {
					t.Skip("running as a user who reads a mode-0 file")
				}
			}
			code, out, errOut := run(t, env, Options{Strict: tt.strict, Top: 10, MaxDesc: 1024})
			if code != tt.want {
				t.Errorf("Run = %d, want %d\n%s%s", code, tt.want, out, errOut)
			}
		})
	}
}

func TestRunShowsHookStatus(t *testing.T) {
	env := envFor(t, map[string]string{
		".claude/settings.json":   registered([3]string{"PreToolUse", "Bash", ".claude/hooks/secret.sh"}),
		".claude/hooks/secret.sh": "#!/bin/sh\nexit 0\n",
	}, nil)
	env.RepoOnly = true
	p := filepath.Join(env.Root, ".claude", "hooks", "secret.sh")
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
	if _, err := os.ReadFile(p); err == nil {
		t.Skip("running as a user who reads a mode-0 file")
	}
	_, out, _ := run(t, env, Options{Top: 10, MaxDesc: 1024})
	want := "  hook project/.claude/hooks/secret.sh  COULD NOT CHECK — open " + p + ": permission denied\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

func TestRunIgnore(t *testing.T) {
	env := envFor(t, withFinding, nil)
	env.RepoOnly = true
	code, out, _ := run(t, env, Options{Strict: true, Ignore: []string{oldKey}, Top: 10, MaxDesc: 1024})
	if code != 0 {
		t.Errorf("Run = %d, want 0: the only finding is ignored", code)
	}
	if strings.Contains(out, oldKey) {
		t.Errorf("output shows the ignored key:\n%s", out)
	}
	if !strings.Contains(out, "Conflicten (0)\n") {
		t.Errorf("output lacks Conflicten (0):\n%s", out)
	}
}

func TestRunStaleIgnore(t *testing.T) {
	env := envFor(t, withFinding, nil)
	env.RepoOnly = true
	gone := "unregistered-script:project/.claude/hooks/weg.sh"
	userKey := "unregistered-script:gebruiker/~/.claude/hooks/weg.sh"
	pairKey := "blocking-pair:PreToolUse:Bash:project/.claude/hooks/a.sh|project/.claude/hooks/b.sh"
	// lokaal is absent: a permission conflict may live there on another machine.
	permKey := "permission:Bash(git push:*)"
	// lighthouse is absent: its guard may still match elsewhere.
	overlapKey := "overlap:main-commit-gate|project/.claude/hooks/weg.sh"
	_, out, _ := run(t, env, Options{Ignore: []string{gone, userKey, oldKey, pairKey, permKey, overlapKey}, Top: 10, MaxDesc: 1024})
	got := []string{}
	for _, line := range headings(out) {
		if strings.HasPrefix(line, "ignore: ") {
			got = append(got, line)
		}
	}
	want := []string{
		"ignore: " + gone + " komt niet meer voor",
		"ignore: " + pairKey + " komt niet meer voor",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ignore lines:\ngot  %q\nwant %q\n%s", got, want, out)
	}
}

func TestRunOnlySurface(t *testing.T) {
	env := envFor(t, map[string]string{
		".claude/settings.json":     `{}`,
		oldScript:                   "#!/bin/sh\nexit 0\n",
		".claude/skills/a/SKILL.md": "---\nname: a\ndescription: abc\n---\n",
	}, nil)
	env.RepoOnly = true
	// A stale ignore key belongs to the findings, which --only surface does not show.
	stale := "unregistered-script:project/.claude/hooks/weg.sh"
	code, out, _ := run(t, env, Options{Only: []string{"surface"}, Strict: true, Ignore: []string{stale}, Top: 10, MaxDesc: 1024})
	if code != 0 {
		t.Errorf("Run = %d, want 0: --only surface does not judge conflicts", code)
	}
	want := []string{"Bronnen", "Oppervlak (skills en agents, altijd geladen)"}
	if got := headings(out); !reflect.DeepEqual(got, want) {
		t.Errorf("headings:\ngot  %q\nwant %q\n%s", got, want, out)
	}
}

func TestSummaryHasHint(t *testing.T) {
	env := envFor(t, map[string]string{
		".claude/settings.json": `{}`,
		oldScript:               "#!/bin/sh\nexit 0\n",
	}, nil)
	for i := range 7 {
		writeFiles(t, env.Root, map[string]string{
			".claude/skills/s" + string(rune('a'+i)) + "/SKILL.md": "---\ndescription: " + strings.Repeat("d", i+1) + "\n---\n",
		})
	}
	env.RepoOnly = true
	var out, errOut bytes.Buffer
	if code := Summary(env, Options{Top: 10, MaxDesc: 1024}, &out, &errOut); code != 0 {
		t.Fatalf("Summary = %d, want 0; stderr %q", code, errOut.String())
	}
	text := out.String()
	want := []string{
		"Bronnen",
		"Conflicten (1)",
		"Overlap, kandidaten (0)",
		"Oppervlak (skills en agents, altijd geladen)",
		"Draai /lighthouse-audit-agent-config voor een oordeel over de kandidaten.",
	}
	if got := headings(text); !reflect.DeepEqual(got, want) {
		t.Errorf("headings:\ngot  %q\nwant %q\n%s", got, want, text)
	}
	if !strings.HasSuffix(text, "\nDraai /lighthouse-audit-agent-config voor een oordeel over de kandidaten.\n") {
		t.Errorf("the hint is not the last line:\n%s", text)
	}
	// The five largest posts, not the configured top of ten.
	_, posts, _ := strings.Cut(text, "  Grootste posten\n")
	posts, _, _ = strings.Cut(posts, "\n\n")
	if n := strings.Count(posts, "\n") + 1; n != 5 {
		t.Errorf("Grootste posten has %d lines, want 5:\n%s", n, posts)
	}
}

func TestRunCollectFails(t *testing.T) {
	hostless(t)
	env := Env{Root: filepath.Join(t.TempDir(), "gone")}
	code, out, errOut := run(t, env, Options{})
	if code != 2 || out != "" {
		t.Errorf("Run = %d, stdout %q; want 2 and no stdout", code, out)
	}
	if !strings.HasPrefix(errOut, "agent-audit: COULD NOT CHECK — ") {
		t.Errorf("stderr = %q, want the COULD NOT CHECK line", errOut)
	}
	var sout, serr bytes.Buffer
	if code := Summary(env, Options{}, &sout, &serr); code != 2 || !strings.HasPrefix(serr.String(), "agent-audit: COULD NOT CHECK — ") {
		t.Errorf("Summary = %d, stderr %q; want 2 and the COULD NOT CHECK line", code, serr.String())
	}
}
