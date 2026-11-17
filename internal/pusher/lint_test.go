package pusher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeZizmor reports one artipacked finding per line with FINDING, on that line; it refuses to run without --offline and --format json.
const fakeZizmor = `#!/bin/sh
case " $* " in *" --offline "*) ;; *) echo "not offline" >&2; exit 1;; esac
case " $* " in *" --format json "*) ;; *) echo "no json" >&2; exit 1;; esac
printf '%s\n' "$*" >> "$HOME/zizmor.log"
env >> "$HOME/zizmor.log"
sep=""; found=0
printf '['
for f in "$@"; do
  case "$f" in -*|json) continue;; esac
  for line in $(grep -n FINDING "$f" | cut -d: -f1); do
    found=1
    printf '%s{"ident":"artipacked","desc":"credential persistence","determinations":{"severity":"Medium"},"ignored":false,"locations":[{"symbolic":{"key":{"Local":{"prefix":".","given_path":"%s"}},"annotation":"does not set persist-credentials: false","kind":"Primary"},"concrete":{"location":{"start_point":{"row":%s,"column":1}},"feature":"uses: actions/checkout"}}]}' "$sep" "$f" "$line"
    sep=","
  done
done
printf ']\n'
[ $found = 1 ] && exit 13
exit 0
`

// fakeActionlint reports one expression finding per input file that contains ACTIONLINT.
const fakeActionlint = `#!/bin/sh
case "$*" in *"-format {{json .}}"*) ;; *) echo "no json format" >&2; exit 2;; esac
sep=""; found=0
printf '['
for f in "$@"; do
  case "$f" in -*|"{{json .}}") continue;; esac
  line=$(grep -n ACTIONLINT "$f" | head -n1 | cut -d: -f1)
  [ -n "$line" ] || continue
  found=1
  printf '%s{"message":"undefined property","filepath":"%s","line":%s,"column":1,"kind":"expression"}' "$sep" "$f" "$line"
  sep=","
done
printf ']\n'
[ $found = 1 ] && exit 1
exit 0
`

// fakeTools puts the given fakes in a fresh directory in front of the system directories on PATH.
// The fake zizmor logs its arguments to $HOME/zizmor.log: the tools see only PATH, HOME and LANG.
func fakeTools(t *testing.T, tools map[string]string) {
	t.Helper()
	bin := t.TempDir()
	for name, text := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
}

// lintFixture puts base on main (merged into the branch) and head on the branch, for each path.
func lintFixture(t *testing.T, base, head map[string]string) (*fixture, Checked) {
	t.Helper()
	f := newFixture(t)
	if len(base) > 0 {
		for name, text := range base {
			write(t, filepath.Join(f.main, name), text)
		}
		tgit(t, f.main, "add", ".")
		tgit(t, f.main, "commit", "-q", "-m", "base")
		tgit(t, f.main, "push", "-q", "origin", "main")
		tgit(t, f.wt, "merge", "-q", "--no-edit", "main")
	}
	for name, text := range head {
		write(t, filepath.Join(f.wt, name), text)
	}
	tgit(t, f.wt, "add", ".")
	tgit(t, f.wt, "commit", "-q", "-m", "head")
	f.branchRecord(done(f.head()))
	c, red := floor(t, f.req)
	wantGreen(t, red)
	return f, c
}

func lintOf(t *testing.T, base, head map[string]string) *Red {
	t.Helper()
	f, c := lintFixture(t, base, head)
	return Lint(context.Background(), f.req, c, c.Files)
}

func TestLintExistingFindingNotRed(t *testing.T) {
	fakeTools(t, map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint})
	const path = ".github/workflows/loop.yml"
	base := map[string]string{path: "on: schedule\n# FINDING\n# ACTIONLINT\n"}
	// The same findings, three lines further down: the key has no line number.
	head := map[string]string{path: "on: schedule\nname: x\n\n\n# FINDING\n# ACTIONLINT\n"}
	if red := lintOf(t, base, head); red != nil {
		t.Fatalf("Lint red at %q: %s", red.Step, red.Why)
	}
}

