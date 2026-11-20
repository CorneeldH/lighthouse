package packcontract

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/checkinstr"
	"github.com/CorneeldH/lighthouse/internal/config"
)

var forgeSections = []string{"board", "blockers", "sub-issues", "sluitwoorden", "proxy", "ci", "workflows"}

var forgeKindRe = regexp.MustCompile(`lighthouse config forge\.kind\b`)

var forgeRefRe = regexp.MustCompile(`\.\./lighthouse-forge/<kind>\.md §([\w-]+)`)

// chainSkills read and write the forge through `lighthouse forge` and name a lighthouse-forge
// section for what differs per forge.
var chainSkills = []string{"start-issue", "route-issue", "write-issue", "write-pr"}

// Classification: fails when a chain skill loses its pointer to a lighthouse-forge section, names
// a section that does not exist, or stops reading the forge kind from config.
func TestChainSkillsPointAtForgeReference(t *testing.T) {
	for _, name := range chainSkills {
		for variant, text := range variants(t, "core/skills/"+name) {
			t.Run(name+"/"+variant, func(t *testing.T) {
				refs := forgeRefRe.FindAllStringSubmatch(text, -1)
				if len(refs) == 0 {
					t.Fatal("names no `../lighthouse-forge/<kind>.md §<section>`")
				}
				for _, ref := range refs {
					if !slices.Contains(forgeSections, ref[1]) {
						t.Errorf("unknown forge section %q", ref[1])
					}
				}
				if !forgeKindRe.MatchString(text) {
					t.Error("does not read `lighthouse config forge.kind`")
				}
			})
		}
	}
}

