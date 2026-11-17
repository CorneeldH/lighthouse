package pusher

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const pin = "0123456789abcdef0123456789abcdef01234567"

// checkedWith commits files (path → content) on the branch, runs the floor and returns the
// request and what the floor read: the policy then reads the pusher's own clone.
func checkedWith(t *testing.T, files map[string]string) (*fixture, Checked) {
	t.Helper()
	f := newFixture(t)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f.commit(name, files[name])
	}
	f.branchRecord(done(f.head()))
	c, red := floor(t, f.req)
	wantGreen(t, red)
	return f, c
}

func policyOf(t *testing.T, files map[string]string) (Class, []Violation) {
	t.Helper()
	f, c := checkedWith(t, files)
	class, violations, red := Policy(context.Background(), f.req, c)
	if red != nil {
		t.Fatalf("Policy red at %q: %s", red.Step, red.Why)
	}
	return class, violations
}

// rules is the sorted rule names of violations.
func rules(violations []Violation) []string {
	out := []string{}
	for _, v := range violations {
		out = append(out, v.Rule)
	}
	sort.Strings(out)
	return out
}

func wantRules(t *testing.T, violations []Violation, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := rules(violations); !reflect.DeepEqual(got, want) {
		t.Fatalf("violations %+v, want rules %v", violations, want)
	}
	for _, v := range violations {
		if v.File == "" || v.Why == "" {
			t.Errorf("violation %+v without file or reason", v)
		}
	}
}

const scheduleWF = `name: nightly
on:
  schedule:
    - cron: "0 3 * * *"
permissions:
  contents: read
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@` + pin + `
`

func wf(on, permissions, uses string) string {
	return "on: " + on + "\npermissions: " + permissions + "\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: " + uses + "\n"
}

func TestPolicyUnpinnedAction(t *testing.T) {
	cases := map[string]string{
		"tag":         "actions/checkout@v4",
		"branch":      "actions/checkout@main",
		"short sha":   "actions/checkout@0123456",
		"no ref":      "actions/checkout",
		"docker tag":  "docker://alpine:3.20",
		"expression":  "${{ inputs.action }}",
		"sha of 41":   "actions/checkout@" + pin + "0",
		"upper hex":   "actions/checkout@" + strings.ToUpper(pin),
		"sha in path": "actions/checkout/" + pin + "@v4",
	}
	for name, uses := range cases {
		t.Run(name, func(t *testing.T) {
			_, v := policyOf(t, map[string]string{".github/workflows/x.yml": wf("schedule", "read-all", uses)})
			wantRules(t, v, RuleUnpinned)
		})
	}
}

func TestPolicyUnpinnedReusableWorkflow(t *testing.T) {
	text := "on: schedule\npermissions: {}\njobs:\n  call:\n    uses: o/r/.github/workflows/w.yml@main\n"
	_, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
	wantRules(t, v, RuleUnpinned)
}

func TestPolicyUnpinnedBehindAnchorAndMerge(t *testing.T) {
	text := `on: schedule
permissions: {}
x-step: &step
  uses: actions/checkout@v4
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - <<: *step
        name: checkout
`
	_, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
	wantRules(t, v, RuleUnpinned)
}

func TestPolicyPullRequestTarget(t *testing.T) {
	for name, on := range map[string]string{
		"string":       "pull_request_target",
		"list":         "[push, pull_request_target]",
		"map":          "{pull_request_target: {branches: [main]}}",
		"workflow_run": "{workflow_run: {workflows: [ci]}}",
	} {
		t.Run(name, func(t *testing.T) {
			class, v := policyOf(t, map[string]string{".github/workflows/x.yml": wf(on, "read-all", "./local")})
			wantRules(t, v, RuleTrigger)
			if class != PreMerge {
				t.Errorf("class %v, want PreMerge", class)
			}
		})
	}
}

func TestPolicyTriggerUnderYAML11TrueKey(t *testing.T) {
	text := "true: pull_request_target\npermissions: {}\njobs: {}\n"
	_, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
	wantRules(t, v, RuleTrigger)
}

func TestPolicyNoPermissions(t *testing.T) {
	text := "on: schedule\njobs:\n  a:\n    runs-on: ubuntu-latest\n    permissions: read-all\n    steps:\n      - run: echo\n"
	_, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
	wantRules(t, v, RulePermissions)
}

func TestPolicyWriteAll(t *testing.T) {
	for name, perms := range map[string]string{
		"workflow":   "write-all",
		"null":       "",
		"expression": "${{ inputs.p }}",
	} {
		t.Run(name, func(t *testing.T) {
			_, v := policyOf(t, map[string]string{".github/workflows/x.yml": wf("schedule", perms, "./local")})
			wantRules(t, v, RulePermissions)
		})
	}
	t.Run("job", func(t *testing.T) {
		text := "on: schedule\npermissions: {}\njobs:\n  a:\n    runs-on: ubuntu-latest\n    permissions: write-all\n    steps:\n      - run: echo\n"
		_, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
		wantRules(t, v, RulePermissions)
	})
}

func TestPolicyLocalAndDockerDigestAllowed(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	text := `on: schedule
permissions:
  contents: write
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
  ext:
    uses: o/r/.github/workflows/w.yml@` + pin + `
  a:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/x
      - uses: docker://alpine@sha256:` + digest + `
      - uses: actions/checkout@` + pin + `
      - run: echo
`
	class, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
	wantRules(t, v)
	if class != PostMerge {
		t.Errorf("class %v, want PostMerge", class)
	}
}

