package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/runs"
	"github.com/CorneeldH/lighthouse/internal/store"
)

var update = flag.Bool("update", false, "rewrite internal/dispatch/testdata/snapshot.json from TestGatherCommandMatchesFixture")

// snapshotFixture is the snapshot decide is tested on; TestGatherCommandMatchesFixture writes it.
var snapshotFixture = filepath.Join("..", "dispatch", "testdata", "snapshot.json")

const fixtureToken = "ghp-fixture-secret"

// gatherForge serves a board with a question answered by a trusted author (issue 7), an On
// hold item with a closed predecessor (issue 8) and an item with an open run on a branch
// (issue 9).
func gatherForge(t *testing.T) *forgetest.Server {
	t.Helper()
	srv := forgetest.New(t)
	srv.Login = "lh-bot"
	srv.Roles = map[string]string{"owner": "admin", "stranger": "read"}
	check := func(state string) map[string]any { return map[string]any{"state": state} }
	pr := func(number int, state, sha, checks string) map[string]any {
		return map[string]any{"number": number, "state": state, "isDraft": false, "mergeable": "MERGEABLE",
			"headRefOid": sha, "headRefName": "issue-9-y", "statusCheckRollup": check(checks)}
	}
	p1 := map[string]any{"optionId": "p1", "field": map[string]any{"id": "PF"}}
	seven := boardNode("I7", "c1", issueContent(7, "Kies een richting", nil))
	seven["fieldValues"].(map[string]any)["nodes"] = append(seven["fieldValues"].(map[string]any)["nodes"].([]any), p1)
	srv.BoardNodes = []map[string]any{
		seven,
		boardNode("I8", "h1", issueContent(8, "Wacht op 5", map[string]any{
			"blockedBy": map[string]any{"nodes": []any{map[string]any{
				"number": 5, "state": "CLOSED", "repository": map[string]any{"nameWithOwner": "o/r"},
				"closedByPullRequestsReferences": map[string]any{"nodes": []any{map[string]any{"merged": true}}},
			}}},
		})),
		boardNode("I9", "c1", issueContent(9, "Bouw het", map[string]any{
			"closedByPullRequestsReferences": map[string]any{"nodes": []any{pr(30, "OPEN", "sha-new", "SUCCESS")}},
		})),
	}
	// an earlier PR of the branch, closed with red checks, joins the closing PR
	srv.Branches = map[string][]map[string]any{"issue-9-y": {
		pr(30, "OPEN", "sha-new", "SUCCESS"),
		pr(29, "CLOSED", "sha-old", "FAILURE"),
	}}
	comment := func(id int64, issue int, login, body string) map[string]any {
		return map[string]any{"id": id, "body": body, "created_at": "2026-10-30T11:00:00Z",
			"html_url": "https://github.com/o/r/issues/" + strconv.Itoa(issue) + "#issuecomment-" + strconv.FormatInt(id, 10),
			"user":     map[string]any{"login": login}}
	}
	srv.Comments = map[int][]map[string]any{
		7: {
			comment(71, 7, "owner", "🤖 Vraag\n\nWelke kant op?"),
			comment(72, 7, "owner", "Links."),
			comment(73, 7, "stranger", "Rechts, zeker weten."),
		},
		9: {comment(91, 9, "owner", "🤖 Handoff\nnext: /lighthouse-start-issue 9")},
	}
	srv.Rockets = map[int64][]string{91: {"lh-bot"}}
	return srv
}

func TestGatherCommandMatchesFixture(t *testing.T) {
	dir := repoDir(t, completeRepo+"\n[forge.board.priority_options]\n\"P1\" = \"p1\"\n")
	env, db := tickFixture(t)
	vars := map[string]string{"GH_TOKEN": fixtureToken}
	env.Getenv = func(k string) string { return vars[k] }
	env.ForgeURL = gatherForge(t).URL()
	env.Now = func() time.Time { return time.Date(2026, 10, 31, 13, 0, 0, 0, time.FixedZone("CET", 3600)) }

	ctx := context.Background()
	st, err := store.OpenShared(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	run, err := runs.Take(ctx, st.DB, runs.Claim{Repo: "o/r", Issue: 9, SessionID: "0b9a3c1e-0000-4000-8000-000000000009",
		Mode: "start", Trigger: "handoff", TriggerKey: "start:9", Command: "/lighthouse-start-issue 9",
		Profile: "software", Now: time.Date(2026, 10, 31, 10, 30, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE runs SET branch = ? WHERE id = ?`, "issue-9-y", run.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := gather(ctx, env, dir, db, &out, &errOut); err != nil {
		t.Fatalf("gather = %v\n%s", err, errOut.String())
	}
	if *update {
		if err := os.WriteFile(snapshotFixture, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(snapshotFixture)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("gather output differs from %s (rerun with -update after checking the change):\n%s", snapshotFixture, out.String())
	}
	if strings.Contains(out.String(), fixtureToken) {
		t.Error("gather output contains the token")
	}
}

func TestGatherWithoutToken(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, db := tickFixture(t)
	env.Getenv = func(string) string { return "" }
	var out bytes.Buffer
	err := gather(context.Background(), env, dir, db, &out, &bytes.Buffer{})
	if err == nil || err.Error() != "gather: no GitHub token: set GH_TOKEN or GITHUB_TOKEN" {
		t.Fatalf("err = %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("out = %q, want nothing", out.String())
	}
}

func TestGatherTrustsAnswerPermissions(t *testing.T) {
	dir := repoDir(t, completeRepo)
	env, db := tickFixture(t)
	if err := os.WriteFile(env.HostToml, []byte("[dispatch]\nanswer_permissions = [\"read\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env.ForgeURL = gatherForge(t).URL()
	var out, errOut bytes.Buffer
	if err := gather(context.Background(), env, dir, db, &out, &errOut); err != nil {
		t.Fatalf("gather = %v\n%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), `"body": "Rechts, zeker weten."`) {
		t.Errorf("the comment of a read collaborator is missing with answer_permissions = [\"read\"]:\n%s", out.String())
	}
	if strings.Contains(out.String(), `"body": "Links."`) {
		t.Errorf("the comment of an admin is kept with answer_permissions = [\"read\"]:\n%s", out.String())
	}
}
