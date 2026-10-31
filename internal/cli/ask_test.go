package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/protocol"
)

const askSession = "3f2b8c1e-5d4a-4e7b-9c10-0a1b2c3d4e5f"

// askFixture is a forgetest server with issue 42, a repo dir with lighthouse.toml (language nl)
// and an env that has a token and the session. XDG_STATE_HOME points at an empty temp dir.
func askFixture(t *testing.T) (*forgetest.Server, askEnv, string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := forgetest.New(t)
	srv.Login = "bot"
	srv.Issues = map[int]map[string]any{
		42: {"number": 42, "title": "De titel", "html_url": "https://github.com/o/r/issues/42"},
	}
	dir := repoDir(t, "[forge]\nrepo = \"o/r\"\n\n[project]\nlanguage = \"nl\"\n")
	env := askEnv{
		Getenv: func(k string) string {
			return map[string]string{"GH_TOKEN": "tok", "LIGHTHOUSE_CONTROLLER_RUN": askSession}[k]
		},
		Cwd:      dir,
		ForgeURL: srv.URL(),
		GhToken:  func() string { return "" },
	}
	return srv, env, dir
}

// runAsk runs `lighthouse ask args` and returns the exit code, stdout and stderr.
func runAsk(env askEnv, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	root := &cobra.Command{Use: "lighthouse", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newAskCommand(env))
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(append([]string{"ask"}, args...))
	return execute(root, &errOut), out.String(), errOut.String()
}

var askBase = []string{"--issue", "42", "--phase", "execution", "--where", "Task 2 klaar", "--what", "Twee keuzes"}

func askArgs(extra ...string) []string { return append(append([]string{}, askBase...), extra...) }

var oneQuestion = []string{"--question", "Welke aanpak?", "--option", "A", "--option", "B", "--recommended", "b"}

func TestAskPostsBundle(t *testing.T) {
	srv, env, dir := askFixture(t)
	code, out, errOut := runAsk(env, askArgs(
		"--task", "Task 2",
		"--question", "Welke aanpak?", "--option", "aanpak A", "--option", "aanpak B", "--recommended", "b",
		"--question", "Wanneer weg?", "--option", "nu", "--option", "later", "--option", "nooit", "--recommended", "a",
		"--command", "sudo dnf install ripgrep")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	posted := bodies(srv, 42)
	if len(posted) != 1 {
		t.Fatalf("comments on #42 = %d, want 1", len(posted))
	}
	if want := srv.IssueComments(42)[0]["html_url"].(string) + "\n"; out != want {
		t.Errorf("stdout = %q, want the comment URL %q", out, want)
	}
	got, ok := protocol.ParseQuestion(posted[0])
	if !ok {
		t.Fatalf("not a question:\n%s", posted[0])
	}
	want := protocol.CleanFields(protocol.QuestionFields{
		Phase: "execution", Task: "Task 2", Where: "Task 2 klaar", What: "Twee keuzes",
		Commands: []string{"sudo dnf install ripgrep"},
		Questions: []protocol.Question{
			{Question: "Welke aanpak?", Options: []protocol.Option{{Text: "aanpak A"}, {Text: "aanpak B", Recommended: true}}},
			{Question: "Wanneer weg?", Options: []protocol.Option{{Text: "nu", Recommended: true}, {Text: "later"}, {Text: "nooit"}}},
		},
	})
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parsed = %+v\nwant   = %+v", got, want)
	}
	h, _ := protocol.ParseHeader(posted[0])
	if h.Session != askSession {
		t.Errorf("footer session = %q, want %q", h.Session, askSession)
	}
	if !strings.Contains(posted[0], "De titel") || !strings.Contains(posted[0], dir) {
		t.Errorf("body lacks the title or the worktree %s:\n%s", dir, posted[0])
	}
	if !strings.Contains(posted[0], protocol.Heading("question_where", "nl")) {
		t.Errorf("body is not in the project language nl:\n%s", posted[0])
	}
}

