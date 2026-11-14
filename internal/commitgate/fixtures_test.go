package commitgate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
}

// sameStrings treats nil and empty as equal.
func sameStrings(a, b []string) bool {
	return len(a) == 0 && len(b) == 0 || reflect.DeepEqual(a, b)
}
