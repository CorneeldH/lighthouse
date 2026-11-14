package config

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureDir = "../../tests/pack/fixtures/config"

type expectation struct {
	Args         []string `json:"args"`
	RC           int      `json:"rc"`
	Stdout       string   `json:"stdout"`
	Stderr       *string  `json:"stderr"`
	StderrPrefix *string  `json:"stderr_prefix"`
}

// TestParityWithConfigPy runs the fixtures that tests/pack/test_config_fixtures.py runs
// against config.py. It fails when the Go port prints, exits or words an error differently.
func TestParityWithConfigPy(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cases++
		dir, err := filepath.Abs(filepath.Join(fixtureDir, entry.Name()))
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
			t.Setenv("LIGHTHOUSE_HOST_TOML", "host.toml")
			for _, want := range expects {
				var out, errOut bytes.Buffer
				rc := Run(want.Args, &out, &errOut)
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
	if cases < 100 {
		t.Fatalf("only %d fixture cases under %s", cases, fixtureDir)
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{123456789.0, "123456789.0"},
		{1e16, "1e+16"},
		{1e-5, "1e-05"},
		{math.Inf(1), "inf"},
		{math.Inf(-1), "-inf"},
		{math.NaN(), "nan"},
		{0.0001, "0.0001"},
		{1.5, "1.5"},
		{math.Copysign(0, -1), "-0.0"},
		{0, "0.0"},
		{100, "100.0"},
		{1e15, "1000000000000000.0"},
		{1.5e16, "1.5e+16"},
		{1.2345678901234568e17, "1.2345678901234568e+17"},
		{5e-324, "5e-324"},
		{0.00012345, "0.00012345"},
		{-2.5e-7, "-2.5e-07"},
		{1e100, "1e+100"},
	}
	for _, tt := range tests {
		if got := FormatFloat(tt.in); got != tt.want {
			t.Errorf("FormatFloat(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
