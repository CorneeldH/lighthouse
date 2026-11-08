package gitinvocations

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const casesFile = "../../tests/pack/fixtures/git_invocations/cases.json"

type setupStep struct {
	Repo      *string `json:"repo"`
	Branch    string  `json:"branch"`
	Dir       *string `json:"dir"`
	File      *string `json:"file"`
	Text      string  `json:"text"`
	Symlink   *string `json:"symlink"`
	To        string  `json:"to"`
	Overwrite *string `json:"overwrite"`
}

type fixtureCase struct {
	Name    string            `json:"name"`
	Setup   []setupStep       `json:"setup"`
	Env     map[string]string `json:"env"`
	Kind    string            `json:"kind"`
	Command string            `json:"command"`
	CWD     string            `json:"cwd"`
	Path    string            `json:"path"`
	Expect  json.RawMessage   `json:"expect"`
}

var homePlaceholder = regexp.MustCompile(`\{home:([^}]+)\}`)

// fill replaces {root} and {home:<name>}, the placeholders the Python runner fills too.
func fill(t *testing.T, value, root string) string {
	t.Helper()
	value = strings.ReplaceAll(value, "{root}", root)
	return homePlaceholder.ReplaceAllStringFunc(value, func(m string) string {
		u, err := user.Lookup(homePlaceholder.FindStringSubmatch(m)[1])
		if err != nil {
			t.Fatalf("lookup user: %v", err)
		}
		return u.HomeDir
	})
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func build(t *testing.T, setup []setupStep, root string) {
	t.Helper()
	write := func(rel, text string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range setup {
		switch {
		case step.Repo != nil:
			dir := filepath.Join(root, *step.Repo)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			git(t, dir, "init", "-q", "-b", step.Branch)
			write(filepath.Join(*step.Repo, "README.md"), "# repo\n")
			git(t, dir, "add", "README.md")
			git(t, dir, "commit", "-qm", "init")
		case step.Dir != nil:
			if err := os.MkdirAll(filepath.Join(root, *step.Dir), 0o755); err != nil {
				t.Fatal(err)
			}
		case step.File != nil:
			write(*step.File, step.Text)
		case step.Symlink != nil:
			if err := os.Symlink(filepath.Join(root, step.To), filepath.Join(root, *step.Symlink)); err != nil {
				t.Fatal(err)
			}
		case step.Overwrite != nil:
			write(*step.Overwrite, step.Text)
		default:
			t.Fatalf("unknown setup step %+v", step)
		}
	}
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
			if c.Kind != "walk" && c.Kind != "repo_of" {
				t.Skip("taak 3")
			}
			root := t.TempDir() // deliberately without EvalSymlinks
			t.Setenv("LH_UNSET_VAR", "")
			if err := os.Unsetenv("LH_UNSET_VAR"); err != nil {
				t.Fatal(err)
			}
			build(t, c.Setup, root)
			for key, value := range c.Env {
				t.Setenv(key, fill(t, value, root))
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
				if !ok || got != fill(t, *want, root) {
					t.Fatalf("RepoOf = %q, %v; want %q", got, ok, fill(t, *want, root))
				}
				return
			}
			command := fill(t, c.Command, root)
			cwd := root
			if c.CWD != "" {
				cwd = filepath.Join(root, c.CWD)
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
				if wantHere := fill(t, want[i][1], root); step.Here != wantHere {
					t.Errorf("step %d here = %q, want %q", i, step.Here, wantHere)
				}
			}
		})
	}
}