// units lists every skill directory and every agent file of pack/core, relative to packRoot.
func units(t *testing.T) []string {
	t.Helper()
	agents, err := filepath.Glob(filepath.Join(packRoot, "core", "agents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	all := skillDirs(t)
	for _, a := range agents {
		all = append(all, "core/agents/"+filepath.Base(a))
	}
	return all
}

// packOf is the package a unit (relative to packRoot) belongs to.
func packOf(unit string) string {
	pkg, _, _ := strings.Cut(unit, "/")
	return pkg
}

// visible are the packages whose files a unit may name without "if present": core and its own.
func visible(unit string) []string {
	if packOf(unit) == "core" {
		return []string{"core"}
	}
	return []string{"core", packOf(unit)}
}

// sortedTexts returns the texts of a unit in a stable order.
func sortedTexts(t *testing.T, unit string) ([]string, map[string]string) {
	t.Helper()
	texts := unitTexts(t, unit)
	rels := make([]string, 0, len(texts))
	for rel := range texts {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels, texts
}

// joinedText is every text file of a unit, joined with newlines.
func joinedText(t *testing.T, unit string) string {
	t.Helper()
	rels, texts := sortedTexts(t, unit)
	parts := make([]string, 0, len(rels))
	for _, rel := range rels {
		parts = append(parts, texts[rel])
	}
	return strings.Join(parts, "\n")
}

var paragraphSplit = regexp.MustCompile(`\n\s*\n`)

// guarded is a paragraph that names a unit of another package only "if present".
func guarded(para string) bool {
	return strings.Contains(strings.ToLower(flat(para)), "if present")
}

func isFile(rel string) bool {
	info, err := os.Stat(filepath.Join(packRoot, rel))
	return err == nil && info.Mode().IsRegular()
}

// pathRef maps an installed path a unit names to where it lives in a package directory.
type pathRef struct {
	re     *regexp.Regexp
	target func(m []string, pkg string) string
}

var pathRefs = []pathRef{
	{regexp.MustCompile(`\.lighthouse/scripts/([\w./-]*\w)`),
		func(m []string, pkg string) string { return path.Join(pkg, "scripts", m[1]) }},
	{regexp.MustCompile(`\.claude/hooks/lighthouse/([\w.-]+)`),
		func(m []string, pkg string) string { return path.Join(pkg, "hooks", m[1]) }},
	{regexp.MustCompile(`\.claude/agents/lighthouse-([\w-]+)\.md`),
		func(m []string, pkg string) string { return path.Join(pkg, "agents", m[1]+".md") }},
	{regexp.MustCompile(`\.(?:claude|agents)/skills/lighthouse-([a-z0-9-]+)/([\w./-]*\w)`),
		func(m []string, pkg string) string {
			shared := path.Join(pkg, "skills", m[1], m[2])
			if m[2] == "SKILL.md" && !isFile(shared) {
				return path.Join(pkg, "skills", m[1], "claude", "SKILL.md")
			}
			return shared
		}},
}

// Classification: fails when a unit names an installed script, hook, agent or skill file that
// neither pack/core nor its own package has, outside a paragraph that says "if present".
func TestReferencedFilesExist(t *testing.T) {
	hits := 0
	for _, unit := range units(t) {
		t.Run(unit, func(t *testing.T) {
			rels, texts := sortedTexts(t, unit)
			for _, rel := range rels {
				for _, para := range paragraphSplit.Split(texts[rel], -1) {
					for _, ref := range pathRefs {
						for _, m := range ref.re.FindAllStringSubmatch(para, -1) {
							hits++
							found := false
							for _, pkg := range visible(unit) {
								found = found || isFile(ref.target(m, pkg))
							}
							if !found && !guarded(para) {
								t.Errorf("%s: %s", rel, m[0])
							}
						}
					}
				}
			}
		})
	}
	if hits == 0 {
		t.Error("no installed path found in any unit: the patterns see nothing")
	}
}

var configRe = regexp.MustCompile("\\blighthouse config\\s+(?:--json\\s+)?(\"[^\"]+\"|'[^']+'|[^\\s`\"')|;]+)")

// Classification: fails when a unit calls `lighthouse config <key>` with a key the reader does
// not know, or when the pattern stops seeing the keys route-issue reads.
func TestConfigKeysKnown(t *testing.T) {
	for _, unit := range units(t) {
		t.Run(unit, func(t *testing.T) {
			rels, texts := sortedTexts(t, unit)
			for _, rel := range rels {
				for _, m := range configRe.FindAllStringSubmatch(texts[rel], -1) {
					key := strings.Trim(m[1], `"'`)
					if key == "host-check" || strings.HasPrefix(key, "$") || strings.HasPrefix(key, "-") ||
						strings.Contains(key, "<") {
						continue
					}
					if !config.IsKnownKey(key) {
						t.Errorf("%s: unknown config key %q", rel, key)
					}
				}
			}
		})
	}
	seen := map[string]bool{}
	for _, m := range configRe.FindAllStringSubmatch(joinedText(t, "core/skills/route-issue"), -1) {
		seen[m[1]] = true
	}
	for _, want := range []string{"labels.blocked_issue", "labels.needs_shaping", "forge.kind"} {
		if !seen[want] {
			t.Errorf("route-issue: pattern does not see `lighthouse config %s`", want)
		}
	}
}

var headingRe = regexp.MustCompile(`heading:([A-Za-z0-9_]+)`)

// Classification: fails when a unit names a `heading:<key>` that pack/core/headings.toml lacks,
// so `lighthouse headings <key>` would print nothing.
func TestHeadingKeysKnown(t *testing.T) {
	var table map[string]any
	if _, err := toml.Decode(readPack(t, "core/headings.toml"), &table); err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, unit := range units(t) {
		t.Run(unit, func(t *testing.T) {
			rels, texts := sortedTexts(t, unit)
			for _, rel := range rels {
				for _, m := range headingRe.FindAllStringSubmatch(texts[rel], -1) {
					hits++
					if _, ok := table[m[1]]; !ok {
						t.Errorf("%s: unknown heading key %q", rel, m[1])
					}
				}
			}
		})
	}
	if hits == 0 {
		t.Error("no heading:<key> found in any unit")
	}
}

// allowedExternal are the skills outside lighthouse a unit may call: the Superpowers skills and
// two Claude Code built-in commands.
var allowedExternal = []string{
	"clear",
	"code-review",
	"superpowers:brainstorming",
	"superpowers:dispatching-parallel-agents",
	"superpowers:executing-plans",
	"superpowers:finishing-a-development-branch",
	"superpowers:receiving-code-review",
	"superpowers:requesting-code-review",
	"superpowers:subagent-driven-development",
	"superpowers:systematic-debugging",
	"superpowers:test-driven-development",
	"superpowers:using-git-worktrees",
	"superpowers:verification-before-completion",
	"superpowers:writing-plans",
	"superpowers:writing-skills",
}

// lighthouseSkillRe is `/lighthouse-<name>` not preceded by a path character; a match followed
// by `/` is a path (`<git-common-dir>/lighthouse-tools/`), not a skill, and is skipped.
var lighthouseSkillRe = regexp.MustCompile(`(?:^|[^\w./-])/lighthouse-([a-z0-9-]+)`)

// externalSkillRes are `/name` alone in backticks (optionally with arguments) and a namespaced
// `/ns:name` not preceded by a path character.
var externalSkillRes = []*regexp.Regexp{
	regexp.MustCompile("`/([a-z][a-z0-9-]*(?::[a-z0-9-]+)?)(?:\\s[^`]*)?`"),
	regexp.MustCompile(`(?:^|[^\w./~-])/([a-z][a-z0-9-]*:[a-z0-9-]+)`),
}

// Classification: fails when a unit links a lighthouse skill that its packages do not have
// (outside "if present"), or calls an external skill that is not on the allowlist.
func TestSkillLinksResolve(t *testing.T) {
	dirs := skillDirs(t)
	lighthouseHits, externalHits := 0, 0
	for _, unit := range units(t) {
		t.Run(unit, func(t *testing.T) {
			known := map[string]bool{}
			for _, d := range dirs {
				if slices.Contains(visible(unit), packOf(d)) {
					known[path.Base(d)] = true
				}
			}
			rels, texts := sortedTexts(t, unit)
			for _, rel := range rels {
				for _, para := range paragraphSplit.Split(texts[rel], -1) {
					for _, idx := range lighthouseSkillRe.FindAllStringSubmatchIndex(para, -1) {
						if idx[1] < len(para) && para[idx[1]] == '/' {
							continue
						}
						lighthouseHits++
						name := para[idx[2]:idx[3]]
						if !known[name] && !guarded(para) {
							t.Errorf("%s: /lighthouse-%s", rel, name)
						}
					}
				}
				for _, re := range externalSkillRes {
					for _, m := range re.FindAllStringSubmatch(texts[rel], -1) {
						if strings.HasPrefix(m[1], "lighthouse-") {
							continue
						}
						externalHits++
						if !slices.Contains(allowedExternal, m[1]) {
							t.Errorf("%s: /%s", rel, m[1])
						}
					}
				}
			}
		})
	}
	if lighthouseHits == 0 || externalHits == 0 {
		t.Errorf("links seen: %d lighthouse, %d external; both patterns must see one", lighthouseHits, externalHits)
	}
}

var (
	optionalRe = regexp.MustCompile(`(?i)backlog-stand|board-sweep|\brelease\b|pre-push|\bcontroller\b`)
	guardRe    = regexp.MustCompile(`(?i)if present|skipped`)
	headingMd  = regexp.MustCompile(`^(#{1,6})\s`)
)

// enclosedParagraph is a paragraph with the markdown headings that enclose it.
type enclosedParagraph struct {
	text      string
	enclosing []string
}

// paragraphs splits text into paragraphs, each with its enclosing headings (outside fences).
func paragraphs(text string) []enclosedParagraph {
	type heading struct {
		level int
		line  string
	}
	var headings []heading
	var out []enclosedParagraph
	inFence := false
	for _, para := range paragraphSplit.Split(text, -1) {
		enclosing := make([]string, 0, len(headings))
		for _, h := range headings {
			enclosing = append(enclosing, h.line)
		}
		out = append(out, enclosedParagraph{para, enclosing})
		for line := range strings.SplitSeq(para, "\n") {
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
				inFence = !inFence
				continue
			}
			m := headingMd.FindStringSubmatch(line)
			if m == nil || inFence {
				continue
			}
			level := len(m[1])
			headings = slices.DeleteFunc(headings, func(h heading) bool { return h.level >= level })
			headings = append(headings, heading{level, line})
		}
	}
	return out
}

// Classification: fails when a core unit names a step that stays in paia (backlog-stand,
// board-sweep, release, pre-push, controller) without "if present" or "skipped" in the paragraph
// or in a heading that encloses it.
func TestOptionalStepsGuarded(t *testing.T) {
	hits := 0
	for _, unit := range units(t) {
		if packOf(unit) != "core" {
			continue
		}
		t.Run(unit, func(t *testing.T) {
			rels, texts := sortedTexts(t, unit)
			for _, rel := range rels {
				for _, p := range paragraphs(texts[rel]) {
					if !optionalRe.MatchString(p.text) {
						continue
					}
					hits++
					if guardRe.MatchString(flat(p.text)) ||
						slices.ContainsFunc(p.enclosing, guardRe.MatchString) {
						continue
					}
					for line := range strings.SplitSeq(p.text, "\n") {
						if optionalRe.MatchString(line) {
							t.Errorf("%s: %s", rel, strings.TrimSpace(line))
							break
						}
					}
				}
			}
		})
	}
	if hits == 0 {
		t.Error("no paia step named in any core unit: the pattern sees nothing")
	}
}

// installSkills lays every skill out under .claude/skills and .agents/skills, as init does.
func installSkills(t *testing.T, root string) {
	t.Helper()
	for _, dir := range skillDirs(t) {
		entries, err := os.ReadDir(filepath.Join(packRoot, dir))
		if err != nil {
			t.Fatal(err)
		}
		for variant, text := range variants(t, dir) {
			base := map[string]string{"claude": ".claude/skills", "agents": ".agents/skills"}[variant]
			dest := filepath.Join(root, base, "lighthouse-"+path.Base(dir))
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() {
					continue
				}
				writeFile(t, filepath.Join(dest, e.Name()), readPack(t, path.Join(dir, e.Name())))
			}
			writeFile(t, filepath.Join(dest, "SKILL.md"), text)
		}
	}
}

func writeFile(t *testing.T, name, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Classification: fails when two skill descriptions share five trigger words and neither carries
// a bounding clause, so check 4 of `lighthouse check-instructions` fails in every installed repo.
func TestDescriptionsPassTheOverlapCheck(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "lighthouse.toml"),
		"[project]\nlanguage = \"en\"\n\n[forge]\nkind = \"github\"\nrepo = \"Acme/widgets\"\n")
	writeFile(t, filepath.Join(root, ".lighthouse", "headings.toml"), readPack(t, "core/headings.toml"))
	installSkills(t, root)
	t.Setenv("LIGHTHOUSE_TOML", "")
	var out, errOut bytes.Buffer
	rc := checkinstr.Run(root, filepath.Join(root, "no-personal-skills"), &out, &errOut)
	if rc == 2 || errOut.Len() > 0 {
		t.Fatalf("check-instructions could not check: rc %d, stderr %q", rc, errOut.String())
	}
	if strings.Contains(out.String(), "overlap without a bounding clause") {
		t.Error(out.String())
	}
}

