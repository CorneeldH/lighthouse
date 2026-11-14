package commitgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

// Shared fixtures: tests/pack/fixtures/<name>/cases.json, read here and by the Python tests
// (tests/pack/_testkit.py). Each file is a list of cases with a "name", a "kind" and an "expect".
//
//   - no_verify: {command, expect: bool}. Does the shell command carry --no-verify?
//   - host_memory, kind "meminfo": {text, expect}. /proc/meminfo text to a map of fields in kB,
//     or expect "raises" for text that does not parse.
//   - host_memory, kind "shortfalls": {info, min_available_gib, min_swap_free_gib, expect:
//     {found, notes}}. The fields and thresholds to what is short and the notes about it.
//   - commit_base, kind "record": {setup, roots, expect: {heads: {root: sha}}}. The marker that
//     Record writes for the roots.
//   - commit_base, kind "moved": {setup, roots, commits, expect, expect_second}. Record the roots,
//     apply commits, then MovedRoots must give the rows [root, base, head]; expect_second, when
//     present, is a second call on the same marker.
//   - commit_base, kind "changed_files": {setup, repo, base, head, expect}. The files that
//     ChangedFiles reports between two revisions.
//
// A "setup" (and the "commits" of "moved") is a list of steps that fixturesetup.Build runs under
// a temporary root, in the format of tests/pack/_testkit.py build_fixture_setup: repo, dir, file,
// symlink, overwrite, worktree, origin, and unborn (git init without a commit), commit (add -A
// and commit) and mv (git mv). Strings are filled by fixturesetup.Fill: {root} is the temporary
// directory, {rev:<path>:<rev>} the output of git rev-parse <rev> in {root}/<path>, and
// {empty_tree} the empty tree. The keys of an expected "heads" are filled too.
//
// commit_base/marker_python.json is not a case list: it is a marker as the Python gate wrote it
// (json.dumps escapes the é of a root), which MovedRoots must read (TestMovedReadsAPythonWrittenMarker).

const fixtureDir = "../../tests/pack/fixtures/"

func readCases(t *testing.T, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixtureDir, name, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatal(err)
	}
}

type noVerifyCase struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Expect  bool   `json:"expect"`
}

type memoryCase struct {
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	Text     string           `json:"text"`
	Info     map[string]int64 `json:"info"`
	MinAvail *float64         `json:"min_available_gib"`
	MinSwap  *float64         `json:"min_swap_free_gib"`
	Expect   json.RawMessage  `json:"expect"`
}

type baseCase struct {
	Name         string           `json:"name"`
	Kind         string           `json:"kind"`
	Setup        []map[string]any `json:"setup"`
	Roots        []string         `json:"roots"`
	Commits      []map[string]any `json:"commits"`
	Repo         string           `json:"repo"`
	Base         string           `json:"base"`
	Head         string           `json:"head"`
	Expect       json.RawMessage  `json:"expect"`
	ExpectSecond json.RawMessage  `json:"expect_second"`
}

var fixturePayload = []byte(`{"session_id": "s1", "tool_use_id": "t1"}`)

// fillAll fills every string of a list.
func fillAll(t *testing.T, values []string, root string) []string {
	t.Helper()
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = fixturesetup.Fill(t, v, root)
	}
	return out
}

