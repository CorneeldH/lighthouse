package canary

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/hook"
	// The blank imports register the core hooks' reasons with internal/hook.
	_ "github.com/CorneeldH/lighthouse/internal/codelang"
	_ "github.com/CorneeldH/lighthouse/internal/commitgate"
	_ "github.com/CorneeldH/lighthouse/internal/hooks"
)

// packFile is the path of a file under pack/, relative to the repository root.
func packFile(rel string) string { return filepath.Join("..", "..", "pack", rel) }

var (
	blockingLine  = regexp.MustCompile(`(?m)^BLOCKING_RULES="([^"]*)"[ \t]*$`)
	nonLinterLine = regexp.MustCompile(`(?m)^NON_LINTER_RULES="([^"]*)"[ \t]*$`)
)

// parseRuleLine reads the single line of text that matches line, as a list of IDs; with
// optional, no line at all is an empty list. Several lines, an empty ID or a duplicate ID is an error.
func parseRuleLine(text string, line *regexp.Regexp, name string, optional bool) ([]string, error) {
	found := line.FindAllStringSubmatch(text, -1)
	switch {
	case len(found) == 0 && optional:
		return nil, nil
	case len(found) == 0:
		return nil, fmt.Errorf("no %s line", name)
	case len(found) > 1:
		return nil, fmt.Errorf("%d %s lines, want one", len(found), name)
	}
	var ids []string
	for _, id := range strings.Split(found[0][1], ",") {
		switch {
		case id == "":
			return nil, fmt.Errorf("%s holds an empty ID", name)
		case slices.Contains(ids, id):
			return nil, fmt.Errorf("%s holds %q twice", name, id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func readRules(t *testing.T, hookFile string, line *regexp.Regexp, name string, optional bool) []string {
	t.Helper()
	data, err := os.ReadFile(packFile(hookFile))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := parseRuleLine(string(data), line, name, optional)
	if err != nil {
		t.Fatalf("%s: %v", hookFile, err)
	}
	return ids
}

// blockingRules are the IDs on the one BLOCKING_RULES="..." line of hookFile (relative to pack/).
func blockingRules(t *testing.T, hookFile string) []string {
	t.Helper()
	return readRules(t, hookFile, blockingLine, "BLOCKING_RULES", false)
}

// nonLinterRules are the IDs on the NON_LINTER_RULES="..." line of hookFile: the blocks that are no
// linter rule. A hook without that line has none.
func nonLinterRules(t *testing.T, hookFile string) []string {
	t.Helper()
	return readRules(t, hookFile, nonLinterLine, "NON_LINTER_RULES", true)
}

// bijectionProblem says how the files testdata/<hook>/<id>.* differ from ids; "" when they match.
func bijectionProblem(hookName string, ids []string) (string, error) {
	entries, err := os.ReadDir(filepath.Join("testdata", hookName))
	if err != nil {
		return "", err
	}
	var have []string
	for _, e := range entries {
		name := e.Name()
		have = append(have, strings.TrimSuffix(name, filepath.Ext(name)))
	}
	want := slices.Clone(ids)
	sort.Strings(want)
	sort.Strings(have)
	if slices.Equal(want, have) {
		return "", nil
	}
	return fmt.Sprintf("%s: testdata files %v, want one per reason %v", hookName, have, want), nil
}

// assertBijection fails unless the files testdata/<hook>/<id>.* are exactly the ids.
func assertBijection(t *testing.T, hookName string, ids []string) {
	t.Helper()
	problem, err := bijectionProblem(hookName, ids)
	if err != nil {
		t.Fatal(err)
	}
	if problem != "" {
		t.Error(problem)
	}
}

// coreHooks are the hooks of pack/core/hooks/blocking.
func coreHooks(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(packFile("core/hooks/blocking"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func TestCoreReasonsMatchBlockingFile(t *testing.T) {
	declared := hook.DeclaredHooks()
	for _, name := range coreHooks(t) {
		if !slices.Contains(declared, name) {
			t.Errorf("%s is in pack/core/hooks/blocking but declares no reasons", name)
		}
	}
	for _, name := range declared {
		if !slices.Contains(coreHooks(t), name) {
			t.Errorf("%s declares reasons but is not in pack/core/hooks/blocking", name)
		}
	}
}

// Every reason of every core hook has a testdata file.
func TestEveryReasonHasATestdataFile(t *testing.T) {
	for _, name := range hook.DeclaredHooks() {
		ids, _ := hook.Reasons(name)
		if name == "bash-write-check" {
			if len(ids) != 0 {
				t.Errorf("bash-write-check declares %v, want none", ids)
			}
			continue
		}
		have, err := filepath.Glob(filepath.Join("testdata", name, "*"))
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			if !slices.ContainsFunc(have, func(p string) bool { return strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) == id }) {
				t.Errorf("%s/%s has no testdata file", name, id)
			}
		}
	}
}

// ruleListHooks are the hooks outside pack/core that declare their reasons on a BLOCKING_RULES line:
// the name of their testdata directory and the file of the hook, relative to pack/. A task that adds
// such a hook adds it here.
var ruleListHooks = map[string]string{
	"gofmt-check":  "go/hooks/gofmt-check.sh",
	"go-lint-gate": "go/hooks/go-lint-gate.sh",
}

// strayDirs are the directories of testdata that no hook claims: not declared with hook.Reasons and
// not on the list of rule-list hooks.
func strayDirs(dirs, declared []string, listed map[string]string) []string {
	var stray []string
	for _, dir := range dirs {
		if _, ok := listed[dir]; !ok && !slices.Contains(declared, dir) {
			stray = append(stray, dir)
		}
	}
	return stray
}

// Every testdata file of a hook belongs to a declared reason, and every testdata directory to a hook.
func TestEveryTestdataFileHasAReason(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		dirs = append(dirs, e.Name())
	}
	if stray := strayDirs(dirs, hook.DeclaredHooks(), ruleListHooks); len(stray) > 0 {
		t.Errorf("testdata directories without a hook that declares reasons: %v", stray)
	}
	for _, name := range hook.DeclaredHooks() {
		ids, _ := hook.Reasons(name)
		if len(ids) == 0 {
			continue
		}
		assertBijection(t, name, ids)
	}
	for name, file := range ruleListHooks {
		assertBijection(t, name, blockingRules(t, file))
	}
}

func TestStrayDirsSeesADirectoryNoHookClaims(t *testing.T) {
	got := strayDirs([]string{"main-commit-gate", "gofmt-check", "ghost"}, []string{"main-commit-gate"}, ruleListHooks)
	if !slices.Equal(got, []string{"ghost"}) {
		t.Errorf("strayDirs = %v, want [ghost]", got)
	}
}

func TestBijectionProblemSeesAMissingAndAnExtraReason(t *testing.T) {
	for name, ids := range map[string][]string{
		"missing file": {"commit-to-main", "push-to-main", "ghost"},
		"extra file":   {"commit-to-main"},
	} {
		if problem, err := bijectionProblem("main-commit-gate", ids); err != nil || problem == "" {
			t.Errorf("%s: problem %q, error %v, want a problem", name, problem, err)
		}
	}
}

func TestBlockingRulesRejectsMissingEmptyOrDuplicate(t *testing.T) {
	cases := map[string]struct {
		text    string
		wantErr bool
		want    []string
	}{
		"one line":   {text: "x\nBLOCKING_RULES=\"A,B\"\n", want: []string{"A", "B"}},
		"missing":    {text: "echo hi\n", wantErr: true},
		"two lines":  {text: "BLOCKING_RULES=\"A\"\nBLOCKING_RULES=\"B\"\n", wantErr: true},
		"empty ID":   {text: "BLOCKING_RULES=\"A,,B\"\n", wantErr: true},
		"empty list": {text: "BLOCKING_RULES=\"\"\n", wantErr: true},
		"duplicate":  {text: "BLOCKING_RULES=\"A,B,A\"\n", wantErr: true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseRuleLine(c.text, blockingLine, "BLOCKING_RULES", false)
			if (err != nil) != c.wantErr || !slices.Equal(got, c.want) {
				t.Errorf("got %v, %v; want %v, error %v", got, err, c.want, c.wantErr)
			}
		})
	}
}

func TestRuleReadersReadTheRealHooks(t *testing.T) {
	if got := blockingRules(t, "python/hooks/ruff-format-check.sh"); len(got) != 8 || got[0] != "E722" {
		t.Errorf("blockingRules = %v, want the eight ruff rules", got)
	}
	if got := nonLinterRules(t, "python/hooks/ruff-format-check.sh"); got != nil {
		t.Errorf("nonLinterRules = %v, want none: the hook has no such line", got)
	}
	if got, err := parseRuleLine("NON_LINTER_RULES=\"typecheck\"\n", nonLinterLine, "NON_LINTER_RULES", true); err != nil || !slices.Equal(got, []string{"typecheck"}) {
		t.Errorf("a present line: %v, %v", got, err)
	}
}