// toolUses is what a skill text does that needs a tool on the machine.
var toolUses = map[string]*regexp.Regexp{
	"superpowers": regexp.MustCompile(`superpowers:`),
	"gh":          regexp.MustCompile(`\bgh (pr|issue|api|project|run|repo|search|label|workflow)\b`),
	"codex":       regexp.MustCompile("(?m)(^|[\\s`$(])codex (exec|review)\\b|codex-usage"),
	// A call (`jq -r`, `| jq '.x'`), not a mention like "no `jq`".
	"jq": regexp.MustCompile(`\bjq\s+[-'".]`),
	"git": regexp.MustCompile(`\bgit (-C|worktree|log|fetch|diff|status|rev-parse|branch|push|commit|grep|show` +
		`|merge-base|checkout|switch|ls-files|add)\b`),
	"python3": regexp.MustCompile(`\bpython3\b`),
	// A lighthouse Python script reads lighthouse.toml through tomllib; `lighthouse config` does not.
	"tomllib": regexp.MustCompile(`python3 \.lighthouse/scripts/|tomllib`),
}

// requires reads pkg/requires.toml as tool -> units, with [forge.<kind>] folded in per tool.
func requires(t *testing.T, pkg string) map[string][]string {
	t.Helper()
	var raw map[string]any
	if _, err := toml.Decode(readPack(t, pkg+"/requires.toml"), &raw); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	add := func(tool string, units any) {
		list, ok := units.([]any)
		if !ok {
			t.Fatalf("%s/requires.toml: %s is not a list", pkg, tool)
		}
		for _, u := range list {
			out[tool] = append(out[tool], u.(string))
		}
	}
	for tool, v := range raw {
		if tool != "forge" {
			add(tool, v)
			continue
		}
		for _, kind := range v.(map[string]any) {
			for ftool, units := range kind.(map[string]any) {
				add(ftool, units)
			}
		}
	}
	return out
}