func TestPolicyMalformedWorkflow(t *testing.T) {
	for name, text := range map[string]string{
		"syntax":         "on: [schedule\n",
		"not a map":      "- a\n- b\n",
		"duplicate key":  "on: schedule\non: push\npermissions: {}\n",
		"two documents":  scheduleWF + "---\non: pull_request_target\n",
		"steps not list": "on: schedule\npermissions: {}\njobs:\n  a:\n    steps: {uses: x@v1}\n",
	} {
		t.Run(name, func(t *testing.T) {
			class, v := policyOf(t, map[string]string{".github/workflows/x.yml": text})
			wantRules(t, v, RuleParse)
			if class != PreMerge {
				t.Errorf("class %v, want PreMerge", class)
			}
		})
	}
}

func TestPolicyIgnoresOtherFilesAndDeletedWorkflow(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.main, ".github/workflows/old.yml"), wf("pull_request_target", "write-all", "a/b@v1"))
	tgit(t, f.main, "add", ".")
	tgit(t, f.main, "commit", "-q", "-m", "old")
	tgit(t, f.main, "push", "-q", "origin", "main")
	tgit(t, f.wt, "merge", "-q", "--no-edit", "main")
	tgit(t, f.wt, "rm", "-q", ".github/workflows/old.yml")
	tgit(t, f.wt, "commit", "-q", "-m", "rm old")
	f.commit("docs/x.yml", "on: pull_request_target\n")
	f.commit(".github/workflows/sub/y.yml", "on: pull_request_target\n")
	f.branchRecord(done(f.head()))
	c, red := floor(t, f.req)
	wantGreen(t, red)
	class, v, red := Policy(context.Background(), f.req, c)
	if red != nil {
		t.Fatalf("Policy red: %s", red.Why)
	}
	wantRules(t, v)
	if class != PostMerge {
		t.Errorf("class %v, want PostMerge", class)
	}
}

func TestPolicySymlinkWorkflowIsViolation(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Join(f.wt, ".github/workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../README", filepath.Join(f.wt, ".github/workflows/x.yml")); err != nil {
		t.Fatal(err)
	}
	tgit(t, f.wt, "add", ".")
	tgit(t, f.wt, "commit", "-q", "-m", "link")
	f.branchRecord(done(f.head()))
	c, red := floor(t, f.req)
	wantGreen(t, red)
	_, v, red := Policy(context.Background(), f.req, c)
	if red != nil {
		t.Fatalf("Policy red: %s", red.Why)
	}
	wantRules(t, v, RuleParse)
}

func classOf(t *testing.T, on string) Class {
	t.Helper()
	class, v := policyOf(t, map[string]string{".github/workflows/x.yml": wf(on, "read-all", "./local")})
	wantRules(t, v)
	return class
}

func TestClassPostMergeSchedule(t *testing.T) {
	class, v := policyOf(t, map[string]string{".github/workflows/x.yml": scheduleWF})
	wantRules(t, v)
	if class != PostMerge {
		t.Errorf("class %v, want PostMerge", class)
	}
}

func TestClassPostMergePushDefaultOnly(t *testing.T) {
	for name, on := range map[string]string{
		"push":          "{push: {branches: [main]}}",
		"push+schedule": "{push: {branches: [main]}, schedule: [{cron: '0 3 * * *'}]}",
	} {
		t.Run(name, func(t *testing.T) {
			if got := classOf(t, on); got != PostMerge {
				t.Errorf("class %v, want PostMerge", got)
			}
		})
	}
}

func TestClassPreMergeWorkflowDispatch(t *testing.T) {
	if got := classOf(t, "{schedule: [{cron: '0 3 * * *'}], workflow_dispatch: {}}"); got != PreMerge {
		t.Errorf("class %v, want PreMerge", got)
	}
}

func TestClassPreMergeCall(t *testing.T) {
	if got := classOf(t, "{workflow_call: {}}"); got != PreMerge {
		t.Errorf("class %v, want PreMerge", got)
	}
}

func TestClassPreMergePushNoFilter(t *testing.T) {
	for name, on := range map[string]string{
		"string":          "push",
		"list":            "[push]",
		"null":            "{push: }",
		"empty map":       "{push: {}}",
		"other branch":    "{push: {branches: [dev]}}",
		"two branches":    "{push: {branches: [main, dev]}}",
		"glob":            "{push: {branches: ['*']}}",
		"branch string":   "{push: {branches: main}}",
		"branches-ignore": "{push: {branches-ignore: [dev]}}",
		"paths":           "{push: {branches: [main], paths: [a]}}",
		"pull_request":    "{pull_request: {branches: [main]}}",
		"no on":           "",
	} {
		t.Run(name, func(t *testing.T) {
			if got := classOf(t, on); got != PreMerge {
				t.Errorf("class %v, want PreMerge", got)
			}
		})
	}
}

func TestClassPostMergePushWithTagsIsPre(t *testing.T) {
	for name, on := range map[string]string{
		"tags":        "{push: {branches: [main], tags: ['v*']}}",
		"tags-ignore": "{push: {branches: [main], tags-ignore: ['x']}}",
	} {
		t.Run(name, func(t *testing.T) {
			if got := classOf(t, on); got != PreMerge {
				t.Errorf("class %v, want PreMerge", got)
			}
		})
	}
}

func TestClassPreMergeWhenOneOfTwoIs(t *testing.T) {
	class, v := policyOf(t, map[string]string{
		".github/workflows/a.yml":  scheduleWF,
		".github/workflows/b.yaml": wf("{push: {branches: [dev]}}", "read-all", "./x"),
	})
	wantRules(t, v)
	if class != PreMerge {
		t.Errorf("class %v, want PreMerge", class)
	}
}
