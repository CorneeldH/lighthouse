package main

import "testing"

func TestNextTag(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		branch   string
		tags     []string
		branches []string
		want     string
	}{
		{"dev after release", "dev", "", []string{"v0.27.0"}, nil, "v0.28.0-dev.1"},
		{"dev counts up", "dev", "", []string{"v0.27.0", "v0.28.0-dev.1", "v0.28.0-dev.2"}, nil, "v0.28.0-dev.3"},
		{"dev ten is above nine", "dev", "", []string{"v0.27.0", "v0.28.0-dev.9", "v0.28.0-dev.10"}, nil, "v0.28.0-dev.11"},
		{"dev after patch", "dev", "", []string{"v0.27.0", "v0.27.1"}, nil, "v0.28.0-dev.1"},
		{"dev with open branch", "dev", "", []string{"v0.27.0"}, []string{"release/v0.28"}, "v0.29.0-dev.1"},
		{"dev with closed branch", "dev", "", []string{"v0.27.0", "v0.28.0"}, []string{"release/v0.28"}, "v0.29.0-dev.1"},
		{"dev numbering is per version", "dev", "", []string{"v0.27.0", "v0.28.0-dev.4"}, []string{"release/v0.28"}, "v0.29.0-dev.1"},
		{"rc first", "rc", "release/v0.28", []string{"v0.27.0"}, []string{"release/v0.28"}, "v0.28.0-rc.1"},
		{"rc next", "rc", "release/v0.28", []string{"v0.27.0", "v0.28.0-rc.1"}, nil, "v0.28.0-rc.2"},
		{"test shares base with dev", "test", "", []string{"v0.27.0"}, nil, "v0.28.0-test.1"},
		{"test does not touch rc", "test", "", []string{"v0.27.0", "v0.28.0-rc.3", "v0.28.0-test.1"}, nil, "v0.28.0-test.2"},
		{"rc ignores test", "rc", "release/v0.28", []string{"v0.28.0-test.5"}, nil, "v0.28.0-rc.1"},
		{"patch", "patch", "release/v0.28", []string{"v0.28.0", "v0.28.1"}, nil, "v0.28.2"},
		{"patch first", "patch", "release/v0.28", []string{"v0.28.0"}, nil, "v0.28.1"},
		{"minor", "minor", "", []string{"v0.27.0"}, nil, "v0.28.0"},
		{"minor with branch above", "minor", "", []string{"v0.27.0"}, []string{"release/v0.28"}, "v0.29.0"},
		{"major", "major", "", []string{"v0.27.0", "v1.2.0"}, nil, "v2.0.0"},
	}
	for _, tc := range tests {
		got, err := nextTag(tc.kind, tc.branch, tc.tags, tc.branches)
		if err != nil || got != tc.want {
			t.Errorf("%s: nextTag(%s, %q) = %q, %v; want %q", tc.name, tc.kind, tc.branch, got, err, tc.want)
		}
	}
}

func TestNextTagRefuses(t *testing.T) {
	tests := []struct {
		name, kind, branch string
		tags, branches     []string
	}{
		{"rc for released branch", "rc", "release/v0.28", []string{"v0.28.0"}, nil},
		{"rc without branch", "rc", "", nil, nil},
		{"rc with bad branch", "rc", "main", nil, nil},
		{"patch before the first release", "patch", "release/v0.28", []string{"v0.28.0-rc.1"}, nil},
		{"unknown kind", "huge", "", nil, nil},
		{"dev without any release", "dev", "", nil, nil},
	}
	for _, tc := range tests {
		if got, err := nextTag(tc.kind, tc.branch, tc.tags, tc.branches); err == nil {
			t.Errorf("%s: nextTag = %q, want an error", tc.name, got)
		}
	}
}

func TestRunNextPrintsTag(t *testing.T) {
	root := taggedRepo(t, nil)
	git(t, root, "branch", "release/v0.2")
	code, stdout, stderr := runCLI("next", "--kind", "dev", "--root", root)
	if code != 0 || stdout != "v0.3.0-dev.1\n" || stderr != "" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, _ = runCLI("next", "--kind", "rc", "--branch", "release/v0.2", "--root", root)
	if code != 0 || stdout != "v0.2.0-rc.1\n" {
		t.Fatalf("rc: code=%d stdout=%q", code, stdout)
	}
	if code, _, stderr := runCLI("next", "--kind", "huge", "--root", root); code != 2 {
		t.Fatalf("kind huge: code=%d stderr=%q, want usage error 2", code, stderr)
	}
}
