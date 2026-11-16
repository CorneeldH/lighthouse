package lane

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/pins"
)

// releaseStub serves fixed releases per repo.
type releaseStub map[string][]forge.Release

func (s releaseStub) Releases(_ context.Context, repo string) ([]forge.Release, error) {
	return s[repo], nil
}

const (
	clashFile = "pack/core/lib/clash.py"
	goneFile  = "pack/core/lib/gone.py"
)

// fixtureSources is the SOURCES.toml of the end-to-end fixture: the demo skill, a file pin
// whose merge conflicts and a file pin whose upstream path vanished.
func fixtureSources(pin string) string {
	return `[[file]]
path = "pack/core/hooks/g.sh"
new = true

[[file]]
path = "pack/core/skills/demo/SKILL.md"
pin = "core/skills/demo"

[[file]]
path = "pack/core/skills/demo/ref.md"
pin = "core/skills/demo"

[[file]]
path = "` + clashFile + `"
source_repo = "o/r"
source_sha = "` + pin + `"
source_path = "lib/clash.py"

[[file]]
path = "` + goneFile + `"
source_repo = "o/r"
source_sha = "` + pin + `"
source_path = "lib/gone.py"
`
}

const toolToml = "[pins.mytool]\nkind = \"tool\"\nrepo = \"o/mytool\"\nversion = \"1.0.0\"\n"

var toolReleases = releaseStub{"o/mytool": {
	{Tag: "v1.1.0", Name: "1.1", Body: "new @someone Fixes #9", URL: "https://github.com/o/mytool/releases/v1.1.0"},
	{Tag: "v1.0.0", Name: "1.0", Body: "old"},
}}

// e2eRepo is a repository with four pins behind upstream, each ending differently: the demo
// skill merges cleanly, clash.py conflicts, gone.py vanished upstream and mytool has a release.
func e2eRepo(t *testing.T) (root, origin string, up *upstream, head string) {
	t.Helper()
	isolateGit(t)
	up = newUpstream(t)
	pin := up.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillBase,
		"skills/demo/ref.md":   "a\nb\nc\n",
		"lib/clash.py":         "a = 1\n",
		"lib/gone.py":          "g\n",
	})
	head = up.commit(map[string]string{
		"skills/demo/SKILL.md": upSkillHead,
		"lib/clash.py":         "a = 2\n",
		"lib/gone.py":          "",
	})
	root, origin = initRepo(t, map[string]string{
		"lighthouse.toml":                  toolToml,
		"pack/SOURCES.toml":                fixtureSources(pin),
		"pack/core/hooks/g.sh":             "#!/bin/bash\necho g\n",
		"pack/core/hooks/hooks.json":       `{"hooks": {}}`,
		"pack/core/instructions/AGENTS.md": "## core\n\n- rule\n",
		demoSkill:                          localSkill(pin, localBody),
		"pack/core/skills/demo/ref.md":     "a\nb\nc\n",
		clashFile:                          "a = 3\n",
		goneFile:                           "g\n",
	})
	return root, origin, up, head
}

func runEnv(root string, up *upstream, f forge.Forge, rels forge.ReleaseReader) Env {
	return Env{
		Root: root,
		Git: pins.NewGitSourceAt(func(repo string) string {
			return "file://" + filepath.Join(up.root, filepath.FromSlash(repo))
		}),
		Releases: rels,
		Forge:    f,
		BotLogin: laneBot,
	}
}

