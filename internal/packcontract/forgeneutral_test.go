package packcontract

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// forgeNeutralUnits are the skills that no longer call `gh` for any forge operation (#407).
var forgeNeutralUnits = []string{"review-plan", "upgrade", "audit-worktrees", "write-handoff", "fix-pr"}

// forgeOpRe is the `gh` tool pattern of requires.toml plus `release`: `gh run` and `gh release`
// are no forge operation as such, but these units no longer need them either.
var forgeOpRe = regexp.MustCompile(`\bgh (release|pr|issue|api|project|run|repo|search|label|workflow)\b`)

// Classification: fails when a forge-neutral unit has a line that calls `gh` (pr, issue, api,
// project, run, repo, search, label, workflow or release) instead of `lighthouse forge`.
func TestForgeNeutralUnitsCallNoGh(t *testing.T) {
	for _, name := range forgeNeutralUnits {
		t.Run(name, func(t *testing.T) {
			rels, texts := sortedTexts(t, "core/skills/"+name)
			if len(rels) == 0 {
				t.Fatal("no text files")
			}
			for _, rel := range rels {
				for i, line := range strings.Split(texts[rel], "\n") {
					if forgeOpRe.MatchString(line) {
						t.Errorf("%s:%d: %s", rel, i+1, strings.TrimSpace(line))
					}
				}
			}
		})
	}
}

// rawRequires parses a requires.toml without folding [forge.<kind>] into the tool map.
func rawRequires(t *testing.T, rel string) map[string]any {
	t.Helper()
	var raw map[string]any
	if _, err := toml.Decode(readPack(t, rel), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

// Classification: fails when a package's requires.toml lists `gh` as a top-level tool, so init
// would demand it on every forge, or names a forge kind other than github and gitlab.
func TestGhIsRequiredOnlyOnGitHub(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(packRoot, "*", "requires.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no requires.toml found")
	}
	for _, f := range files {
		rel, _ := filepath.Rel(packRoot, f)
		raw := rawRequires(t, filepath.ToSlash(rel))
		if _, ok := raw["gh"]; ok {
			t.Errorf("%s: `gh` is a top-level tool", rel)
		}
		if forge, ok := raw["forge"].(map[string]any); ok {
			for kind := range forge {
				if kind != "github" && kind != "gitlab" {
					t.Errorf("%s: unknown forge kind %q", rel, kind)
				}
			}
		}
	}
}

// Classification: fails when write-handoff or audit-worktrees is named again under
// [forge.github] gh in core/requires.toml, or no longer under the top-level git tool.
func TestRequiresDropsGhForConvertedUnits(t *testing.T) {
	raw := rawRequires(t, "core/requires.toml")
	list := func(v any) []any {
		l, _ := v.([]any)
		return l
	}
	forge, _ := raw["forge"].(map[string]any)
	github, _ := forge["github"].(map[string]any)
	for _, unit := range []string{"write-handoff", "audit-worktrees"} {
		if slices.Contains(list(github["gh"]), any(unit)) {
			t.Errorf("%s is still listed under [forge.github] gh", unit)
		}
		if !slices.Contains(list(raw["git"]), any(unit)) {
			t.Errorf("%s is not listed under git", unit)
		}
	}
}

// goUnitSources are the Go sources of the units converted off `gh`, relative to the repo root.
// forge_label.go is the CLI side of internal/labels.
var goUnitSources = []string{
	"internal/postartefact",
	"internal/labels",
	"internal/worktreeaudit",
	"internal/cli/postartefact.go",
	"internal/cli/worktreeaudit.go",
	"internal/cli/forge_label.go",
}

// goGhAllowlist are the places where `gh` stays legitimate: the token lookup and `ask`. Listed
// so the exception is explicit; they must stay outside goUnitSources.
var goGhAllowlist = []string{"internal/forge/token.go", "internal/cli/ask.go"}

// goGhCallRe matches `gh` as a program name (quoted argument) or `gh <forge command>` in prose
// or strings.
var goGhCallRe = regexp.MustCompile(`"gh"|\bgh\s+(label|api|pr|issue)\b`)

// Classification: fails when a non-test Go source of a converted unit names `gh` as a program or
// runs `gh <forge command>`, when the scan list goes stale, or when an allowlisted file lands in it.
func TestGoUnitsDoNotCallGh(t *testing.T) {
	root := filepath.Dir(packRoot)
	var sources []string
	for _, entry := range goUnitSources {
		full := filepath.Join(root, entry)
		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("%s: %v (the scan list is stale)", entry, err)
		}
		if !info.IsDir() {
			sources = append(sources, entry)
			continue
		}
		matches, err := filepath.Glob(filepath.Join(full, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, "_test.go") {
				rel, _ := filepath.Rel(root, m)
				sources = append(sources, filepath.ToSlash(rel))
			}
		}
	}
	if len(sources) == 0 {
		t.Fatal("no Go sources found: the scan list is stale")
	}
	for _, s := range sources {
		if slices.Contains(goGhAllowlist, s) {
			t.Errorf("%s is allowlisted for `gh` but is in the scan list", s)
		}
		data, err := os.ReadFile(filepath.Join(root, s))
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if goGhCallRe.MatchString(line) {
				t.Errorf("%s:%d: %s", s, i+1, strings.TrimSpace(line))
			}
		}
	}
}