func TestLintNewFindingRed(t *testing.T) {
	cases := map[string]struct {
		base, head map[string]string
		want       string
	}{
		"zizmor in new file": {
			head: map[string]string{".github/workflows/new.yml": "on: push\n# FINDING\n"},
			want: "artipacked",
		},
		"actionlint in changed file": {
			base: map[string]string{".github/workflows/ci.yml": "on: push\n"},
			head: map[string]string{".github/workflows/ci.yml": "on: push\n# ACTIONLINT\n"},
			want: "undefined property",
		},
		"finding moves to another file": {
			base: map[string]string{".github/workflows/a.yml": "# FINDING\n", ".github/workflows/b.yml": "on: push\n"},
			head: map[string]string{".github/workflows/a.yml": "on: push\n", ".github/workflows/b.yml": "# FINDING\n"},
			want: ".github/workflows/b.yml",
		},
		"odd file name": {
			head: map[string]string{".github/workflows/-rf .yml": "# FINDING\n"},
			want: ".github/workflows/-rf .yml",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fakeTools(t, map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint})
			red := lintOf(t, tc.base, tc.head)
			if red == nil {
				t.Fatal("Lint green, want red for a new finding")
			}
			if red.Step != StepLint || !strings.Contains(red.Why, tc.want) {
				t.Fatalf("red %+v, want step %q naming %q", red, StepLint, tc.want)
			}
			if strings.Contains(red.Why, "COULD NOT CHECK") {
				t.Errorf("red %q is a check failure, want a finding", red.Why)
			}
		})
	}
}

func TestLintSecondInstanceOfFindingIsRed(t *testing.T) {
	fakeTools(t, map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint})
	base := map[string]string{".github/workflows/a.yml": "# FINDING\n"}
	head := map[string]string{".github/workflows/a.yml": "# FINDING\non: push\n# FINDING\n"}
	if red := lintOf(t, base, head); red == nil || !strings.Contains(red.Why, "(1 new)") {
		t.Fatalf("red %+v, want one new instance of the finding in a.yml", red)
	}
}

func TestLintPassesOnlyGeneratedNamesAndNoSecrets(t *testing.T) {
	fakeTools(t, map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint})
	f, c := lintFixture(t, nil, map[string]string{".github/workflows/-evil.yml": "on: push\n"})
	t.Setenv("GH_TOKEN", "secret-token")
	t.Setenv("GIT_DIR", "/elsewhere") // the pusher's git commands drop it as well
	if red := Lint(context.Background(), f.req, c, c.Files); red != nil {
		t.Fatalf("Lint red: %s", red.Why)
	}
	text, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), "zizmor.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), "evil") {
		t.Errorf("zizmor got the branch's file name: %s", text)
	}
	for _, leak := range []string{"secret-token", "GIT_DIR"} {
		if strings.Contains(string(text), leak) {
			t.Errorf("zizmor saw %s in its environment: %s", leak, text)
		}
	}
}

func TestLintMissingBinaryIsRed(t *testing.T) {
	for _, missing := range []string{"zizmor", "actionlint"} {
		t.Run(missing, func(t *testing.T) {
			tools := map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint}
			delete(tools, missing)
			fakeTools(t, tools)
			red := lintOf(t, nil, map[string]string{".github/workflows/x.yml": "on: push\n"})
			if red == nil || red.Step != StepLint || !strings.HasPrefix(red.Why, "COULD NOT CHECK") || !strings.Contains(red.Why, missing) {
				t.Fatalf("red %+v, want COULD NOT CHECK naming %s", red, missing)
			}
		})
	}
}

func TestLintToolFailureIsRed(t *testing.T) {
	// Each prints valid, empty output: only the exit code or the format says it failed.
	broken := "#!/bin/sh\necho '[]'\necho boom >&2\nexit 1\n"
	for name, tools := range map[string]map[string]string{
		"zizmor error":       {"zizmor": broken, "actionlint": fakeActionlint},
		"actionlint fatal":   {"zizmor": fakeZizmor, "actionlint": "#!/bin/sh\necho '[]'\nexit 3\n"},
		"zizmor no json":     {"zizmor": "#!/bin/sh\necho nope\nexit 13\n", "actionlint": fakeActionlint},
		"actionlint 1 no []": {"zizmor": fakeZizmor, "actionlint": "#!/bin/sh\nexit 1\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fakeTools(t, tools)
			red := lintOf(t, nil, map[string]string{".github/workflows/x.yml": "on: push\n"})
			if red == nil || red.Step != StepLint || !strings.HasPrefix(red.Why, "COULD NOT CHECK") {
				t.Fatalf("red %+v, want COULD NOT CHECK", red)
			}
		})
	}
}

func TestLintWithoutWorkflowsRunsNothing(t *testing.T) {
	fakeTools(t, map[string]string{"zizmor": fakeZizmor, "actionlint": fakeActionlint})
	if red := lintOf(t, nil, map[string]string{"README": "x\n"}); red != nil {
		t.Fatalf("Lint red: %s", red.Why)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), "zizmor.log")); err == nil {
		t.Error("zizmor ran without a workflow to lint")
	}
}