func TestAskCleansOption(t *testing.T) {
	srv, env, _ := askFixture(t)
	code, _, errOut := runAsk(env, askArgs("--question", "Q?", "--option", "kies <sub>dit</sub>", "--option", "B", "--recommended", "a")...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	posted := bodies(srv, 42)
	if len(posted) != 1 {
		t.Fatalf("comments = %d, want 1", len(posted))
	}
	got, _ := protocol.ParseQuestion(posted[0])
	// An open <sub> would read as the start of the footer; only the opening tag is escaped.
	const want = "kies &lt;sub&gt;dit</sub>"
	if text := got.Questions[0].Options[0].Text; text != want {
		t.Errorf("posted option = %q, want the cleaned %q", text, want)
	}
}

func TestAskRejectsShape(t *testing.T) {
	q := func(text string, n int, rec ...string) []string {
		out := []string{"--question", text}
		for i := range n {
			out = append(out, "--option", string(rune('A'+i)))
		}
		for _, r := range rec {
			out = append(out, "--recommended", r)
		}
		return out
	}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	noIssue := []string{"--phase", "p", "--where", "w", "--what", "x"}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"five questions", askArgs(join(q("1", 2, "a"), q("2", 2, "a"), q("3", 2, "a"), q("4", 2, "a"), q("5", 2, "a"))...), "5 questions, want 1 to 4"},
		{"no question", askArgs(), "0 questions, want 1 to 4"},
		{"one option", askArgs(q("1", 1, "a")...), "question 1: 1 options, want 2 to 6"},
		{"seven options", askArgs(q("1", 7, "a")...), "question 1: 7 options, want 2 to 6"},
		{"option before question", askArgs("--option", "A", "--question", "1", "--option", "B", "--option", "C", "--recommended", "a"), "--option before the first --question"},
		{"recommended before question", askArgs("--recommended", "a", "--question", "1", "--option", "B", "--option", "C", "--recommended", "a"), "--recommended before the first --question"},
		{"two recommended", askArgs(q("1", 3, "a", "b")...), "question 1: 2 times --recommended, want exactly one"},
		{"no recommended", askArgs(q("1", 3)...), "question 1: 0 times --recommended, want exactly one"},
		{"recommended f at three options", askArgs(q("1", 3, "f")...), "question 1: --recommended f points to no option"},
		{"recommended not a letter", askArgs(q("1", 3, "z")...), "question 1: --recommended z points to no option"},
		{"empty option", askArgs("--question", "1", "--option", " ", "--option", "B", "--recommended", "a"), "question 1: an option is empty"},
		{"empty where", join([]string{"--issue", "42", "--phase", "p", "--where", " ", "--what", "x"}, q("1", 2, "a")), "where is empty"},
		{"no issue", join(noIssue, q("1", 2, "a")), "--issue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, env, _ := askFixture(t)
			code, out, errOut := runAsk(env, tc.args...)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			if !strings.Contains(errOut, tc.want) || !strings.HasSuffix(strings.TrimSpace(errOut), "; nothing posted") {
				t.Errorf("stderr = %q, want %q and a '; nothing posted' ending", errOut, tc.want)
			}
			if out != "" || len(srv.Posts) != 0 {
				t.Errorf("stdout %q, %d POSTs; want nothing", out, len(srv.Posts))
			}
		})
	}
	t.Run("no session", func(t *testing.T) {
		for _, session := range []string{"", "not-a-uuid"} {
			srv, env, _ := askFixture(t)
			env.Getenv = func(k string) string {
				return map[string]string{"GH_TOKEN": "tok", "LIGHTHOUSE_CONTROLLER_RUN": session}[k]
			}
			code, _, errOut := runAsk(env, askArgs(oneQuestion...)...)
			if code != 1 || !strings.Contains(errOut, "LIGHTHOUSE_CONTROLLER_RUN") || !strings.Contains(errOut, "nothing posted") || len(srv.Posts) != 0 {
				t.Errorf("session %q: exit %d, stderr %q, %d POSTs", session, code, errOut, len(srv.Posts))
			}
		}
	})
}

func TestAskPostFailsExit1(t *testing.T) {
	srv, env, _ := askFixture(t)
	srv.Fail = map[string]int{"POST /repos/o/r/issues/42/comments": 500}
	code, out, errOut := runAsk(env, askArgs(oneQuestion...)...)
	if code != 1 || out != "" {
		t.Errorf("exit %d, stdout %q; want 1 and nothing", code, out)
	}
	if !strings.HasPrefix(errOut, "lighthouse ask: post failed: ") {
		t.Errorf("stderr = %q, want the 'lighthouse ask: post failed: ' prefix", errOut)
	}
	if n := len(bodies(srv, 42)); n != 0 {
		t.Errorf("%d comments stored, want none", n)
	}
}

func TestAskUnknownIssueExit1(t *testing.T) {
	srv, env, _ := askFixture(t)
	args := append([]string{"--issue", "43"}, askBase[2:]...)
	code, _, errOut := runAsk(env, append(args, oneQuestion...)...)
	if code != 1 || !strings.HasPrefix(errOut, "lighthouse ask: post failed: ") || len(srv.Posts) != 0 {
		t.Errorf("exit %d, stderr %q, %d POSTs", code, errOut, len(srv.Posts))
	}
}

func TestAskNoLighthouseToml(t *testing.T) {
	srv, env, _ := askFixture(t)
	env.Cwd = t.TempDir()
	code, _, errOut := runAsk(env, askArgs(oneQuestion...)...)
	if code != 1 || !strings.Contains(errOut, "lighthouse.toml") || !strings.HasSuffix(strings.TrimSpace(errOut), "; nothing posted") {
		t.Errorf("exit %d, stderr %q; want 1, lighthouse.toml and '; nothing posted'", code, errOut)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("%d POSTs, want none", len(srv.Posts))
	}
}

func TestAskNoToken(t *testing.T) {
	srv, env, _ := askFixture(t)
	env.Getenv = func(k string) string { return map[string]string{"LIGHTHOUSE_CONTROLLER_RUN": askSession}[k] }
	code, _, errOut := runAsk(env, askArgs(oneQuestion...)...)
	if code != 1 || !strings.Contains(errOut, "GH_TOKEN") || len(srv.Posts) != 0 {
		t.Errorf("exit %d, stderr %q, %d POSTs", code, errOut, len(srv.Posts))
	}
}

