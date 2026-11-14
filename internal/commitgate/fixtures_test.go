package commitgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

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
