package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

const laneToml = "[forge]\nkind = \"github\"\nrepo = \"o/r\"\n\n[pins.broken]\nkind = \"tool\"\nrepo = \"o/broken\"\n"

func runLane(t *testing.T, setup laneSetup) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.ResetCommands()
	root.AddCommand(newPinsCommand(nil, setup))
	root.SetArgs([]string{"pins", "lane"})
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func noEnv(string) string { return "" }

// Fails when the lane runs without a token (it would write anonymously and fail per pin), says
// nothing clear about it, exits 0, or reaches the forge at all.
func TestPinsLaneCommand_requiresToken(t *testing.T) {
	pinsRepo(t, laneToml)
	srv := forgetest.New(t)
	code, _, stderr := runLane(t, laneEnvFrom(noEnv, srv.URL()))
	if code == 0 {
		t.Fatalf("exit 0 without a token")
	}
	if !strings.Contains(stderr, "COULD NOT CHECK") || !strings.Contains(stderr, "GH_TOKEN") {
		t.Fatalf("stderr %q: want COULD NOT CHECK naming the token variable", stderr)
	}
	if n := srv.Count("/repos/o/r/issues"); n != 0 || len(srv.Posts) != 0 {
		t.Fatalf("the lane reached the forge without a token: %d issue requests, posts %+v", n, srv.Posts)
	}
}

// Fails when a pin that cannot be measured does not get its error issue, or when the command
// then exits 0 or hides which pin failed. It is also the positive control of the test above:
// with a token the same repository does reach the forge.
func TestPinsLaneCommand_errorPinWritesIssueAndExitsOne(t *testing.T) {
	pinsRepo(t, laneToml)
	srv := forgetest.New(t)
	getenv := func(k string) string {
		if k == "GH_TOKEN" {
			return "secret"
		}
		return ""
	}
	code, stdout, stderr := runLane(t, laneEnvFrom(getenv, srv.URL()))
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "pin broken") {
		t.Fatalf("stderr %q does not name the failed pin", stderr)
	}
	if !strings.Contains(stdout, "broken: error") {
		t.Errorf("stdout %q has no line for the pin", stdout)
	}
	if len(srv.Posts) != 1 || srv.Posts[0].Path != "/repos/o/r/issues" ||
		!strings.Contains(srv.Posts[0].Body["body"].(string), "lighthouse-pin: broken error") {
		t.Fatalf("posts %+v, want one error issue for broken", srv.Posts)
	}
}

// Fails when the lane does not take [ci] bot_login from lighthouse.toml as the author of its own
// items (so forged markers would count), or invents one when the key is not set.
func TestLaneEnvFrom_botLogin(t *testing.T) {
	getenv := func(k string) string {
		if k == "GH_TOKEN" {
			return "secret"
		}
		return ""
	}
	for _, tc := range []struct{ toml, want string }{
		{laneToml + "\n[ci]\nbot_login = \"paia-bot[bot]\"\n", "paia-bot[bot]"},
		{laneToml, ""},
	} {
		root := pinsRepo(t, tc.toml)
		env, err := laneEnvFrom(getenv, "http://127.0.0.1:1")(root)
		if err != nil {
			t.Fatalf("laneEnvFrom: %v", err)
		}
		if env.BotLogin != tc.want {
			t.Errorf("BotLogin %q, want %q", env.BotLogin, tc.want)
		}
	}
}

type workflowStep struct {
	ID   string            `yaml:"id"`
	Uses string            `yaml:"uses"`
	With map[string]any    `yaml:"with"`
	Run  string            `yaml:"run"`
	Env  map[string]string `yaml:"env"`
}

type workflow struct {
	On          map[string]any `yaml:"on"`
	Permissions any            `yaml:"permissions"`
	Concurrency struct {
		Group            string `yaml:"group"`
		CancelInProgress *bool  `yaml:"cancel-in-progress"`
	} `yaml:"concurrency"`
	Jobs map[string]struct {
		Permissions    any            `yaml:"permissions"`
		TimeoutMinutes int            `yaml:"timeout-minutes"`
		Steps          []workflowStep `yaml:"steps"`
	} `yaml:"jobs"`
}

