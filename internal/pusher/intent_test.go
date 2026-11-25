package pusher

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// baseWF is the workflow on the merge-base that every intentOK case changes.
const baseWF = `on:
  pull_request:
    paths: ["src/**"]
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@` + pin + `
      - run: make
        env:
          FOO: bar
          TOKEN: ${{ secrets.DEPLOY }}
`

// postWF runs only after the merge: a push to exactly the default branch.
const postWF = `on:
  push:
    branches: [main]
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
`

// exprWF takes its container image, service image and environment from the matrix and its
// runner from an input, each in its own job so that each case meets only one of them.
const exprWF = `on:
  workflow_dispatch:
    inputs:
      runner:
        type: choice
        options: [ubuntu-latest]
permissions:
  contents: read
jobs:
  box:
    strategy:
      matrix:
        img: [node:20]
    runs-on: ubuntu-latest
    container: ${{ matrix.img }}
    steps:
      - run: make
        env:
          FOO: bar
  svc:
    strategy:
      matrix:
        db: [postgres:16]
    runs-on: ubuntu-latest
    services:
      db:
        image: ${{ matrix.db }}
    steps:
      - run: make
  deploy:
    strategy:
      matrix:
        env: [staging]
    runs-on: ubuntu-latest
    environment: ${{ matrix.env }}
    steps:
      - run: make
  manual:
    runs-on: ${{ inputs.runner }}
    steps:
      - run: make
`

// needsJobs take a runner from the output of a job, which its run: step writes.
const needsJobs = `  pick:
    runs-on: ubuntu-latest
    outputs:
      r: ${{ steps.p.outputs.r }}
    steps:
      - id: p
        run: echo r=ubuntu-latest >> "$GITHUB_OUTPUT"
  after:
    needs: pick
    runs-on: ${{ needs.pick.outputs.r }}
    steps:
      - run: make
`

// boxJob runs in a container with options and has a service.
const boxJob = `  box:
    runs-on: ubuntu-latest
    container:
      image: node:20
      options: --cpus 1
    services:
      db:
        image: postgres:16
    steps:
      - run: make
`

// swap replaces old by repl in text and fails when old is not there, so every case changes
// what it says it changes.
func swap(t *testing.T, text, old, repl string) string {
	t.Helper()
	if !strings.Contains(text, old) {
		t.Fatalf("%q not in the workflow", old)
	}
	return strings.Replace(text, old, repl, 1)
}

func parsed(t *testing.T, text string) map[string]any {
	t.Helper()
	m, err := parseWorkflow([]byte(text))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, text)
	}
	return m
}

// inheritJob calls a reusable workflow with every secret.
const inheritJob = "  call:\n    uses: o/r/.github/workflows/x.yml@" + pin + "\n    secrets: inherit\n"

func TestIntentOKInClass(t *testing.T) {
	for name, tc := range map[string]struct{ old, new string }{
		"paths filter": {baseWF, swap(t, baseWF, `"src/**"`, `"src/**", "cmd/**"`)},
		"new run step": {baseWF, swap(t, baseWF, "      - run: make\n", "      - run: make\n      - run: make test\n")},
		"new job without new rights": {baseWF,
			baseWF + "  lint:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: actions/checkout@" + pin + "\n      - run: make lint\n"},
		"env change":            {baseWF, swap(t, baseWF, "FOO: bar", "FOO: baz")},
		"narrower permission":   {baseWF, swap(t, baseWF, "contents: read", "contents: none")},
		"secret dropped":        {baseWF, swap(t, baseWF, "          TOKEN: ${{ secrets.DEPLOY }}\n", "")},
		"secret name, any case": {baseWF, swap(t, baseWF, "secrets.DEPLOY", "SECRETS.deploy")},
		"env change next to expression-valued runner and images": {exprWF, swap(t, exprWF, "FOO: bar", "FOO: baz")},
		"github.sha and github.event stay in": {baseWF,
			swap(t, baseWF, "FOO: bar", "FOO: ${{ github.sha }}-${{ github.event.number }}")},
		"env change next to a container with options": {baseWF + boxJob, swap(t, baseWF+boxJob, "FOO: bar", "FOO: baz")},
		"secrets of a reusable workflow kept": {baseWF + inheritJob,
			swap(t, baseWF+inheritJob, "FOO: bar", "FOO: baz")},
	} {
		t.Run(name, func(t *testing.T) {
			ok, why := intentOK(parsed(t, tc.old), parsed(t, tc.new), "main")
			if !ok {
				t.Fatalf("outside the class (%s), want inside", why)
			}
		})
	}
}