// Classification: fails when a skill uses a tool (gh, git, jq, codex, python3, …) and its own
// package's requires.toml does not name the skill under that tool, so init would not report it
// missing; or when the python3 and tomllib patterns stop seeing python-standard.
func TestRequiresNamesTheSkillPerTool(t *testing.T) {
	for _, dir := range skillDirs(t) {
		t.Run(dir, func(t *testing.T) {
			req := requires(t, packOf(dir))
			text := joinedText(t, dir)
			for tool, re := range toolUses {
				if re.MatchString(text) && !slices.Contains(req[tool], path.Base(dir)) {
					t.Errorf("requires.toml lacks %s under %s", path.Base(dir), tool)
				}
			}
		})
	}
	standard := joinedText(t, "python/skills/python-standard")
	for _, tool := range []string{"python3", "tomllib"} {
		if !toolUses[tool].MatchString(standard) {
			t.Errorf("the %s pattern does not see python-standard", tool)
		}
	}
}

// Classification: fails when requires.toml names a skill under python3 or tomllib that no longer
// uses it, so init reports a missing tool the skill does not need.
func TestRequiresPythonOnlyWhereTheSkillUsesIt(t *testing.T) {
	for _, dir := range skillDirs(t) {
		t.Run(dir, func(t *testing.T) {
			req := requires(t, packOf(dir))
			text := joinedText(t, dir)
			for _, tool := range []string{"python3", "tomllib"} {
				if slices.Contains(req[tool], path.Base(dir)) && !toolUses[tool].MatchString(text) {
					t.Errorf("requires.toml names %s under %s, the skill does not use it", path.Base(dir), tool)
				}
			}
		})
	}
}