// readOnly reports whether a permissions value grants no write: absent, {}, read-all or a map
// of read and none.
func readOnly(p any) bool {
	switch v := p.(type) {
	case nil:
		return true
	case string:
		return v == "read-all"
	case map[string]any:
		for _, level := range v {
			if level != "read" && level != "none" {
				return false
			}
		}
		return true
	}
	return false
}

// Fails when pins.yml loses its weekly schedule or manual start, lets two runs overlap, grants
// GITHUB_TOKEN a write, asks the App for less than contents, pull-requests and issues write,
// checks out without the App token (so the push to pins/* would not use it), stops running
// `pins lane` with that token, or loses the job time-out that frees the concurrency group.
func TestPinsWorkflow_shape(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "pins.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var wf workflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		t.Fatal(err)
	}
	if _, ok := wf.On["workflow_dispatch"]; !ok {
		t.Errorf("no workflow_dispatch")
	}
	schedule, _ := wf.On["schedule"].([]any)
	if len(schedule) != 1 {
		t.Fatalf("schedule %v, want one cron", wf.On["schedule"])
	}
	cron, _ := schedule[0].(map[string]any)["cron"].(string)
	if !regexp.MustCompile(`^\d+ \d+ \* \* [0-6]$`).MatchString(cron) {
		t.Errorf("cron %q is not weekly", cron)
	}
	if wf.Concurrency.Group == "" || wf.Concurrency.CancelInProgress == nil || *wf.Concurrency.CancelInProgress {
		t.Errorf("concurrency %+v, want one group with cancel-in-progress false", wf.Concurrency)
	}
	if wf.Permissions == nil || !readOnly(wf.Permissions) {
		t.Errorf("workflow permissions %v, want {} or read only", wf.Permissions)
	}
	if len(wf.Jobs) != 1 {
		t.Fatalf("%d jobs, want 1", len(wf.Jobs))
	}
	for name, job := range wf.Jobs {
		if !readOnly(job.Permissions) {
			t.Errorf("job %s permissions %v grant a write", name, job.Permissions)
		}
		if job.TimeoutMinutes <= 0 || job.TimeoutMinutes > 60 {
			t.Errorf("job %s timeout-minutes %d, want a bound of at most an hour", name, job.TimeoutMinutes)
		}
		var app, checkout, lane = -1, -1, -1
		for i, s := range job.Steps {
			switch {
			case strings.HasPrefix(s.Uses, "actions/create-github-app-token@"):
				app = i
			case strings.HasPrefix(s.Uses, "actions/checkout@"):
				checkout = i
			case strings.Contains(s.Run, "go run ./cmd/lighthouse pins lane"):
				lane = i
			}
			if s.Uses != "" && !regexp.MustCompile(`@[0-9a-f]{40}$`).MatchString(s.Uses) {
				t.Errorf("step %d: %s is not pinned to a commit", i, s.Uses)
			}
		}
		if app < 0 || checkout < app || lane < checkout {
			t.Fatalf("steps app %d, checkout %d, lane %d: want App token, then checkout, then the lane", app, checkout, lane)
		}
		a := job.Steps[app]
		for _, perm := range []string{"permission-contents", "permission-pull-requests", "permission-issues"} {
			if a.With[perm] != "write" {
				t.Errorf("App token %s = %v, want write", perm, a.With[perm])
			}
		}
		token := "${{ steps." + a.ID + ".outputs.token }}"
		if a.ID == "" {
			t.Fatalf("App token step has no id")
		}
		c := job.Steps[checkout]
		if c.With["token"] != token || c.With["persist-credentials"] != true {
			t.Errorf("checkout with %v, want token %s and persist-credentials true", c.With, token)
		}
		if c.With["fetch-depth"] != 0 {
			t.Errorf("checkout fetch-depth %v, want 0 (full history)", c.With["fetch-depth"])
		}
		if got := job.Steps[lane].Env["GH_TOKEN"]; got != token {
			t.Errorf("lane GH_TOKEN %q, want %s", got, token)
		}
	}
}