func TestIntentOKOutsideClass(t *testing.T) {
	for name, tc := range map[string]struct {
		old, new string
		reason   string // what the reason names: the first broken requirement
	}{
		"new event": {baseWF, swap(t, baseWF, "on:\n", "on:\n  workflow_dispatch:\n"), "event"},
		"push on default gains pull_request": {postWF,
			swap(t, postWF, "on:\n", "on:\n  pull_request:\n"), "event"},
		"push branches widen to issue branches": {postWF,
			swap(t, postWF, "branches: [main]", "branches: [main, 'issue-*']"), "post-merge"},
		"push branches filter dropped": {postWF,
			swap(t, postWF, "  push:\n    branches: [main]\n", "  push:\n    paths: [\"src/**\"]\n"), "post-merge"},
		"new action": {baseWF,
			swap(t, baseWF, "      - run: make\n", "      - uses: actions/setup-go@"+pin+"\n      - run: make\n"), "action"},
		"other pin of the same action": {baseWF,
			swap(t, baseWF, "actions/checkout@"+pin, "actions/checkout@"+strings.Repeat("f", 40)), "action"},
		"new reusable workflow": {baseWF,
			baseWF + "  call:\n    uses: o/r/.github/workflows/x.yml@" + pin + "\n", "action"},
		"contents read to write": {baseWF, swap(t, baseWF, "contents: read", "contents: write"), "permission"},
		"new scope": {baseWF,
			swap(t, baseWF, "contents: read", "contents: read\n  id-token: write"), "permission"},
		"write-all above read": {baseWF,
			swap(t, baseWF, "permissions:\n  contents: read", "permissions: write-all"), "permission"},
		"read-all above a map of read": {baseWF,
			swap(t, baseWF, "permissions:\n  contents: read", "permissions: read-all"), "permission"},
		// The top level stays write; the job drops its own read and inherits write.
		"job without permissions inherits a wider top level": {
			swap(t, swap(t, baseWF, "contents: read", "contents: write"),
				"    runs-on: ubuntu-latest\n", "    permissions:\n      contents: read\n    runs-on: ubuntu-latest\n"),
			swap(t, baseWF, "contents: read", "contents: write"), "permission"},
		"job permission wider than its own on the merge-base": {
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    permissions:\n      contents: none\n    runs-on: ubuntu-latest\n"),
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    permissions:\n      contents: read\n    runs-on: ubuntu-latest\n"), "permission"},
		"new job wider than the top level of the merge-base": {baseWF,
			baseWF + "  rel:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: write\n    steps:\n      - run: x\n", "permission"},
		"new job where the merge-base had no top level": {
			swap(t, baseWF, "permissions:\n  contents: read\n", ""),
			swap(t, baseWF, "permissions:\n  contents: read\n", "") +
				"  rel:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    steps:\n      - run: x\n", "permission"},
		"top level dropped": {baseWF, swap(t, baseWF, "permissions:\n  contents: read\n", ""), "permission"},
		"permission by expression": {baseWF,
			swap(t, baseWF, "contents: read", "contents: ${{ inputs.level }}"), "permission"},
		"new secret":             {baseWF, swap(t, baseWF, "FOO: bar", "FOO: ${{ secrets.OTHER }}"), "secret"},
		"secret by index":        {baseWF, swap(t, baseWF, "secrets.DEPLOY", "secrets['DEPLOY']"), "secret"},
		"secret by matrix index": {baseWF, swap(t, baseWF, "secrets.DEPLOY", "secrets[matrix.n]"), "secret"},
		"all secrets as JSON":    {baseWF, swap(t, baseWF, "secrets.DEPLOY", "toJSON(secrets)"), "secret"},
		"secrets inherit": {baseWF,
			baseWF + "  call:\n    uses: actions/checkout@" + pin + "\n    secrets: inherit\n", "secret"},
		"new github.token":          {baseWF, swap(t, baseWF, "FOO: bar", "FOO: ${{ github.token }}"), "token"},
		"new GITHUB_TOKEN":          {baseWF, swap(t, baseWF, "FOO: bar", "GITHUB_TOKEN: x"), "token"},
		"token by index":            {baseWF, swap(t, baseWF, "FOO: bar", "FOO: ${{ github['token'] }}"), "token"},
		"github as JSON":            {baseWF, swap(t, baseWF, "FOO: bar", "FOO: ${{ toJSON(github) }}"), "token"},
		"every github property":     {baseWF, swap(t, baseWF, "FOO: bar", `FOO: ${{ join(github.*, ',') }}`), "token"},
		"github as format argument": {baseWF, swap(t, baseWF, "FOO: bar", `FOO: ${{ format('{0}', github) }}`), "token"},
		"container options added": {baseWF + boxJob,
			swap(t, baseWF+boxJob, "      options: --cpus 1\n", "      options: --privileged\n"), "image"},
		"container volume added": {baseWF + boxJob,
			swap(t, baseWF+boxJob, "      image: node:20\n", "      image: node:20\n      volumes: [\"/:/host\"]\n"), "image"},
		"service options added": {baseWF + boxJob,
			swap(t, baseWF+boxJob, "        image: postgres:16\n", "        image: postgres:16\n        options: --privileged\n"), "image"},
		"merge-base unreadable": {"on: pull_request\npermissions:\n  contents: read\njobs: [a]\n",
			"on: pull_request\npermissions:\n  contents: read\njobs:\n  a:\n    steps:\n      - run: x\n", "merge-base"},
		"new runs-on": {baseWF, swap(t, baseWF, "runs-on: ubuntu-latest", "runs-on: self-hosted"), "runner"},
		"new matrix runner": {
			swap(t, baseWF, "runs-on: ubuntu-latest", "strategy:\n      matrix:\n        os: [ubuntu-latest]\n    runs-on: ${{ matrix.os }}"),
			swap(t, baseWF, "runs-on: ubuntu-latest", "strategy:\n      matrix:\n        os: [ubuntu-latest, self-hosted]\n    runs-on: ${{ matrix.os }}"), "runner"},
		"new environment": {baseWF,
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    environment: production\n"), "environment"},
		"new container image": {baseWF,
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    container: node:20\n"), "image"},
		"new container image in a map": {baseWF,
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    container:\n      image: node:20\n"), "image"},
		"new service image": {baseWF,
			swap(t, baseWF, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    services:\n      db:\n        image: postgres:16\n"), "image"},
		"case variant of a key the check reads": {baseWF,
			swap(t, baseWF, "      - run: make\n", "      - Uses: evil/x@"+pin+"\n      - run: make\n"), "case variant"},
		"matrix container image": {exprWF, swap(t, exprWF, "img: [node:20]", "img: [node:20, evil:1]"), "image"},
		"matrix service image":   {exprWF, swap(t, exprWF, "db: [postgres:16]", "db: [postgres:16, evil:1]"), "image"},
		"matrix environment":     {exprWF, swap(t, exprWF, "env: [staging]", "env: [staging, production]"), "environment"},
		"input-driven runner": {exprWF,
			swap(t, exprWF, "options: [ubuntu-latest]", "options: [ubuntu-latest, self-hosted]"), "runner"},
		"runner from a job output": {exprWF + needsJobs,
			swap(t, exprWF+needsJobs, "echo r=ubuntu-latest", "echo r=self-hosted"), "runner"},
		"case variant at the top level": {baseWF, baseWF + "Permissions: write-all\n", "case variant"},
		"jobs not a map":                {baseWF, "on: pull_request\npermissions:\n  contents: read\njobs: [a]\n", "jobs"},
	} {
		t.Run(name, func(t *testing.T) {
			ok, why := intentOK(parsed(t, tc.old), parsed(t, tc.new), "main")
			if ok {
				t.Fatalf("inside the class, want outside (%s)", tc.reason)
			}
			if !strings.Contains(why, tc.reason) {
				t.Errorf("reason %q does not name %q", why, tc.reason)
			}
		})
	}
}

// intentFixture puts baseWF (and an extra workflow) on origin/main, so the merge-base has them.
func intentFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	write(t, filepath.Join(f.main, ".github/workflows/ci.yml"), baseWF)
	write(t, filepath.Join(f.main, ".github/workflows/old.yml"), postWF)
	tgit(t, f.main, "add", ".")
	tgit(t, f.main, "commit", "-q", "-m", "workflows")
	tgit(t, f.main, "push", "-q", "origin", "main")
	tgit(t, f.wt, "merge", "-q", "--no-edit", "main")
	return f
}

func inIntent(t *testing.T, f *fixture) (bool, string) {
	t.Helper()
	f.branchRecord(done(f.head()))
	c, red := floor(t, f.req)
	wantGreen(t, red)
	return InIntentClass(context.Background(), f.req, c)
}

func TestInIntentClassNewFileAndUnparsable(t *testing.T) {
	t.Run("change in the class next to a deleted workflow", func(t *testing.T) {
		f := intentFixture(t)
		tgit(t, f.wt, "rm", "-q", ".github/workflows/old.yml")
		tgit(t, f.wt, "commit", "-q", "-m", "rm old")
		f.commit(".github/workflows/ci.yml", swap(t, baseWF, "FOO: bar", "FOO: baz"))
		if ok, why := inIntent(t, f); !ok {
			t.Fatalf("outside the class (%s), want inside", why)
		}
	})
	t.Run("change outside the class", func(t *testing.T) {
		f := intentFixture(t)
		f.commit(".github/workflows/ci.yml", swap(t, baseWF, "contents: read", "contents: write"))
		if ok, why := inIntent(t, f); ok || !strings.Contains(why, "permission") {
			t.Fatalf("got %v (%s), want outside for a permission", ok, why)
		}
	})
	t.Run("new workflow", func(t *testing.T) {
		f := intentFixture(t)
		f.commit(".github/workflows/new.yml", baseWF)
		if ok, why := inIntent(t, f); ok || !strings.Contains(why, "new.yml") {
			t.Fatalf("got %v (%s), want outside naming new.yml", ok, why)
		}
	})
	t.Run("unparsable workflow", func(t *testing.T) {
		f := intentFixture(t)
		f.commit(".github/workflows/ci.yml", "on: [\n")
		if ok, why := inIntent(t, f); ok || !strings.Contains(why, "ci.yml") {
			t.Fatalf("got %v (%s), want outside naming ci.yml", ok, why)
		}
	})
	t.Run("unreadable merge-base", func(t *testing.T) {
		f := intentFixture(t)
		f.commit(".github/workflows/ci.yml", swap(t, baseWF, "FOO: bar", "FOO: baz"))
		f.branchRecord(done(f.head()))
		c, red := floor(t, f.req)
		wantGreen(t, red)
		c.MergeBase = strings.Repeat("e", 40) // no such commit in the clone
		if ok, why := InIntentClass(context.Background(), f.req, c); ok || why == "" {
			t.Fatalf("got %v (%q), want outside with a reason", ok, why)
		}
	})
}

func TestCanonDistinguishesStringFromList(t *testing.T) {
	if canon("[ubuntu-latest]") == canon([]any{"ubuntu-latest"}) {
		t.Fatal("canon: a string and a list of the same text collide")
	}
	if canon(map[string]any{"a": "b"}) == canon("map[a:b]") {
		t.Fatal("canon: a string and a map of the same text collide")
	}
}
