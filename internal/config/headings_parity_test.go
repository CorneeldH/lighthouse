package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const headingsFixtureDir = "../../tests/pack/fixtures/headings"

// TestHeadingsParity runs the fixtures that tests/pack/test_config_fixtures.py runs against
// headings.py. It fails when the Go port prints, exits or words an error differently.
func TestHeadingsParity(t *testing.T) {
	entries, err := os.ReadDir(headingsFixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cases++
		dir, err := filepath.Abs(filepath.Join(headingsFixtureDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "expect.json"))
			if err != nil {
				t.Fatal(err)
			}
			var expects []expectation
			if err := json.Unmarshal(raw, &expects); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
			t.Setenv("LIGHTHOUSE_REPO_ROOT", "")
			t.Setenv("LIGHTHOUSE_TOML", "lighthouse.toml")
			t.Setenv("LIGHTHOUSE_HEADINGS", "headings.toml")
			for _, want := range expects {
				var out, errOut bytes.Buffer
				rc := RunHeadings(want.Args, &out, &errOut)
				name := strings.Join(want.Args, " ")
				if rc != want.RC {
					t.Errorf("%s: rc = %d, want %d (stderr %q)", name, rc, want.RC, errOut.String())
				}
				if out.String() != want.Stdout {
					t.Errorf("%s: stdout = %q, want %q", name, out.String(), want.Stdout)
				}
				switch {
				case want.StderrPrefix != nil:
					if !strings.HasPrefix(errOut.String(), *want.StderrPrefix) {
						t.Errorf("%s: stderr = %q, want prefix %q", name, errOut.String(), *want.StderrPrefix)
					}
				case want.Stderr != nil:
					if errOut.String() != *want.Stderr {
						t.Errorf("%s: stderr = %q, want %q", name, errOut.String(), *want.Stderr)
					}
				default:
					t.Fatalf("%s: expect.json has neither stderr nor stderr_prefix", name)
				}
			}
		})
	}
	if cases < 10 {
		t.Fatalf("only %d fixture cases under %s", cases, headingsFixtureDir)
	}
}

func TestVariants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "headings.toml")
	body := "[a]\nnl = \"Een\"\nen = \"One\"\nfr = \"Un\"\n\n[same]\nnl = \"X\"\nen = \"X\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_HEADINGS", path)
	tests := []struct {
		key  string
		want []string
	}{
		{"a", []string{"Een", "One", "Un"}},
		{"same", []string{"X"}},
	}
	for _, tt := range tests {
		got, err := Variants(tt.key)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Join(got, "|") != strings.Join(tt.want, "|") {
			t.Errorf("Variants(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
	if _, err := Variants("nope"); err == nil {
		t.Error("Variants(unknown) returned no error")
	}
}