var (
	ghWriteRe  = regexp.MustCompile(`gh (?:issue|pr) (?:create|edit|comment|reopen|close|merge|ready|review)\b`)
	codeSpanRe = regexp.MustCompile("`([^`]*)`")
	flagOrArg  = regexp.MustCompile(`--|<`)
)

// Classification: fails when a core skill or the PR template writes through `gh issue|pr <verb>`
// without `--repo`, so gh picks the upstream repo in a clone of a fork.
func TestEveryWritingGhCommandNamesTheRepo(t *testing.T) {
	var files []string
	err := filepath.WalkDir(filepath.Join(packRoot, "core", "skills"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			rel, _ := filepath.Rel(packRoot, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "core/templates/pull_request_template.md")
	commands := 0
	for _, f := range files {
		text := strings.ReplaceAll(readPack(t, f), "\\\n", " ")
		inFence := false
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
				inFence = !inFence
				continue
			}
			spans := []string{line}
			if !inFence {
				spans = nil
				for _, m := range codeSpanRe.FindAllStringSubmatch(line, -1) {
					spans = append(spans, m[1])
				}
			}
			for _, span := range spans {
				loc := ghWriteRe.FindStringIndex(span)
				if loc == nil || !flagOrArg.MatchString(span[loc[1]:]) {
					continue
				}
				commands++
				if !strings.Contains(span, "--repo") {
					t.Errorf("%s: %s", f, strings.TrimSpace(span))
				}
			}
		}
	}
	if commands == 0 {
		t.Error("no writing gh command found: the pattern sees nothing")
	}
}

// Classification: fails when write-pr stops opening the PR with the fenced
// `lighthouse forge pr create --title '<title>' --body-file <path>` that pr-create-gate reads.
func TestWritePRCreatesThePRThroughLighthouseForge(t *testing.T) {
	const want = "lighthouse forge pr create --title '<title>' --body-file <path>"
	for variant, text := range variants(t, "core/skills/write-pr") {
		t.Run(variant, func(t *testing.T) {
			creates := 0
			for line := range strings.SplitSeq(text, "\n") {
				if !strings.HasPrefix(line, "lighthouse forge pr create ") {
					continue
				}
				creates++
				if !strings.HasPrefix(line, want) {
					t.Errorf("create line is %q, want prefix %q", line, want)
				}
			}
			if creates == 0 {
				t.Error("write-pr has no fenced `lighthouse forge pr create` command")
			}
		})
	}
}

var ghForgeCallRe = regexp.MustCompile(`\bgh (api|issue|pr|label|project)\b`)

// ghExceptions are lines of a chain skill that may name a `gh` forge form: the evidence allowlist
// of write-pr and what pr-create-gate recognises.
var ghExceptions = map[string][]string{
	"write-pr": {
		"| `gh api <path>` |",
		"| `gh api graphql` |",
		"| `gh issue view`, `gh pr view` |",
		"`gh pr create` and `glab mr create`",
	},
}

