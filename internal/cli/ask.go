package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// askEnv is what `lighthouse ask` takes from its surroundings, so a test can replace it.
type askEnv struct {
	Getenv   func(string) string
	Cwd      string        // where ask runs; its git top is the worktree and holds lighthouse.toml
	ForgeURL string        // "" is forge.DefaultBaseURL
	GhToken  func() string // the token of `gh auth token`; "" when there is none
}

func defaultAskEnv() askEnv {
	cwd, _ := os.Getwd()
	return askEnv{Getenv: os.Getenv, Cwd: cwd, GhToken: ghAuthToken}
}

// ghAuthToken is the output of `gh auth token`, "" when gh is missing or not logged in.
func ghAuthToken() string {
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// token is GH_TOKEN, then GITHUB_TOKEN, then `gh auth token`.
func (e askEnv) token() string {
	if t := githubToken(e.Getenv); t != "" {
		return t
	}
	if e.GhToken == nil {
		return ""
	}
	return e.GhToken()
}

// askSessionEnv carries the session of the run; the controller reads it back from the footer.
const askSessionEnv = "LIGHTHOUSE_CONTROLLER_RUN"

var askSessionUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// askStep is one --question, --option or --recommended, in command-line order.
type askStep struct{ flag, value string }

// askFlag is a pflag.Value that appends (name, value) to the slice it shares with the other
// two; Set only receives the value, so each flag carries its own name.
type askFlag struct {
	name  string
	steps *[]askStep
}

func (f askFlag) Set(v string) error {
	*f.steps = append(*f.steps, askStep{f.name, v})
	return nil
}
func (f askFlag) String() string { return "" }
func (f askFlag) Type() string   { return "string" }

// askInput is what the flags gave.
type askInput struct {
	issue       int
	phase, task string
	where, what string
	commands    []string
	steps       []askStep
	dryRun      bool
}

func newAskCommand(env askEnv) *cobra.Command {
	var in askInput
	cmd := &cobra.Command{
		Use:   "ask --issue N --phase T [--task T] --where T --what T (--question T (--option T){2,6} --recommended <a-f>){1,4} [--command T]... [--dry-run]",
		Short: "Post a 🤖 Question on an issue from a controller run",
		Long: "Post the question of a controller run on its issue, as one bundle of 1 to 4 questions. Every\n" +
			"--question opens a question; the --option and --recommended flags after it belong to it. The\n" +
			"session comes from LIGHTHOUSE_CONTROLLER_RUN, the repo and the language from lighthouse.toml at\n" +
			"the top of the git repository around the current directory. Prints the comment URL; with\n" +
			"--dry-run, the question instead of posting it.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := ask(cmd.Context(), env, in, cmd.OutOrStdout())
			if err == nil {
				return nil
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "lighthouse ask: %v\n", err)
			return errSilent
		},
	}
	fl := cmd.Flags()
	fl.IntVar(&in.issue, "issue", 0, "the issue number <N> from the prompt (issue #<N>, controller-run); required")
	fl.StringVar(&in.phase, "phase", "", "the phase of the run; required")
	fl.StringVar(&in.task, "task", "", "the task of the run; optional")
	fl.StringVar(&in.where, "where", "", "where the run stands; required")
	fl.StringVar(&in.what, "what", "", "what is going on; required")
	fl.StringArrayVar(&in.commands, "command", nil, "a command the user has to run, put unchanged in the question; optional, repeatable")
	fl.BoolVar(&in.dryRun, "dry-run", false, "print the question instead of posting it")
	for _, f := range []struct{ name, usage string }{
		{"question", "opens a question; 1 to 4 per call"},
		{"option", "an option of the last --question; 2 to 6 per question"},
		{"recommended", "the letter (a-f) of the recommended option of the last --question; exactly once per question"},
	} {
		fl.Var(askFlag{f.name, &in.steps}, f.name, f.usage)
	}
	return cmd
}