// items returns the lane items of key on the fake forge.
func items(t *testing.T, f forge.Forge, key string) []forge.Item {
	t.Helper()
	got, err := f.FindByMarker(context.Background(), keyMarker(key))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// only fails unless exactly one item of key exists, and returns it with its marker kind.
func only(t *testing.T, f forge.Forge, key string) (forge.Item, ItemKind, string) {
	t.Helper()
	got := items(t, f, key)
	if len(got) != 1 {
		t.Fatalf("pin %s: %d items, want 1: %+v", key, len(got), got)
	}
	_, kind, target, _ := ParseMarker(got[0].Body)
	return got[0], kind, target
}

func pull(t *testing.T, srv *forgetest.Server, number int) map[string]any {
	t.Helper()
	for _, p := range srv.Pulls {
		if p["number"] == number {
			return p
		}
	}
	t.Fatalf("no pull request %d", number)
	return nil
}

func issueTitle(t *testing.T, srv *forgetest.Server, number int) string {
	t.Helper()
	it, ok := srv.Issues[number]
	if !ok {
		t.Fatalf("no item %d", number)
	}
	return it["title"].(string)
}

// Fails when a clean merge does not become one draft PR on its pushed branch, a conflict not an
// issue, a vanished path not an error issue, a tool behind not a behind issue, when a second run
// duplicates an item, or when the lane merges anything.
func TestRun_endToEndFixtures(t *testing.T) {
	root, origin, up, head := e2eRepo(t)
	srv, f := newFake(t)
	env := runEnv(root, up, f, toolReleases)
	var log bytes.Buffer
	env.Log = &log

	err := Run(context.Background(), env)
	if err == nil || !strings.Contains(err.Error(), goneFile) {
		t.Fatalf("Run: %v, want the error of the vanished pin", err)
	}
	for _, key := range []string{"core/skills/demo", clashFile, "mytool"} {
		if strings.Contains(err.Error(), key) {
			t.Errorf("error names %s, which did not fail: %v", key, err)
		}
	}

	pr, kind, target := only(t, f, "core/skills/demo")
	if !pr.IsPR || kind != KindPR || target != head {
		t.Fatalf("demo: %+v kind %s target %s, want a PR for %s", pr, kind, target, head)
	}
	p := pull(t, srv, pr.Number)
	if p["draft"] != true || p["base"].(map[string]any)["ref"] != "main" || pr.HeadRef != "pins/core-skills-demo" {
		t.Fatalf("demo PR: draft %v base %v head %s", p["draft"], p["base"], pr.HeadRef)
	}
	if !strings.HasPrefix(pr.Body, "No issue: ") || strings.Contains(pr.Body, "Closes") {
		t.Fatalf("demo PR body: %q", pr.Body)
	}
	if !strings.Contains(pr.Body, "SKILL.md") {
		t.Errorf("demo PR body lacks the diffstat: %q", pr.Body)
	}
	if got := gitT(t, origin, "show", "refs/heads/pins/core-skills-demo:"+demoSkill); !strings.Contains(got, "upstream-commit: "+head) {
		t.Fatalf("pushed branch does not raise the pin:\n%s", got)
	}

	clash, kind, target := only(t, f, clashFile)
	if clash.IsPR || kind != KindConflict || target != head || !strings.Contains(clash.Body, clashFile) {
		t.Fatalf("clash: %+v kind %s target %s, want a conflict issue naming the file", clash, kind, target)
	}
	gone, kind, _ := only(t, f, goneFile)
	if gone.IsPR || kind != KindError || !strings.Contains(gone.Body, "no longer exists upstream") {
		t.Fatalf("gone: %+v kind %s, want an error issue with the reason", gone, kind)
	}
	tool, kind, target := only(t, f, "mytool")
	if tool.IsPR || kind != KindBehind || target != "1.1.0" || !strings.Contains(tool.Body, "v1.1.0") {
		t.Fatalf("mytool: %+v kind %s target %s, want a behind issue for 1.1.0", tool, kind, target)
	}
	if branches := gitT(t, origin, "for-each-ref", "--format=%(refname)", "refs/heads/pins/"); branches != "refs/heads/pins/core-skills-demo" {
		t.Fatalf("pins branches on origin: %q, want only the demo branch", branches)
	}
	// An Update replaces only the body, so a title must hold nothing that a newer target changes.
	for _, it := range []forge.Item{pr, clash, tool} {
		title := issueTitle(t, srv, it.Number)
		if strings.Contains(title, head[:7]) || strings.Contains(title, "1.1.0") {
			t.Errorf("title %q of #%d names the target", title, it.Number)
		}
	}
	if !strings.Contains(log.String(), "core/skills/demo") {
		t.Errorf("log names no pin: %q", log.String())
	}

	// Without the branch, a second run that pushed anyway would bring it back: an open PR for
	// the same target asks for nothing, so nothing is pushed.
	gitT(t, origin, "update-ref", "-d", "refs/heads/pins/core-skills-demo")
	posts := len(srv.Posts)
	if err := Run(context.Background(), env); err == nil {
		t.Fatalf("second run: no error for the vanished pin")
	}
	for _, key := range []string{"core/skills/demo", clashFile, goneFile, "mytool"} {
		if n := len(items(t, f, key)); n != 1 {
			t.Errorf("after two runs pin %s has %d items, want 1", key, n)
		}
	}
	if len(srv.Posts) != posts {
		t.Errorf("second run posted %d times: %+v", len(srv.Posts)-posts, srv.Posts[posts:])
	}
	for _, it := range []forge.Item{clash, tool} { // same kind, same target: not even a body update
		if n := srv.Count(fmt.Sprintf("/repos/o/r/issues/%d", it.Number)); n != 0 {
			t.Errorf("second run touched issue #%d %d times", it.Number, n)
		}
	}
	if n := f.merges.Load(); n != 0 {
		t.Fatalf("the lane merged %d times", n)
	}
	if b := gitT(t, origin, "for-each-ref", "refs/heads/pins/"); b != "" {
		t.Errorf("second run pushed although nothing changed: %s", b)
	}
	if s := gitT(t, root, "status", "--porcelain"); s != "" {
		t.Errorf("the run changed the checkout:\n%s", s)
	}
}

// Fails when a pin whose result reaches .github/workflows/ (through init --upgrade) becomes a
// PR or pushes a branch instead of becoming a conflict issue, or stops the other pins.
func TestRun_workflowsTouchingMergeBecomesIssue(t *testing.T) {
	r := newLaneRepo(t)
	_, f := newFake(t)
	if err := Run(context.Background(), runEnv(r.root, r.up, f, releaseStub{})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	ci, kind, target := only(t, f, ciFile)
	if ci.IsPR || kind != KindConflict || target != r.head {
		t.Fatalf("ci: %+v kind %s, want a conflict issue", ci, kind)
	}
	if !strings.Contains(ci.Body, ".github/workflows/") {
		t.Errorf("ci issue does not say why: %q", ci.Body)
	}
	for _, key := range []string{"core/skills/demo", toolFile} {
		if it, kind, _ := only(t, f, key); !it.IsPR || kind != KindPR {
			t.Errorf("%s: %+v kind %s, want a PR", key, it, kind)
		}
	}
	if got := r.pinBranches(t); !slices.Equal(got, []string{"refs/heads/pins/core-skills-demo", "refs/heads/pins/pack-core-lib-tool.py"}) {
		t.Fatalf("pins branches %v: the workflows pin pushed, or another did not", got)
	}
}

// Fails when one failing pin stops the pins after it, or when the run does not end in an error
// that names the failed pin (and only that one).
func TestRun_exitOneButContinuesAfterPinFailure(t *testing.T) {
	r := newLaneRepo(t)
	srv, f := newFake(t)
	srv.FailFirst = map[string]int{"POST /repos/o/r/pulls": 502} // the first pin's PR: core/skills/demo
	err := Run(context.Background(), runEnv(r.root, r.up, f, releaseStub{}))
	if err == nil || !strings.Contains(err.Error(), "core/skills/demo") {
		t.Fatalf("Run: %v, want an error naming core/skills/demo", err)
	}
	if strings.Contains(err.Error(), toolFile) || strings.Contains(err.Error(), ciFile) {
		t.Fatalf("error names a pin that did not fail: %v", err)
	}
	if got := items(t, f, "core/skills/demo"); len(got) != 0 {
		t.Fatalf("demo: %+v, want nothing after the failed create", got)
	}
	if it, kind, _ := only(t, f, toolFile); !it.IsPR || kind != KindPR {
		t.Fatalf("tool.py after the failed pin: %+v kind %s, want a PR", it, kind)
	}
	if _, kind, _ := only(t, f, ciFile); kind != KindConflict {
		t.Fatalf("ci.yml after the failed pin: kind %s, want a conflict issue", kind)
	}

	// The next run finishes the failed pin without duplicating the others.
	if err := Run(context.Background(), runEnv(r.root, r.up, f, releaseStub{})); err != nil {
		t.Fatalf("second run: %v", err)
	}
	for _, key := range []string{"core/skills/demo", toolFile, ciFile} {
		only(t, f, key)
	}
}

// Fails when a pin that is current again does not get its open error issue closed by the lane.
func TestRun_measurableAgainClosesErrorIssue(t *testing.T) {
	r := newLaneRepo(t)
	_, f := newFake(t)
	toml, err := os.ReadFile(filepath.Join(r.root, "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeT(t, r.root, "lighthouse.toml", string(toml)+"\n"+toolToml)
	gitT(t, r.root, "commit", "-qam", "tool pin")
	gitT(t, r.root, "push", "-q", "origin", "main")

	broken := runEnv(r.root, r.up, f, releaseStub{}) // no release of o/mytool: not measurable
	if err := Run(context.Background(), broken); err == nil || !strings.Contains(err.Error(), "mytool") {
		t.Fatalf("Run without releases: %v, want an error for mytool", err)
	}
	if _, kind, _ := only(t, f, "mytool"); kind != KindError {
		t.Fatalf("mytool kind %s, want error", kind)
	}
	current := runEnv(r.root, r.up, f, releaseStub{"o/mytool": {{Tag: "v1.0.0"}}})
	if err := Run(context.Background(), current); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if it, kind, _ := only(t, f, "mytool"); kind != KindError || it.State != "closed" || !strings.Contains(it.Body, LaneClosedMarker) {
		t.Fatalf("mytool %+v, want the error issue closed by the lane", it)
	}
}

// Fails when a pin whose branch cannot be pushed (a deterministic failure that would recur
// every week) ends only in a red run instead of an error issue, when the run then exits 0, or
// when the pins after it stop.
func TestRun_pushFailureBecomesErrorIssue(t *testing.T) {
	r := newLaneRepo(t)
	// A branch named "pins" on origin makes every push to refs/heads/pins/<key> fail.
	gitT(t, r.origin, "update-ref", "refs/heads/pins", gitT(t, r.origin, "rev-parse", "main"))
	_, f := newFake(t)
	err := Run(context.Background(), runEnv(r.root, r.up, f, releaseStub{}))
	if err == nil || !strings.Contains(err.Error(), "core/skills/demo") {
		t.Fatalf("Run: %v, want an error naming core/skills/demo", err)
	}
	for _, key := range []string{"core/skills/demo", toolFile} {
		if it, kind, _ := only(t, f, key); it.IsPR || kind != KindError || it.State != "open" {
			t.Errorf("%s: %+v kind %s, want an open error issue", key, it, kind)
		}
	}
	if _, kind, _ := only(t, f, ciFile); kind != KindConflict {
		t.Fatalf("ci.yml after the failed pins: kind %s, want a conflict issue", kind)
	}
}

// forge adds an issue (or, with pr, a pull request) by author to the fake forge, as someone
// other than the lane would open it, and returns its number.
func forgeItem(srv *forgetest.Server, author, state, body string, pr bool) int {
	n := 100 + len(srv.Issues)
	it := map[string]any{"number": n, "node_id": fmt.Sprintf("I_%d", n), "title": "forged", "body": body,
		"state": state, "html_url": fmt.Sprintf("https://github.com/o/r/issues/%d", n),
		"user": map[string]any{"login": author}, "labels": []any{}}
	if pr {
		it["pull_request"] = map[string]any{"url": fmt.Sprintf("https://github.com/o/r/pull/%d", n)}
		srv.Pulls = append(srv.Pulls, map[string]any{"number": n, "state": state, "body": body,
			"head": map[string]any{"ref": "evil", "sha": "x"}, "user": map[string]any{"login": author}})
	}
	if srv.Issues == nil {
		srv.Issues = map[int]map[string]any{}
	}
	srv.Issues[n] = it
	return n
}

// Fails when an item with a pin marker that someone other than the lane's bot opened counts as
// a lane item: an open forged issue would be updated or closed by the lane instead of the lane
// writing its own PR, and a forged item closed "by a human" would silence a release.
func TestRun_ignoresMarkersOfOtherAuthors(t *testing.T) {
	r := newLaneRepo(t)
	toml, err := os.ReadFile(filepath.Join(r.root, "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeT(t, r.root, "lighthouse.toml", string(toml)+"\n"+toolToml)
	gitT(t, r.root, "commit", "-qam", "tool pin")
	gitT(t, r.root, "push", "-q", "origin", "main")

	srv, f := newFake(t)
	open := forgeItem(srv, "mallory", "open", RenderErrorBody(Marker("core/skills/demo", KindConflict, "x"), "core/skills/demo", "forged"), false)
	closed := forgeItem(srv, "mallory", "closed", "forged\n"+Marker("mytool", KindBehind, "1.1.0"), true)
	before := map[int]string{open: srv.Issues[open]["body"].(string), closed: srv.Issues[closed]["body"].(string)}

	if err := Run(context.Background(), runEnv(r.root, r.up, f, toolReleases)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if srv.Issues[open]["state"] != "open" || srv.Issues[closed]["state"] != "closed" {
		t.Errorf("forged items changed state: #%d %v, #%d %v", open, srv.Issues[open]["state"], closed, srv.Issues[closed]["state"])
	}
	for n, body := range before {
		if srv.Issues[n]["body"] != body {
			t.Errorf("the lane rewrote the body of forged item #%d: %q", n, srv.Issues[n]["body"])
		}
	}
	for _, p := range srv.Posts {
		if strings.Contains(p.Path, fmt.Sprintf("/%d/", open)) || strings.Contains(p.Path, fmt.Sprintf("/%d/", closed)) {
			t.Errorf("the lane posted to a forged item: %s", p.Path)
		}
	}
	var demoPR, toolIssue bool
	for _, it := range items(t, f, "core/skills/demo") {
		demoPR = demoPR || (it.IsPR && it.Author == laneBot && it.State == "open")
	}
	for _, it := range items(t, f, "mytool") {
		_, kind, target, _ := ParseMarker(it.Body)
		toolIssue = toolIssue || (it.Author == laneBot && kind == KindBehind && target == "1.1.0" && it.State == "open")
	}
	if !demoPR {
		t.Errorf("demo: no draft PR by the lane next to the forged issue: %+v", items(t, f, "core/skills/demo"))
	}
	if !toolIssue {
		t.Errorf("mytool: the forged closed item silenced the release: %+v", items(t, f, "mytool"))
	}
}

// Fails when the lane targets the upstream HEAD instead of the newest commit that touched the
// path: an unrelated upstream commit would then update (and force-push) the open PR, recreate a
// PR a human closed, or raise the pin to a commit that measures anything but current; and when a
// commit on the path does not give a new target.
func TestRun_targetIsNewestCommitOnThePath(t *testing.T) {
	r := newLaneRepo(t)
	srv, f := newFake(t)
	env := runEnv(r.root, r.up, f, releaseStub{})
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("Run: %v", err)
	}
	pr, _, target := only(t, f, toolFile)
	if target != r.head {
		t.Fatalf("target %s, want %s", target, r.head)
	}
	branch := "refs/heads/" + BranchName(toolFile)
	pushed := gitT(t, r.origin, "rev-parse", branch)

	r.up.commit(map[string]string{"unrelated.txt": "x\n"})
	posts := len(srv.Posts)
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if it, _, got := only(t, f, toolFile); got != r.head || it.Body != pr.Body {
		t.Errorf("after an unrelated upstream commit: target %s, body changed %v; want %s, unchanged", got, it.Body != pr.Body, r.head)
	}
	if got := gitT(t, r.origin, "rev-parse", branch); got != pushed {
		t.Errorf("an unrelated upstream commit force-pushed %s: %s -> %s", branch, pushed, got)
	}
	if len(srv.Posts) != posts {
		t.Errorf("an unrelated upstream commit posted: %+v", srv.Posts[posts:])
	}
	// The pin raised to the target measures current although HEAD moved on.
	raised := r.toolPin()
	raised.Commit = target
	if res := pins.MeasureSource(context.Background(), env.Git, raised); res.Status != pins.StatusCurrent {
		t.Errorf("pin raised to %s measures %s (%s), want current", target, res.Status, res.Detail)
	}

	// A human closes the PR; another unrelated commit brings it back neither.
	if err := f.ClosePR(context.Background(), pr.Number, "niet nu"); err != nil {
		t.Fatal(err)
	}
	r.up.commit(map[string]string{"unrelated.txt": "y\n"})
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("third Run: %v", err)
	}
	if got := items(t, f, toolFile); len(got) != 1 || got[0].State != "closed" {
		t.Fatalf("after a human close and an unrelated commit: %+v, want only the closed PR", got)
	}

	// A commit on the path is a new target: a new PR.
	next := r.up.commit(map[string]string{"lib/tool.py": "x = 11\ny = 2\nz = 3\nv = 4\nw = 5\n"})
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("fourth Run: %v", err)
	}
	var open []string
	for _, it := range items(t, f, toolFile) {
		if it.State == "open" {
			_, _, tg, _ := ParseMarker(it.Body)
			open = append(open, tg)
		}
	}
	if len(open) != 1 || open[0] != next {
		t.Fatalf("after a commit on the path: open targets %v, want [%s]", open, next)
	}
}

// Fails when the lane force-pushes over commits a person added to the branch of its open PR
// (the steps in the PR body invite them), rewrites that PR's body, or ends the run red for it;
// and, as the positive control, when the lane stops updating a branch that holds only its own
// commit.
func TestRun_leavesBranchWithHumanCommitsAlone(t *testing.T) {
	r := newLaneRepo(t)
	srv, f := newFake(t)
	env := runEnv(r.root, r.up, f, releaseStub{})
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("Run: %v", err)
	}
	demo, _, _ := only(t, f, "core/skills/demo")
	tool, _, _ := only(t, f, toolFile)

	// A person adds a commit to the tool pin's branch.
	toolBranch := BranchName(toolFile)
	work := t.TempDir()
	gitT(t, work, "clone", "-q", "--branch", toolBranch, r.origin, ".")
	writeT(t, work, toolFile, "x = 10\ny = 2\nz = 3\nv = 4\nw = 5 local, reviewed\n")
	gitT(t, work, "commit", "-qam", "review fix")
	gitT(t, work, "push", "-q", "origin", "HEAD:refs/heads/"+toolBranch)
	human := gitT(t, r.origin, "rev-parse", "refs/heads/"+toolBranch)
	demoTip := gitT(t, r.origin, "rev-parse", "refs/heads/"+BranchName("core/skills/demo"))

	// Upstream moves both paths: both pins get a new target.
	r.up.commit(map[string]string{
		"lib/tool.py":        "x = 11\ny = 2\nz = 3\nv = 4\nw = 5\n",
		"skills/demo/ref.md": "a\nb\nc upstream again\n",
	})
	var log bytes.Buffer
	env.Log = &log
	if err := Run(context.Background(), env); err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if got := gitT(t, r.origin, "rev-parse", "refs/heads/"+toolBranch); got != human {
		t.Errorf("the lane overwrote the human commit on %s: %s -> %s", toolBranch, human, got)
	}
	if it := srv.Issues[tool.Number]; it["body"] != tool.Body || it["state"] != "open" {
		t.Errorf("tool PR #%d changed although its branch was left alone: %v", tool.Number, it)
	}
	if !strings.Contains(log.String(), toolFile+": pr, #") || !strings.Contains(log.String(), "left alone") {
		t.Errorf("log does not say the tool PR was left alone: %q", log.String())
	}
	if got := gitT(t, r.origin, "rev-parse", "refs/heads/"+BranchName("core/skills/demo")); got == demoTip {
		t.Errorf("the demo branch, with only the lane's commit, was not updated to the new target")
	}
	if it := srv.Issues[demo.Number]; it["body"] == demo.Body {
		t.Errorf("demo PR #%d body not updated to the new target", demo.Number)
	}
}
