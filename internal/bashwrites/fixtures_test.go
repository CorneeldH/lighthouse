//go:build !windows

package bashwrites

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

type fixtureCase struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind"`
	Input   string            `json:"input"`
	Entries [][]any           `json:"entries"`
	Stop    *string           `json:"stop"`
	Since   *int64            `json:"since"`
	RC      int               `json:"rc"`
	Output  string            `json:"output"`
	Payload map[string]any    `json:"payload"`
	Setup   []map[string]any  `json:"setup"`
	Env     map[string]string `json:"env"`
	Command string            `json:"command"`
	CWD     string            `json:"cwd"`
	Roots   []string          `json:"roots"`
	Project string            `json:"project"`
	Expect  json.RawMessage   `json:"expect"`
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/bash_writes/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			switch c.Kind {
			case "porcelain", "diff_tree":
				var want struct{ Changed, Deleted []string }
				mustJSON(t, c.Expect, &want)
				parse := ParsePorcelain
				if c.Kind == "diff_tree" {
					parse = ParseDiffTree
				}
				changed, deleted := parse([]byte(c.Input))
				eq(t, "changed", sortedKeys(changed), sortedCopy(want.Changed))
				eq(t, "deleted", sortedKeys(deleted), sortedCopy(want.Deleted))
			case "reflog":
				var want [][]any
				mustJSON(t, c.Expect, &want)
				got, err := ParseReflog(c.Input)
				if err != nil {
					t.Fatal(err)
				}
				var gotRows [][]any
				for _, e := range got {
					gotRows = append(gotRows, []any{e.SHA, float64(e.Epoch), e.Subject})
				}
				eq(t, "entries", gotRows, want)
			case "commits":
				var want struct {
					Commits [][]any
					Notes   []string
				}
				mustJSON(t, c.Expect, &want)
				var entries []ReflogEntry
				for _, e := range c.Entries {
					entries = append(entries, ReflogEntry{e[0].(string), int64(e[1].(float64)), e[2].(string)})
				}
				commits, notes := CommitsToCheck(entries, c.Stop, c.Since)
				gotRows := [][]any{}
				for _, cm := range commits {
					gotRows = append(gotRows, []any{cm.SHA, cm.Merge})
				}
				eq(t, "commits", gotRows, want.Commits)
				eq(t, "notes", append([]string{}, notes...), append([]string{}, want.Notes...))
			case "classify":
				var want string
				mustJSON(t, c.Expect, &want)
				eq(t, "classify", Classify(c.RC, []byte(c.Output)), want)
			case "marker_key":
				var want string
				mustJSON(t, c.Expect, &want)
				payload := map[string]json.RawMessage{}
				for k, v := range c.Payload {
					b, _ := json.Marshal(v)
					payload[k] = b
				}
				got, err := MarkerKey(payload)
				if err != nil {
					t.Fatal(err)
				}
				eq(t, "marker key", got, want)
			case "derive_roots":
				root := t.TempDir()
				fixturesetup.Build(t, root, c.Setup)
				for k, v := range c.Env {
					t.Setenv(k, fixturesetup.Fill(t, v, root))
				}
				var want []string
				mustJSON(t, c.Expect, &want)
				for i := range want {
					want[i] = fixturesetup.Fill(t, want[i], root)
				}
				got := DeriveRoots(fixturesetup.Fill(t, c.Command, root), fixturesetup.Fill(t, c.CWD, root))
				eq(t, "roots", sortedKeys(got), sortedCopy(want))
			case "project_filter":
				root := t.TempDir()
				fixturesetup.Build(t, root, c.Setup)
				roots := map[string]bool{}
				for _, r := range c.Roots {
					roots[fixturesetup.Fill(t, r, root)] = true
				}
				kept, errs, err := ProjectFilter(roots, fixturesetup.Fill(t, c.Project, root), time.Now().Add(5*time.Second))
				var word string
				if json.Unmarshal(c.Expect, &word) == nil && word == "raises" {
					var ge *GitError
					if !errors.As(err, &ge) {
						t.Fatalf("err = %v, want *GitError", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want struct {
					Kept          []string
					ErrorPrefixes []string `json:"error_prefixes"`
				}
				mustJSON(t, c.Expect, &want)
				for i := range want.Kept {
					want.Kept[i] = fixturesetup.Fill(t, want.Kept[i], root)
				}
				eq(t, "kept", sortedKeys(kept), sortedCopy(want.Kept))
				if len(errs) != len(want.ErrorPrefixes) {
					t.Fatalf("errors = %v, want %d", errs, len(want.ErrorPrefixes))
				}
				sort.Strings(errs)
				for i, p := range sortedCopy(want.ErrorPrefixes) {
					if !strings.HasPrefix(errs[i], fixturesetup.Fill(t, p, root)) {
						t.Fatalf("error %q lacks prefix %q", errs[i], p)
					}
				}
			default:
				t.Fatalf("unknown kind %q", c.Kind)
			}
		})
	}
}