// ask validates in, renders the question, checks that the controller reads it back as it went
// in, and posts it. Every error before the post ends in "; nothing posted".
func ask(ctx context.Context, env askEnv, in askInput, out io.Writer) error {
	fields, err := askFields(in)
	if err != nil {
		return fmt.Errorf("%w; nothing posted", err)
	}
	session := env.Getenv(askSessionEnv)
	if !askSessionUUID.MatchString(session) {
		return fmt.Errorf("%s is missing or no UUID; nothing posted", askSessionEnv)
	}
	if in.issue < 1 {
		return errors.New("--issue is required and must be a positive number; nothing posted")
	}
	root := askRoot(env.Cwd)
	repo, err := repoconfig.Load(root)
	if err != nil {
		return fmt.Errorf("%w; nothing posted", err)
	}
	token := env.token()
	if token == "" {
		return errors.New("no GitHub token: set GH_TOKEN or GITHUB_TOKEN; nothing posted")
	}
	c := forge.New(repo.Slug, token)
	if env.ForgeURL != "" {
		c.BaseURL = env.ForgeURL
	}
	issue, err := c.Issue(ctx, in.issue)
	if err != nil {
		return fmt.Errorf("post failed: %w", err)
	}
	body := protocol.RenderQuestion(protocol.QuestionMeta{
		Issue: in.issue, Title: issue.Title, IssueURL: issue.URL, Session: session, Worktree: root,
	}, fields, repo.Language)
	if got, ok := protocol.ParseQuestion(body); !ok || !reflect.DeepEqual(got, protocol.CleanFields(fields)) {
		return errors.New("the question reads back differently than it went in (a field heading in the text?); nothing posted")
	}
	if in.dryRun {
		_, err := fmt.Fprintln(out, body)
		return err
	}
	comment, err := c.CreateComment(ctx, in.issue, body)
	if err != nil {
		return fmt.Errorf("post failed: %w", err)
	}
	_, err = fmt.Fprintln(out, comment.URL)
	return err
}

// askRoot is the top of the git repository around cwd, so ask works from a subdirectory of
// the worktree; cwd itself when git gives no top.
func askRoot(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if top := strings.TrimSpace(string(out)); err == nil && top != "" {
		return top
	}
	return cwd
}

// askFields builds the question fields from the flags, or says what is wrong with their shape.
func askFields(in askInput) (protocol.QuestionFields, error) {
	questions, err := askQuestions(in.steps)
	if err != nil {
		return protocol.QuestionFields{}, err
	}
	f := protocol.QuestionFields{
		Phase: strings.TrimSpace(in.phase), Task: strings.TrimSpace(in.task),
		Where: strings.TrimSpace(in.where), What: strings.TrimSpace(in.what),
		Questions: questions,
	}
	for _, c := range in.commands {
		if strings.TrimSpace(c) != "" {
			f.Commands = append(f.Commands, c)
		}
	}
	for _, field := range []struct{ name, value string }{{"phase", f.Phase}, {"where", f.Where}, {"what", f.What}} {
		if field.value == "" {
			return protocol.QuestionFields{}, fmt.Errorf("%s is empty", field.name)
		}
	}
	return f, nil
}

// askQuestions groups the steps per --question.
func askQuestions(steps []askStep) ([]protocol.Question, error) {
	type group struct {
		text    string
		options []string
		letters []string
	}
	var groups []*group
	for _, s := range steps {
		switch {
		case s.flag == "question":
			groups = append(groups, &group{text: strings.TrimSpace(s.value)})
		case len(groups) == 0:
			return nil, fmt.Errorf("--%s before the first --question", s.flag)
		case s.flag == "option":
			g := groups[len(groups)-1]
			g.options = append(g.options, strings.TrimSpace(s.value))
		default:
			g := groups[len(groups)-1]
			g.letters = append(g.letters, s.value)
		}
	}
	if len(groups) < 1 || len(groups) > protocol.MaxQuestions {
		return nil, fmt.Errorf("%d questions, want 1 to %d", len(groups), protocol.MaxQuestions)
	}
	var out []protocol.Question
	for i, g := range groups {
		n := i + 1
		switch {
		case g.text == "":
			return nil, fmt.Errorf("question %d is empty", n)
		case len(g.options) < 2 || len(g.options) > len(protocol.Letters):
			return nil, fmt.Errorf("question %d: %d options, want 2 to %d", n, len(g.options), len(protocol.Letters))
		case slices.Contains(g.options, ""):
			return nil, fmt.Errorf("question %d: an option is empty", n)
		case len(g.letters) != 1:
			return nil, fmt.Errorf("question %d: %d times --recommended, want exactly one", n, len(g.letters))
		}
		rec := strings.Index(protocol.Letters, g.letters[0])
		if len(g.letters[0]) != 1 || rec < 0 || rec >= len(g.options) {
			return nil, fmt.Errorf("question %d: --recommended %s points to no option", n, g.letters[0])
		}
		q := protocol.Question{Question: g.text}
		for j, text := range g.options {
			q.Options = append(q.Options, protocol.Option{Text: text, Recommended: j == rec})
		}
		out = append(out, q)
	}
	return out, nil
}
