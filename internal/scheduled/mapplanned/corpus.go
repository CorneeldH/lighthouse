package mapplanned

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/protocol"
	"github.com/CorneeldH/lighthouse/internal/scheduled"
	"github.com/CorneeldH/lighthouse/internal/scheduled/safefs"
)

// The corpus is input for the agent and never authority: the agent can edit it, so nothing
// after the session (Cleanup, Allowed, Check) reads it. The result file is the agent's output;
// Cleanup validates it as a whole against labels.md at the base of the run and the forge.

type corpus struct {
	Language  string           `json:"language"`
	Taxonomy  string           `json:"taxonomy"`
	Tenets    string           `json:"tenets"`
	Areas     string           `json:"areas"`
	Documents []corpusDocument `json:"documents"`
	Issues    []corpusIssue    `json:"issues"`
}

type corpusDocument struct {
	Label    string  `json:"label"`
	Document string  `json:"document"`
	Planned  Planned `json:"planned"`
}

type corpusIssue struct {
	Number    int           `json:"number"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	Labels    []string      `json:"labels"`
	Milestone string        `json:"milestone"`
	Label     string        `json:"label"`
	Document  string        `json:"document"`
	Answer    *corpusAnswer `json:"answer"`
}

type corpusAnswer struct {
	Topic  Topic  `json:"topic"`
	Letter string `json:"letter"`
	Text   string `json:"text"`
}

// buildCorpus is the corpus of the rows and the issues due under them, in the repo of env.
func buildCorpus(env scheduled.Env, rows []Row, due []dueIssue) ([]byte, error) {
	c := corpus{
		Language:  env.Repo.Language,
		Taxonomy:  env.Repo.Paths.Labels,
		Tenets:    env.Repo.Paths.Tenets,
		Areas:     env.Repo.Paths.Areas,
		Documents: []corpusDocument{},
		Issues:    []corpusIssue{},
	}
	for _, r := range rows {
		c.Documents = append(c.Documents, corpusDocument(r))
	}
	for _, d := range due {
		is := corpusIssue{
			Number: d.Issue.Number, Title: d.Issue.Title, Body: d.Issue.Body,
			Labels: append([]string{}, d.Issue.Labels...), Label: d.Row.Label, Document: d.Row.Document,
		}
		if d.Issue.Milestone != nil {
			is.Milestone = d.Issue.Milestone.Title
		}
		if d.Answer != nil {
			is.Answer = &corpusAnswer{Topic: d.Answer.Topic, Letter: d.Answer.Letter, Text: d.Answer.Text}
		}
		c.Issues = append(c.Issues, is)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// result is the result file the agent writes: questions to post and decisions that an issue
// gets no item.
type result struct {
	Questions []resultQuestion `json:"questions"`
	Decisions []resultDecision `json:"decisions"`
}

type resultQuestion struct {
	Issue       int      `json:"issue"`
	Topic       Topic    `json:"topic"`
	Text        string   `json:"text"`
	Options     []string `json:"options"`
	Recommended string   `json:"recommended"`
	Off         string   `json:"off"`
}

type resultDecision struct {
	Issue int    `json:"issue"`
	Value string `json:"value"`
}

// decisionNoItem is the only decision value: the issue gets no item on the map.
const decisionNoItem = "no-item"

// readResult reads the result file in dir. A missing file is no result (nil, nil); a symlink
// or anything but a regular file, an unknown field, or trailing data is an error.
func readResult(dir string) (*result, error) {
	info, err := safefs.Stat(dir, resultPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a regular file", scheduled.ShowPath(resultPath))
	}
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(resultPath)))
	if err != nil {
		return nil, safefs.FileErr(resultPath, err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var res result
	if err := dec.Decode(&res); err != nil {
		return nil, fmt.Errorf("%s: %w", scheduled.ShowPath(resultPath), err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: data after the JSON object", scheduled.ShowPath(resultPath))
	}
	return &res, nil
}

// post is one comment Cleanup posts: its issue, its body, and whether it is a wish question
// (which also sets labels.needs_decision).
type post struct {
	issue int
	body  string
	wish  bool
}

// validate checks res as a whole, before any forge write, and returns the comments to post.
// rows are the planned rows of labels.md at the base. Every issue is an open issue on the forge
// that carries a label of a planned row, appears at most once, and every question has a topic,
// a text, 2 to 6 options and a recommended and an off letter among them.
func validate(ctx context.Context, env scheduled.Env, res *result, rows []Row) ([]post, error) {
	planned := map[string]bool{}
	for _, r := range rows {
		planned[r.Label] = true
	}
	seen := map[int]bool{}
	checkIssue := func(n int) error {
		if n <= 0 {
			return fmt.Errorf("issue %d is no issue number", n)
		}
		if seen[n] {
			return fmt.Errorf("#%d appears more than once", n)
		}
		seen[n] = true
		is, err := env.Forge.Issue(ctx, n)
		if err != nil {
			return fmt.Errorf("#%d: %w", n, err)
		}
		if is.State != "open" {
			return fmt.Errorf("#%d is not open", n)
		}
		if !slices.ContainsFunc(is.Labels, func(l string) bool { return planned[l] }) {
			return fmt.Errorf("#%d carries no label of a planned row", n)
		}
		return nil
	}
	var posts []post
	for _, q := range res.Questions {
		if err := checkQuestion(q); err != nil {
			return nil, fmt.Errorf("question on #%d: %w", q.Issue, err)
		}
		if err := checkIssue(q.Issue); err != nil {
			return nil, err
		}
		posts = append(posts, post{issue: q.Issue, wish: q.Topic == TopicWish, body: RenderQuestion(Question(q), env.Repo.Language)})
	}
	for _, d := range res.Decisions {
		if d.Value != decisionNoItem {
			return nil, fmt.Errorf("decision on #%d: value %q, want %q", d.Issue, d.Value, decisionNoItem)
		}
		if err := checkIssue(d.Issue); err != nil {
			return nil, err
		}
		posts = append(posts, post{issue: d.Issue, body: RenderDecision(d.Issue, env.Repo.Language)})
	}
	return posts, nil
}

// checkQuestion checks the form of one question: the gate reads its off letter back, so a
// question without one could never be settled. A wish question has exactly the two options of
// Question, with off "b".
func checkQuestion(q resultQuestion) error {
	if q.Topic != TopicPlace && q.Topic != TopicWish {
		return fmt.Errorf("topic %q, want %q or %q", q.Topic, TopicPlace, TopicWish)
	}
	if strings.TrimSpace(q.Text) == "" {
		return errors.New("no text")
	}
	if len(q.Options) < 2 || len(q.Options) > len(protocol.Letters) {
		return fmt.Errorf("%d options, want 2 to %d", len(q.Options), len(protocol.Letters))
	}
	if q.Topic == TopicWish && (len(q.Options) != 2 || q.Off != "b") {
		return fmt.Errorf("a wish question has 2 options with off \"b\", got %d with off %q", len(q.Options), q.Off)
	}
	letters := protocol.Letters[:len(q.Options)]
	for _, l := range []struct{ name, v string }{{"off", q.Off}, {"recommended", q.Recommended}} {
		if len(l.v) != 1 || !strings.Contains(letters, l.v) {
			return fmt.Errorf("%s %q is not one of the option letters %s", l.name, l.v, letters)
		}
	}
	return nil
}

// fragmentPrefix and fragmentDir name the changelog fragment Finish writes:
// changelog.d/scheduled-map-planned-<run>.md.
const (
	fragmentDir    = "changelog.d"
	fragmentPrefix = fragmentDir + "/scheduled-" + Name + "-"
)

// isFragment reports whether p has the form of the fragment Finish writes.
func isFragment(p string) bool {
	rest, ok := strings.CutPrefix(p, fragmentPrefix)
	if !ok {
		return false
	}
	run, ok := strings.CutSuffix(rest, ".md")
	return ok && run != "" && !strings.Contains(run, "/")
}

// fragmentBody is the content of the fragment, in language ("nl" or other). It is fixed, so
// Check can tell the fragment of Finish from one the agent wrote.
func fragmentBody(language string) string {
	line := "Scheduled run " + Name + ": open issues of planned labels put on their map document."
	if language == "nl" {
		line = "Geplande run " + Name + ": open issues van geplande labels op hun kaartdocument gezet."
	}
	return "---\ntype: internal\n---\n" + line + "\n"
}

// fragmentName is the fragment path of the run on branch: the branch without its scheduled/
// prefix and without the job name, every character outside [A-Za-z0-9._-] a dash.
func fragmentName(branch string) string {
	run := strings.TrimPrefix(branch, "scheduled/")
	run = strings.TrimPrefix(run, Name+"-")
	run = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, run)
	return fragmentPrefix + run + ".md"
}