func TestAskTokenOrder(t *testing.T) {
	for _, tc := range []struct{ gh, github, ghCLI, want string }{
		{"a", "b", "c", "a"}, {"", "b", "c", "b"}, {"", "", "c", "c"},
	} {
		env := askEnv{
			Getenv:  func(k string) string { return map[string]string{"GH_TOKEN": tc.gh, "GITHUB_TOKEN": tc.github}[k] },
			GhToken: func() string { return tc.ghCLI },
		}
		if got := env.token(); got != tc.want {
			t.Errorf("token(%+v) = %q, want %q", tc, got, tc.want)
		}
	}
}

func TestAskDryRunPostsNothing(t *testing.T) {
	srv, env, _ := askFixture(t)
	code, out, errOut := runAsk(env, askArgs(append([]string{"--dry-run"}, oneQuestion...)...)...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("%d POSTs, want none", len(srv.Posts))
	}
	if _, ok := protocol.ParseQuestion(out); !ok {
		t.Errorf("stdout is not the rendered question:\n%s", out)
	}
}

func TestAskOpensNoDatabase(t *testing.T) {
	_, env, _ := askFixture(t)
	state := os.Getenv("XDG_STATE_HOME")
	if code, _, errOut := runAsk(env, askArgs(oneQuestion...)...); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if _, err := os.Stat(filepath.Join(state, "lighthouse", "lighthouse.db")); !os.IsNotExist(err) {
		t.Errorf("lighthouse.db: stat err = %v, want not exist", err)
	}
}

// A text the controller would not read back as it went in is refused before the post.
func TestAskRefusesUnreadableRoundTrip(t *testing.T) {
	srv, env, _ := askFixture(t)
	what := "eerste regel\n\n**" + protocol.Heading("question_where", "nl") + ":** nog een veld"
	code, _, errOut := runAsk(env, "--issue", "42", "--phase", "p", "--where", "w", "--what", what,
		"--question", "Q", "--option", "A", "--option", "B", "--recommended", "a")
	if code != 1 || !strings.Contains(errOut, "reads back differently") || !strings.HasSuffix(strings.TrimSpace(errOut), "; nothing posted") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if len(srv.Posts) != 0 {
		t.Errorf("%d POSTs, want none", len(srv.Posts))
	}
}

// run.md points the run at `lighthouse ask --help`: every flag must say there what it takes.
func TestAskHelpDescribesEveryFlag(t *testing.T) {
	srv, env, _ := askFixture(t)
	code, out, errOut := runAsk(env, "--help")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{
		"--issue int             the issue number <N> from the prompt (issue #<N>, controller-run); required",
		"--phase string          the phase of the run; required",
		"--task string           the task of the run; optional",
		"--where string          where the run stands; required",
		"--what string           what is going on; required",
		"--question string       opens a question; 1 to 4 per call",
		"--option string         an option of the last --question; 2 to 6 per question",
		"--recommended string    the letter (a-f) of the recommended option of the last --question; exactly once per question",
		"--command stringArray   a command the user has to run, put unchanged in the question; optional, repeatable",
		"--dry-run               print the question instead of posting it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
	if len(srv.Posts) != 0 {
		t.Errorf("%d POSTs, want none", len(srv.Posts))
	}
}

// A run may call ask from a subdirectory of its worktree: lighthouse.toml is read from the
// top of the git repository, and that top is the worktree in the question.
func TestAskFromSubdirectoryReadsRepoRoot(t *testing.T) {
	srv, env, root := askFixture(t)
	git := exec.Command("git", "-C", root, "init", "-q")
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	sub := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	env.Cwd = sub
	code, _, errOut := runAsk(env, askArgs(oneQuestion...)...)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	posted := bodies(srv, 42)
	if len(posted) != 1 {
		t.Fatalf("comments on #42 = %d, want 1", len(posted))
	}
	if !strings.Contains(posted[0], root) || strings.Contains(posted[0], sub) {
		t.Errorf("body names the worktree %s, want the root %s:\n%s", sub, root, posted[0])
	}
}

// A flag cobra cannot parse ends like every other refusal before the post.
func TestAskFlagErrorNothingPosted(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"issue not a number", append([]string{"--issue", "abc"}, askBase[2:]...),
			`lighthouse ask: invalid argument "abc" for "--issue" flag: strconv.ParseInt: parsing "abc": invalid syntax; nothing posted` + "\n"},
		{"unknown flag", askArgs("--bogus"), "lighthouse ask: unknown flag: --bogus; nothing posted\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, env, _ := askFixture(t)
			code, out, errOut := runAsk(env, append(tc.args, oneQuestion...)...)
			if code != 1 || errOut != tc.want || out != "" {
				t.Errorf("exit %d, stdout %q, stderr %q; want 1, nothing and %q", code, out, errOut, tc.want)
			}
			if len(srv.Posts) != 0 {
				t.Errorf("%d POSTs, want none", len(srv.Posts))
			}
		})
	}
}