func mustJSON(t *testing.T, raw json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("bad expect %s: %v", raw, err)
	}
}

func soon() time.Time { return time.Now().Add(5 * time.Second) }

func TestGitFailsWithAGitErrorOnANonZeroExit(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "not-a-repo")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Git(outside, soon(), "rev-parse", "--git-dir")
	var ge *GitError
	if !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *GitError", err)
	}
	if ge.Root != outside || ge.Code == 0 || ge.Code == -1 || ge.Timeout || ge.Msg == "" || ge.Error() != ge.Msg {
		t.Fatalf("GitError = %+v", ge)
	}
}

func TestGitFailsWithATimeoutError(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(bin+"/git", []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	// An elapsed deadline still gets the floor of 100 ms.
	_, err := Git(t.TempDir(), time.Now().Add(-time.Hour), "status")
	var ge *GitError
	if !errors.As(err, &ge) || !ge.Timeout || ge.Code != -1 || !strings.Contains(ge.Error(), "git read past the deadline") {
		t.Fatalf("err = %#v", err)
	}
}

func TestHeadOfUnbornAndBroken(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Git(t, root, "init", "-q", "-b", "main", "empty")
	if got, err := HeadOf(filepath.Join(root, "empty"), soon()); err != nil || got != "unborn" {
		t.Fatalf("HeadOf = %q, %v; want unborn", got, err)
	}
	fixturesetup.Build(t, root, []map[string]any{{"repo": "broken", "branch": "main"}, {"overwrite": "broken/.git/HEAD", "text": "garbage\n"}})
	var ge *GitError
	if _, err := HeadOf(filepath.Join(root, "broken"), soon()); !errors.As(err, &ge) {
		t.Fatalf("err = %v, want *GitError", err)
	}
}

func TestHeadOfBornRepoReturnsTheSha(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "r", "branch": "main"}})
	got, err := HeadOf(filepath.Join(root, "r"), soon())
	if err != nil || len(got) != 40 || strings.Trim(got, "0123456789abcdef") != "" {
		t.Fatalf("HeadOf = %q, %v", got, err)
	}
}

func TestDiffCommitOnTheRootCommit(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "r", "branch": "main"}})
	repo := filepath.Join(root, "r")
	sha, err := HeadOf(repo, soon())
	if err != nil {
		t.Fatal(err)
	}
	changed, deleted, err := DiffCommit(repo, sha, false, soon())
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "changed", sortedKeys(changed), []string{"README.md"})
	eq(t, "deleted", sortedKeys(deleted), []string{})
}