// Classification: fails when a chain skill calls gh for a forge operation instead of
// `lighthouse forge`, or when an allowed exception no longer occurs (a stale exception).
func TestChainSkillsDoNotCallGhForForgeOperations(t *testing.T) {
	for _, name := range chainSkills {
		t.Run(name, func(t *testing.T) {
			rels, texts := sortedTexts(t, "core/skills/"+name)
			if len(rels) == 0 {
				t.Fatal("no text files")
			}
			allowed := ghExceptions[name]
			used := map[string]bool{}
			for _, rel := range rels {
				for line := range strings.SplitSeq(texts[rel], "\n") {
					if !ghForgeCallRe.MatchString(line) {
						continue
					}
					hit := false
					for _, frag := range allowed {
						if strings.Contains(line, frag) {
							used[frag], hit = true, true
						}
					}
					if !hit {
						t.Errorf("%s: %s", rel, strings.TrimSpace(line))
					}
				}
			}
			for _, frag := range allowed {
				if !used[frag] {
					t.Errorf("stale exception: %q", frag)
				}
			}
		})
	}
}

var htmlComment = regexp.MustCompile(`<!--.*?-->`)

// h2OutsideFences lists the H2 headings outside code fences, without a trailing comment.
func h2OutsideFences(text string) []string {
	var headings []string
	inFence := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(strings.TrimLeft(line, " \t"), "```"):
			inFence = !inFence
		case !inFence && strings.HasPrefix(line, "## "):
			headings = append(headings, strings.TrimRight(htmlComment.ReplaceAllString(line, ""), " \t"))
		}
	}
	return headings
}

// Classification: fails when the GitHub and GitLab references of lighthouse-forge stop carrying
// the sections a chain skill names, in the same order.
func TestForgeSkillHasBothReferences(t *testing.T) {
	want := make([]string, 0, len(forgeSections))
	for _, s := range forgeSections {
		want = append(want, "## §"+s)
	}
	for _, kind := range []string{"github", "gitlab"} {
		got := h2OutsideFences(readPack(t, "core/skills/forge/"+kind+".md"))
		if !slices.Equal(got, want) {
			t.Errorf("%s.md: H2s %q, want %q", kind, got, want)
		}
	}
}

var onlyRe = regexp.MustCompile(`\b(only|alleen)\b`)

// Classification: fails when the description of lighthouse-forge stops saying it is read only via
// another skill, so it starts triggering on its own.
func TestForgeSkillDescriptionNotSelfTriggering(t *testing.T) {
	for variant, text := range variants(t, "core/skills/forge") {
		t.Run(variant, func(t *testing.T) {
			desc := ""
			for line := range strings.SplitSeq(frontmatter(t, text), "\n") {
				if strings.HasPrefix(line, "description:") {
					desc = line
					break
				}
			}
			if !strings.Contains(desc, "via another skill") || !onlyRe.MatchString(desc) {
				t.Errorf("description does not bound itself: %q", desc)
			}
		})
	}
}

var (
	glabWord    = regexp.MustCompile(`\bglab\b`)
	glabCommand = regexp.MustCompile(`(^|[\s;&|$(])glab\b`)
)

// Classification: fails when lighthouse-forge names `glab` outside gitlab.md, or as a command in
// gitlab.md (a fenced line or an inline code span), making glab a dependency.
func TestForgeReferenceMentionsNoGlabCommand(t *testing.T) {
	rels, texts := sortedTexts(t, "core/skills/forge")
	if _, ok := texts["gitlab.md"]; !ok {
		t.Fatal("forge/gitlab.md missing")
	}
	for _, rel := range rels {
		inFence := false
		for line := range strings.SplitSeq(texts[rel], "\n") {
			if rel != "gitlab.md" {
				if glabWord.MatchString(line) {
					t.Errorf("%s: %s", rel, strings.TrimSpace(line))
				}
				continue
			}
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "```") {
				inFence = !inFence
				continue
			}
			spans := []string{line}
			if !inFence {
				spans = nil
				for _, m := range codeSpanRe.FindAllStringSubmatch(line, -1) {
					spans = append(spans, m[1])
				}
			}
			for _, s := range spans {
				if glabCommand.MatchString(s) {
					t.Errorf("%s: %s", rel, strings.TrimSpace(s))
				}
			}
		}
	}
}

var issueNumberRe = regexp.MustCompile(`#\d{3,}`)

// Classification: fails when forge or start-issue names an issue number of this repo, which
// resolves against another repo's forge once installed there.
func TestForgeSkillsNameNoSourceIssueNumber(t *testing.T) {
	for _, name := range []string{"forge", "start-issue"} {
		t.Run(name, func(t *testing.T) {
			rels, texts := sortedTexts(t, "core/skills/"+name)
			if len(rels) == 0 {
				t.Fatal("no text files")
			}
			for _, rel := range rels {
				for _, m := range issueNumberRe.FindAllString(texts[rel], -1) {
					t.Errorf("%s: %s", rel, m)
				}
			}
		})
	}
}