func runBaseCase(t *testing.T, c baseCase) {
	root := t.TempDir() // deliberately without realpath
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "runtime"))
	fixturesetup.Build(t, root, c.Setup)
	fill := func(v string) string { return fixturesetup.Fill(t, v, root) }
	switch c.Kind {
	case "record":
		if err := Record(fixturePayload, fillAll(t, c.Roots, root)); err != nil {
			t.Fatal(err)
		}
		dir, err := StateDir("s1", false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "t1.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got, expect struct{ Heads map[string]string }
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(c.Expect, &expect); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{}
		for k, v := range expect.Heads { // the keys are filled too, as fill_fixture does
			want[fill(k)] = fill(v)
		}
		if !reflect.DeepEqual(got.Heads, want) {
			t.Errorf("marker heads = %v, want %v", got.Heads, want)
		}
	case "moved":
		if err := Record(fixturePayload, fillAll(t, c.Roots, root)); err != nil {
			t.Fatal(err)
		}
		fixturesetup.Build(t, root, c.Commits)
		check := func(raw json.RawMessage, label string) {
			var rows [][]string
			if err := json.Unmarshal(raw, &rows); err != nil {
				t.Fatal(err)
			}
			got, err := MovedRoots(fixturePayload)
			if err != nil {
				t.Fatalf("%s: %v", label, err)
			}
			if len(got) != len(rows) {
				t.Fatalf("%s: got %v, want %d rows", label, got, len(rows))
			}
			for i, row := range rows {
				want := Moved{fill(row[0]), fill(row[1]), fill(row[2])}
				if got[i] != want {
					t.Errorf("%s: row %d = %v, want %v", label, i, got[i], want)
				}
			}
		}
		check(c.Expect, "first")
		if c.ExpectSecond != nil {
			check(c.ExpectSecond, "second")
		}
	case "changed_files":
		var want []string
		if err := json.Unmarshal(c.Expect, &want); err != nil {
			t.Fatal(err)
		}
		got, err := ChangedFiles(fill(c.Repo), fill(c.Base), fill(c.Head))
		if err != nil || !sameStrings(got, fillAll(t, want, root)) {
			t.Errorf("ChangedFiles = %q, %v; want %q", got, err, want)
		}
	default:
		t.Fatalf("unknown kind %q", c.Kind)
	}
}

func TestFixtures(t *testing.T) {
	t.Run("no_verify", func(t *testing.T) {
		var cases []noVerifyCase
		readCases(t, "no_verify", &cases)
		if len(cases) == 0 {
			t.Fatal("no cases")
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				if got := CommandHasNoVerify(c.Command, t.TempDir()); got != c.Expect {
					t.Errorf("CommandHasNoVerify(%q) = %v, want %v", c.Command, got, c.Expect)
				}
			})
		}
	})
	t.Run("host_memory", func(t *testing.T) {
		var cases []memoryCase
		readCases(t, "host_memory", &cases)
		if len(cases) == 0 {
			t.Fatal("no cases")
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) {
				switch c.Kind {
				case "meminfo":
					path := filepath.Join(t.TempDir(), "meminfo")
					if err := os.WriteFile(path, []byte(c.Text), 0o600); err != nil {
						t.Fatal(err)
					}
					got, err := ReadMeminfo(path)
					if string(c.Expect) == `"raises"` {
						if err == nil {
							t.Fatalf("want an error, got %v", got)
						}
						return
					}
					var want map[string]int64
					if jerr := json.Unmarshal(c.Expect, &want); jerr != nil {
						t.Fatal(jerr)
					}
					if err != nil || !reflect.DeepEqual(got, want) {
						t.Errorf("got %v, %v; want %v", got, err, want)
					}
				case "shortfalls":
					var want struct{ Found, Notes []string }
					if err := json.Unmarshal(c.Expect, &want); err != nil {
						t.Fatal(err)
					}
					found, notes := Shortfalls(c.Info, Thresholds{c.MinAvail, c.MinSwap})
					if !sameStrings(found, want.Found) || !sameStrings(notes, want.Notes) {
						t.Errorf("got %q, %q; want %q, %q", found, notes, want.Found, want.Notes)
					}
				default:
					t.Fatalf("unknown kind %q", c.Kind)
				}
			})
		}
	})
	t.Run("commit_base", func(t *testing.T) {
		var cases []baseCase
		readCases(t, "commit_base", &cases)
		if len(cases) == 0 {
			t.Fatal("no cases")
		}
		for _, c := range cases {
			t.Run(c.Name, func(t *testing.T) { runBaseCase(t, c) })
		}
	})
}

// sameStrings treats nil and empty as equal.
func sameStrings(a, b []string) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