func TestDiffCommitOnAConflictMergeNamesOnlyTheResolution(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "r", "branch": "main"}, {"file": "r/c.txt", "text": "base\n"}})
	repo := filepath.Join(root, "r")
	g := func(args ...string) { fixturesetup.Git(t, repo, args...) }
	g("add", "c.txt")
	g("commit", "-qm", "base")
	g("switch", "-qc", "side")
	fixturesetup.Build(t, root, []map[string]any{{"overwrite": "r/c.txt", "text": "side\n"}, {"file": "r/side.txt", "text": "s\n"}})
	g("add", ".")
	g("commit", "-qm", "side")
	g("switch", "-q", "main")
	fixturesetup.Build(t, root, []map[string]any{{"overwrite": "r/c.txt", "text": "main\n"}})
	g("commit", "-qam", "main")
	mergeConflict(t, repo)
	fixturesetup.Build(t, root, []map[string]any{{"overwrite": "r/c.txt", "text": "resolved\n"}})
	g("add", "c.txt")
	g("commit", "-qm", "merge")
	sha, err := HeadOf(repo, soon())
	if err != nil {
		t.Fatal(err)
	}
	changed, deleted, err := DiffCommit(repo, sha, true, soon())
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "changed", sortedKeys(changed), []string{"c.txt"})
	eq(t, "deleted", sortedKeys(deleted), []string{})
}

// mergeConflict starts `git merge side` in repo; it must stop on a conflict.
func mergeConflict(t *testing.T, repo string) {
	t.Helper()
	c := exec.Command("git", "-C", repo, "merge", "--no-edit", "side")
	if err := c.Run(); err == nil {
		t.Fatal("merge did not conflict")
	}
}

func TestParseReflogLineWithoutSeparatorsIsAnError(t *testing.T) {
	if _, err := ParseReflog("aaa\x1fHEAD@{1}\x1fa\nfragment\n"); err == nil {
		t.Fatal("want an error, got none")
	}
}

func TestMarkerKeyWithoutToolUseIDAndANumericCommand(t *testing.T) {
	_, err := MarkerKey(map[string]json.RawMessage{"tool_input": json.RawMessage(`{"command": 5}`)})
	if err == nil || err.Error() != "command is not a string" {
		t.Fatalf("err = %v", err)
	}
}

func writeManifest(t *testing.T, root, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".lighthouse"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lighthouse", "manifest.toml"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestManagedFilesIsLenient(t *testing.T) {
	root := t.TempDir()
	got, err := ManagedFiles(root)
	if err != nil || len(got) != 0 {
		t.Fatalf("no manifest: %v, %v", got, err)
	}
	writeManifest(t, root, `
future = "key"
[[unit]]
kind = "file"
path = "a.sh"
sha256 = "0"
[[unit]]
kind = "block"
path = 5
[[unit]]
kind = "file"
path = "a.sh"
sha256 = "1"
`)
	got, err = ManagedFiles(root)
	if err != nil || !reflect.DeepEqual(got, map[string]string{"a.sh": "1"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestManagedFilesErrors(t *testing.T) {
	for name, text := range map[string]string{
		"invalid toml":   "[[unit",
		"missing sha256": "[[unit]]\nkind = \"file\"\npath = \"a\"\n",
		"mistyped path":  "[[unit]]\nkind = \"file\"\npath = 3\nsha256 = \"x\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeManifest(t, root, text)
			_, err := ManagedFiles(root)
			var me *ManifestError
			if !errors.As(err, &me) || !strings.HasPrefix(me.Error(), "cannot read "+root+"/.lighthouse/manifest.toml: ") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAsInstalled(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881"
	managed := map[string]string{"f": sum, "gone": sum, "bad": "0"}
	if !asInstalled(root, "f", managed) {
		t.Error("unchanged file should count as installed")
	}
	for _, rel := range []string{"gone", "bad", "unlisted"} {
		if asInstalled(root, rel, managed) {
			t.Errorf("%s should not count as installed", rel)
		}
	}
}

func TestGatePathAndLabel(t *testing.T) {
	if got := GatePath("/r", "md-link-check"); got != "/r/.claude/hooks/lighthouse/run.sh" {
		t.Errorf("GatePath = %q", got)
	}
	if got := GatePath("/r", "gofmt-check.sh"); got != "/r/.claude/hooks/lighthouse/gofmt-check.sh" {
		t.Errorf("GatePath = %q", got)
	}
	if GateLabel("md-link-check") != "md-link-check.sh" || GateLabel("gofmt-check.sh") != "gofmt-check.sh" {
		t.Error("GateLabel")
	}
	if !reflect.DeepEqual(Gates[".go"], []string{"gofmt-check.sh", "code-language-check.sh"}) {
		t.Error("Gates")
	}
}
