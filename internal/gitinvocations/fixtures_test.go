package gitinvocations

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

const casesFile = "../../tests/pack/fixtures/git_invocations/cases.json"

type fixtureCase struct {
	Name    string            `json:"name"`
	Setup   []map[string]any  `json:"setup"`
	Env     map[string]string `json:"env"`
	Kind    string            `json:"kind"`
	Command string            `json:"command"`
	CWD     string            `json:"cwd"`
	Path    string            `json:"path"`
	Default string            `json:"default_branch"`
	Report  bool              `json:"report_new_dirs"`
	Expect  json.RawMessage   `json:"expect"`
}

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile(casesFile)
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
			root := t.TempDir() // deliberately without EvalSymlinks
			t.Setenv("LH_UNSET_VAR", "")
			if err := os.Unsetenv("LH_UNSET_VAR"); err != nil {
				t.Fatal(err)
			}
			fixturesetup.Build(t, root, c.Setup)
			for key, value := range c.Env {
				t.Setenv(key, fixturesetup.Fill(t, value, root))
			}
			if c.Kind == "repo_of" {
				var want *string
				if err := json.Unmarshal(c.Expect, &want); err != nil {
					t.Fatal(err)
				}
				got, ok := RepoOf(filepath.Join(root, c.Path))
				if want == nil {
					if ok {
						t.Fatalf("RepoOf = %q, want none", got)
					}
					return
				}
				if !ok || got != fixturesetup.Fill(t, *want, root) {
					t.Fatalf("RepoOf = %q, %v; want %q", got, ok, fixturesetup.Fill(t, *want, root))
				}
				return
			}
			command := fixturesetup.Fill(t, c.Command, root)
			cwd := root
			if c.CWD != "" {
				cwd = filepath.Join(root, c.CWD)
			}
			if c.Kind == "find" {
				got, err := Find(command, cwd, Options{DefaultBranch: c.Default, ReportNewDirs: c.Report})
				var word string
				if json.Unmarshal(c.Expect, &word) == nil && word == "unparseable" {
					if !errors.Is(err, ErrUnparseable) {
						t.Fatalf("err = %v, want ErrUnparseable", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want [][4]string
				if err := json.Unmarshal(c.Expect, &want); err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) {
					t.Fatalf("got %d invocations, want %d: %+v", len(got), len(want), got)
				}
				for i, inv := range got {
					w := Invocation{want[i][0], fixturesetup.Fill(t, want[i][1], root), want[i][2], fixturesetup.Fill(t, want[i][3], root)}
					if inv != w {
						t.Errorf("invocation %d = %+v, want %+v", i, inv, w)
					}
				}
				return
			}
			if c.Kind == "git_target" {
				var want struct {
					Target     *string `json:"target"`
					Known      *bool   `json:"known"`
					ActionWord string  `json:"action_word"`
				}
				if err := json.Unmarshal(c.Expect, &want); err != nil {
					t.Fatal(err)
				}
				steps, err := Walk(command, cwd)
				if err != nil || len(steps) != 1 {
					t.Fatalf("Walk = %v, %v; want one step", steps, err)
				}
				target, known, _, _, actionIdx := GitTarget(steps[0])
				wantKnown := want.Known == nil || *want.Known
				wantTarget := ""
				if want.Target != nil {
					wantTarget = fixturesetup.Fill(t, *want.Target, root)
				}
				if known != wantKnown || target != wantTarget || steps[0].Words[actionIdx] != want.ActionWord {
					t.Fatalf("GitTarget = %q, %v, word %q; want %q, %v, %q", target, known, steps[0].Words[actionIdx], wantTarget, wantKnown, want.ActionWord)
				}
				return
			}
			steps, err := Walk(command, cwd)
			var word string
			if json.Unmarshal(c.Expect, &word) == nil && word == "unparseable" {
				if !errors.Is(err, ErrUnparseable) {
					t.Fatalf("err = %v, want ErrUnparseable", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var want [][2]string
			if err := json.Unmarshal(c.Expect, &want); err != nil {
				t.Fatal(err)
			}
			if len(steps) != len(want) {
				t.Fatalf("got %d steps, want %d: %+v", len(steps), len(want), steps)
			}
			for i, step := range steps {
				if got := step.Words[step.Idx]; got != want[i][0] {
					t.Errorf("step %d command = %q, want %q", i, got, want[i][0])
				}
				if wantHere := fixturesetup.Fill(t, want[i][1], root); step.Here != wantHere {
					t.Errorf("step %d here = %q, want %q", i, step.Here, wantHere)
				}
			}
		})
	}
}
