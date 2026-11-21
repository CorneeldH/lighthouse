package forge

import (
	"context"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
)

// projectBoard is a board whose IDs the fake serves: SF is in forgetest.DefaultFieldIDs.
func projectBoard() *repoconfig.Board {
	return &repoconfig.Board{ProjectNumber: 1, ProjectID: "PVT_1", StatusFieldID: "SF", PriorityFieldID: "PF"}
}

func projectServer(t *testing.T) (*forgetest.Server, *GitHub) {
	t.Helper()
	srv, c := boardClient(t)
	srv.ProjectID = "PVT_1"
	return srv, c
}

func TestProjectUserThenOrg(t *testing.T) {
	srv, c := projectServer(t)
	srv.ProjectAtOrg = true
	p, err := c.project(context.Background(), projectBoard())
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL() + "/orgs/o/projectsV2/1"; p.base != want {
		t.Fatalf("base = %q, want %q", p.base, want)
	}
	if srv.Count("/users/o/projectsV2/1") != 1 || srv.Count("/orgs/o/projectsV2/1") != 1 {
		t.Fatalf("users %d, orgs %d; want 1 and 1", srv.Count("/users/o/projectsV2/1"), srv.Count("/orgs/o/projectsV2/1"))
	}
}

func TestProjectUserPathFirst(t *testing.T) {
	srv, c := projectServer(t)
	p, err := c.project(context.Background(), projectBoard())
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL() + "/users/o/projectsV2/1"; p.base != want {
		t.Fatalf("base = %q, want %q", p.base, want)
	}
	if srv.Count("/orgs/o/projectsV2/1") != 0 {
		t.Fatalf("orgs path asked %d times; want 0 when the user path answers", srv.Count("/orgs/o/projectsV2/1"))
	}
}

func TestProjectNotFoundNamesOwnerKey(t *testing.T) {
	srv, c := projectServer(t)
	srv.Fail = map[string]int{"GET /users/o/projectsV2/1": 404, "GET /orgs/o/projectsV2/1": 404}
	_, err := c.project(context.Background(), projectBoard())
	if err == nil || !strings.Contains(err.Error(), "project_owner") {
		t.Fatalf("err = %v, want one naming project_owner", err)
	}
}

func TestProjectOtherErrorIsNotNotFound(t *testing.T) {
	srv, c := projectServer(t)
	srv.Fail = map[string]int{"GET /users/o/projectsV2/1": 500}
	_, err := c.project(context.Background(), projectBoard())
	if err == nil || strings.Contains(err.Error(), "project_owner") {
		t.Fatalf("err = %v, want the 500 itself", err)
	}
	if srv.Count("/orgs/o/projectsV2/1") != 0 {
		t.Fatal("a 500 on the user path must not fall through to the org path")
	}
}

func TestProjectOwnerOverridesRepoOwner(t *testing.T) {
	srv, c := projectServer(t)
	b := projectBoard()
	b.ProjectOwner = "acme"
	if _, err := c.project(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if srv.Count("/users/acme/projectsV2/1") != 1 || srv.Count("/users/o/projectsV2/1") != 0 {
		t.Fatalf("acme %d, o %d; want 1 and 0", srv.Count("/users/acme/projectsV2/1"), srv.Count("/users/o/projectsV2/1"))
	}
}

func TestProjectNodeIDMismatch(t *testing.T) {
	srv, c := projectServer(t)
	srv.ProjectID = "PVT_other"
	_, err := c.project(context.Background(), projectBoard())
	if err == nil || !strings.Contains(err.Error(), "PVT_other") || !strings.Contains(err.Error(), "PVT_1") {
		t.Fatalf("err = %v, want one naming PVT_other and PVT_1", err)
	}
}

func TestProjectFieldIDs(t *testing.T) {
	srv, c := projectServer(t)
	srv.FieldIDs = []string{"PF", "SF"}
	p, err := c.project(context.Background(), projectBoard())
	if err != nil {
		t.Fatal(err)
	}
	// Sorted node IDs: PF is 100, SF is 101; Linked pull requests is 99.
	if p.status != 101 || p.priority != 100 || p.linkedPRs != 99 {
		t.Fatalf("status %d, priority %d, linkedPRs %d; want 101, 100, 99", p.status, p.priority, p.linkedPRs)
	}
	if want := p.base + "/items?per_page=100&fields=101,100,99"; p.itemsURL() != want {
		t.Fatalf("itemsURL = %q, want %q", p.itemsURL(), want)
	}
}

func TestProjectWithoutPriorityField(t *testing.T) {
	srv, c := projectServer(t)
	srv.FieldIDs = []string{"SF"}
	b := projectBoard()
	b.PriorityFieldID = ""
	p, err := c.project(context.Background(), b)
	if err != nil {
		t.Fatal(err)
	}
	if p.priority != 0 {
		t.Fatalf("priority = %d, want 0", p.priority)
	}
	if want := p.base + "/items?per_page=100&fields=100,99"; p.itemsURL() != want {
		t.Fatalf("itemsURL = %q, want %q", p.itemsURL(), want)
	}
}

func TestProjectUnknownField(t *testing.T) {
	for _, tc := range []struct{ name, key string }{{"status", "status_field_id"}, {"priority", "priority_field_id"}} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := projectServer(t)
			b := projectBoard()
			if tc.name == "status" {
				srv.FieldIDs = []string{"PF"}
			} else {
				srv.FieldIDs = []string{"SF"}
			}
			_, err := c.project(context.Background(), b)
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("err = %v, want one naming %s", err, tc.key)
			}
		})
	}
}

func TestProjectWithoutNumber(t *testing.T) {
	srv, c := projectServer(t)
	b := projectBoard()
	b.ProjectNumber = 0
	_, err := c.project(context.Background(), b)
	if err == nil || !strings.Contains(err.Error(), "project_number") {
		t.Fatalf("err = %v, want one naming project_number", err)
	}
	if srv.Count("/users/o/projectsV2/0") != 0 {
		t.Fatal("no request expected without a project number")
	}
}

func TestProjectResolvedOnce(t *testing.T) {
	srv, c := projectServer(t)
	for range 2 {
		if _, err := c.project(context.Background(), projectBoard()); err != nil {
			t.Fatal(err)
		}
	}
	if srv.Count("/users/o/projectsV2/1") != 1 || srv.Count("/users/o/projectsV2/1/fields") != 1 {
		t.Fatalf("project %d, fields %d; want 1 and 1", srv.Count("/users/o/projectsV2/1"), srv.Count("/users/o/projectsV2/1/fields"))
	}
}

func TestProjectErrorIsNotKept(t *testing.T) {
	srv, c := projectServer(t)
	srv.FailFirst = map[string]int{"GET /users/o/projectsV2/1": 500}
	if _, err := c.project(context.Background(), projectBoard()); err == nil {
		t.Fatal("want the first call to fail")
	}
	if _, err := c.project(context.Background(), projectBoard()); err != nil {
		t.Fatalf("second call: %v; an error must not be stored", err)
	}
}
