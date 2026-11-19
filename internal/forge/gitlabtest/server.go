// Package gitlabtest is a fake GitLab for tests of the GitLab forge client and its callers. It
// answers the REST routes under /api/v4 from seeded data and fails the test on any request it
// does not know, like forgetest does for GitHub. It does not import forge.
package gitlabtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// reporter is the part of *testing.T the server uses; tests of the server pass their own.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
	Cleanup(func())
}

const apiPrefix = "/api/v4"

// Server is a fake GitLab. Set Login and Project before the first request; seed through the
// Add and Set methods, which are safe to call while requests run.
type Server struct {
	// Login is the username GET /user answers for the token, and the author of what is POSTed.
	Login string
	// Project is the project path ("group/sub/project") the server knows. The default is
	// "group/project". A request for another project is unexpected.
	Project string
	// Free makes the server a GitLab Free plan: a blocking link is refused with 403 on write and
	// left out of the links on read.
	Free bool

	t      reporter
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []string
	writes []Write
	nextID int64

	issues map[int]map[string]any
	notes  map[int][]map[string]any
	awards map[int64][]map[string]any
	links  map[int][]map[string]any
	// failLinksPage2 holds the iids whose links list answers a 500 on page two.
	failLinksPage2 map[int]bool
	// failMRsPage2 makes the merge request list answer a 500 on page two.
	failMRsPage2 bool
	// failLinks holds the iids whose links list answers a 403 on every page.
	failLinks   map[int]bool
	mrs         []map[string]any
	closedBy    map[string][]map[string]any // "project#iid" to the merge requests that close it
	related     map[int][]map[string]any    // iid to its related merge requests
	stateEvents map[int][]map[string]any    // iid to its resource state events, oldest first
	// failIssueMRsPage2 holds the iids whose closed_by and related_merge_requests answer a 500
	// on page two.
	failIssueMRsPage2 map[int]bool
	failNotes         map[int]bool     // iids whose note creation answers a 500
	users             map[string]int64 // username to user ID
	members           map[string]int   // username to access level
	statuses          []string
	status            map[int]string

	milestones []map[string]any
	labels     []string
	mrNotes    map[int][]map[string]any // notes of a merge request; its iid is not an issue iid
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server { return newServer(t) }

func newServer(t reporter) *Server {
	t.Helper()
	s := &Server{
		t: t, Project: "group/project", nextID: 1000,
		issues: map[int]map[string]any{}, notes: map[int][]map[string]any{}, mrNotes: map[int][]map[string]any{},
		awards: map[int64][]map[string]any{}, links: map[int][]map[string]any{}, closedBy: map[string][]map[string]any{},
		related: map[int][]map[string]any{}, stateEvents: map[int][]map[string]any{}, failIssueMRsPage2: map[int]bool{},
		users: map[string]int64{}, members: map[string]int{}, status: map[int]string{},
	}
	mux := http.NewServeMux()
	// Register routes here; later tasks add theirs next to these.
	p := "/projects/{project}"
	s.route(mux, "GET /user", s.user)
	s.route(mux, "GET /users", s.userByName)
	s.route(mux, "GET "+p+"/members/all/{uid}", s.project(s.member))
	s.route(mux, "GET "+p+"/issues", s.project(s.listIssues))
	s.route(mux, "GET "+p+"/issues/{iid}", s.project(s.issue))
	s.route(mux, "GET "+p+"/issues_statistics", s.project(s.issuesStatistics))
	s.route(mux, "GET "+p+"/issues/{iid}/notes", s.project(s.listNotes))
	s.route(mux, "POST "+p+"/issues/{iid}/notes", s.project(s.createNote))
	s.route(mux, "GET "+p+"/issues/{iid}/notes/{nid}/award_emoji", s.project(s.listAwards))
	s.route(mux, "POST "+p+"/issues/{iid}/notes/{nid}/award_emoji", s.project(s.createAward))
	s.route(mux, "GET "+p+"/issues/{iid}/links", s.project(s.listLinks))
	// closed_by answers for any project: a blocker can live in another one.
	s.route(mux, "GET "+p+"/issues/{iid}/closed_by", s.listClosedBy)
	s.route(mux, "GET "+p+"/issues/{iid}/related_merge_requests", s.project(s.listRelatedMRs))
	s.route(mux, "GET "+p+"/issues/{iid}/resource_state_events", s.project(s.listStateEvents))
	s.route(mux, "GET "+p+"/merge_requests", s.project(s.listMRs))
	s.route(mux, "GET "+p+"/merge_requests/{iid}", s.project(s.getMR))
	s.route(mux, "PUT "+p+"/merge_requests/{iid}", s.project(s.updateMR))
	s.route(mux, "GET "+p+"/merge_requests/{iid}/notes", s.project(s.listMRNotes))
	s.route(mux, "PUT "+p+"/merge_requests/{iid}/merge", s.project(s.mergeMR))
	s.writeRoutes(mux, p)
	mux.HandleFunc("POST /api/graphql", s.graphql)
	mux.HandleFunc("/", s.unexpected)
	s.srv = httptest.NewServer(s.record(mux))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) route(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	method, path, _ := strings.Cut(pattern, " ")
	mux.HandleFunc(method+" "+apiPrefix+path, h)
}

// URL is the API base URL to give the client; it ends in /api/v4.
func (s *Server) URL() string { return s.srv.URL + apiPrefix }

// Requests returns every request received, as "METHOD /path?query", in order.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.reqs)
}

// record logs the request and refuses one without a PRIVATE-TOKEN.
func (s *Server) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		s.mu.Lock()
		s.reqs = append(s.reqs, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			s.writes = append(s.writes, Write{Method: r.Method, Path: r.URL.EscapedPath(), Body: body})
		}
		s.mu.Unlock()
		if r.Header.Get("PRIVATE-TOKEN") == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "401 Unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) unexpected(w http.ResponseWriter, r *http.Request) {
	s.t.Errorf("gitlabtest: unexpected request %s %s", r.Method, r.URL)
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not Found"})
}

// project runs h only for the known project; any other is an unexpected request.
func (s *Server) project(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("project") != s.Project {
			s.unexpected(w, r)
			return
		}
		h(w, r)
	}
}

// userID returns a stable ID for a username, creating it on first use.
func (s *Server) userID(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userIDLocked(name)
}

func (s *Server) userIDLocked(name string) int64 {
	if id, ok := s.users[name]; ok {
		return id
	}
	id := int64(len(s.users) + 1)
	s.users[name] = id
	return id
}

func (s *Server) newID() int64 {
	s.nextID++
	return s.nextID
}

func (s *Server) user(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": s.userID(s.Login), "username": s.Login})
}

// userByName answers a one-element list for a user that is Login or a member, else an empty one.
func (s *Server) userByName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("username")
	s.mu.Lock()
	_, member := s.members[name]
	known := member || name == s.Login
	s.mu.Unlock()
	out := []any{}
	if known {
		out = append(out, map[string]any{"id": s.userID(name), "username": name})
	}
	writeJSON(w, http.StatusOK, out)
}

// member answers the membership of a user ID, or 404 as GitLab does for a non-member.
func (s *Server) member(w http.ResponseWriter, r *http.Request) {
	uid, _ := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, id := range s.users {
		if level, ok := s.members[name]; ok && id == uid {
			writeJSON(w, http.StatusOK, map[string]any{"id": id, "username": name, "access_level": level})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
}

// listIssues serves the issues by iid, filtered on state (opened, closed, all; default all),
// on milestone (the milestone title), on labels (a comma list, all must be on the issue), on
// search (a case-insensitive part of the title or description) and on updated_after (an RFC 3339 time; an issue updated
// earlier is left out).
func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state, milestone := q.Get("state"), q.Get("milestone")
	label, search := q.Get("labels"), strings.ToLower(q.Get("search"))
	after, _ := time.Parse(time.RFC3339, q.Get("updated_after"))
	s.mu.Lock()
	var out []any
	for _, iid := range sortedKeys(s.issues) {
		it := s.issues[iid]
		if st, _ := it["state"].(string); state != "" && state != "all" && state != st {
			continue
		}
		if milestone != "" {
			if m, _ := it["milestone"].(map[string]any); m == nil || m["title"] != milestone {
				continue
			}
		}
		if label != "" && !hasLabels(it, label) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(fmt.Sprint(it["title"], " ", it["description"])), search) {
			continue
		}
		if !after.IsZero() {
			if at, _ := time.Parse(time.RFC3339, fmt.Sprint(it["updated_at"])); at.Before(after) {
				continue
			}
		}
		out = append(out, it)
	}
	s.mu.Unlock()
	paginate(w, r, out)
}

// issuesStatistics serves GET …/issues_statistics for the milestone title in the milestone
// query parameter, counted over the seeded issues, as GitLab does: any closed issue is closed.
func (s *Server) issuesStatistics(w http.ResponseWriter, r *http.Request) {
	title := r.URL.Query().Get("milestone")
	s.mu.Lock()
	var opened, closed int
	for _, it := range s.issues {
		if m, _ := it["milestone"].(map[string]any); m == nil || m["title"] != title {
			continue
		}
		if it["state"] == "closed" {
			closed++
		} else {
			opened++
		}
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"statistics": map[string]any{
		"counts": map[string]any{"all": opened + closed, "opened": opened, "closed": closed},
	}})
}

func (s *Server) issue(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	it, ok := s.issues[iid]
	s.mu.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) listNotes(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	out := toAny(s.notes[iid])
	s.mu.Unlock()
	paginate(w, r, out)
}

func (s *Server) createNote(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	var body struct {
		Body string `json:"body"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	s.mu.Lock()
	failed := s.failNotes[iid]
	s.mu.Unlock()
	if failed {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "500 Internal Server Error"})
		return
	}
	id := s.AddNote(iid, s.Login, body.Body, false)
	s.mu.Lock()
	n := s.findNote(iid, id)
	s.mu.Unlock()
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) listAwards(w http.ResponseWriter, r *http.Request) {
	nid, _ := strconv.ParseInt(r.PathValue("nid"), 10, 64)
	s.mu.Lock()
	out := toAny(s.awards[nid])
	s.mu.Unlock()
	paginate(w, r, out)
}

func (s *Server) createAward(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	nid, _ := strconv.ParseInt(r.PathValue("nid"), 10, 64)
	// The name is a query parameter, as GitLab documents it.
	name := r.URL.Query().Get("name")
	s.mu.Lock()
	known := s.findNote(iid, nid) != nil
	dup := slices.ContainsFunc(s.awards[nid], func(a map[string]any) bool {
		u, _ := a["user"].(map[string]any)
		return a["name"] == name && u["username"] == s.Login
	})
	s.mu.Unlock()
	if !known || dup {
		// GitLab answers 404 for an award the user already gave.
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
		return
	}
	s.AddAward(iid, nid, name, s.Login)
	writeJSON(w, http.StatusCreated, map[string]any{"name": name, "user": map[string]any{"username": s.Login}})
}

func (s *Server) listClosedBy(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("project") + "#" + r.PathValue("iid")
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	fail := r.PathValue("project") == s.Project && s.failIssueMRsPage2[iid]
	out := toAny(s.closedBy[key])
	s.mu.Unlock()
	paginateOrFail(w, r, out, fail)
}

// listRelatedMRs serves the merge requests related to issue iid: those that close it and those
// that mention it, as AddRelatedMR seeded them.
func (s *Server) listRelatedMRs(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	fail := s.failIssueMRsPage2[iid]
	out := toAny(s.related[iid])
	s.mu.Unlock()
	paginateOrFail(w, r, out, fail)
}

// listStateEvents serves the resource state events of issue iid, oldest first.
func (s *Server) listStateEvents(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	out := toAny(s.stateEvents[iid])
	s.mu.Unlock()
	paginate(w, r, out)
}

// paginateOrFail is paginate, but answers a 500 for page two when failPage2 is set.
func paginateOrFail(w http.ResponseWriter, r *http.Request, items []any, failPage2 bool) {
	if failPage2 && r.URL.Query().Get("page") == "2" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "500 Internal Server Error"})
		return
	}
	paginate(w, r, items)
}

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	if s.failLinks[iid] {
		s.mu.Unlock()
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden"})
		return
	}
	if s.failLinksPage2[iid] && r.URL.Query().Get("page") == "2" {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"message": "500 Internal Server Error"})
		return
	}
	var links []map[string]any
	for _, l := range s.links[iid] {
		if !s.Free || l["link_type"] != "is_blocked_by" {
			links = append(links, l)
		}
	}
	if !s.Free {
		links = append(links, s.blocksLinksLocked(iid)...)
	}
	s.mu.Unlock()
	paginate(w, r, toAny(links))
}

// blocksLinksLocked are the links of type blocks that GitLab shows on issue iid for every
// is_blocked_by link of another issue that names iid of Project, in iid order. The link state is
// the state of the blocked issue. Call it with s.mu held.
func (s *Server) blocksLinksLocked(iid int) []map[string]any {
	var blocked []int
	for m, ls := range s.links {
		for _, l := range ls {
			refs, _ := l["references"].(map[string]any)
			if l["link_type"] == "is_blocked_by" && toInt(l["iid"]) == iid && fmt.Sprint(refs["full"]) == fmt.Sprintf("%s#%d", s.Project, iid) {
				blocked = append(blocked, m)
				break
			}
		}
	}
	slices.Sort(blocked)
	var out []map[string]any
	for _, m := range blocked {
		out = append(out, map[string]any{
			"iid": m, "state": s.issues[m]["state"], "link_type": "blocks",
			"references": map[string]any{"full": fmt.Sprintf("%s#%d", s.Project, m)},
		})
	}
	return out
}

// FailLinks makes every page of the links of issue iid answer a 403.
func (s *Server) FailLinks(iid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failLinks == nil {
		s.failLinks = map[int]bool{}
	}
	s.failLinks[iid] = true
}

// FailLinksPage2 makes the second page of the links of issue iid answer a 500.
func (s *Server) FailLinksPage2(iid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failLinksPage2 == nil {
		s.failLinksPage2 = map[int]bool{}
	}
	s.failLinksPage2[iid] = true
}

// listMRs serves the merge requests, filtered on state (opened, closed, merged; default all)
// and on source_branch. It leaves out head_pipeline, as GitLab's list does: only the single
// merge request (getMR) carries it. It encodes copies: mergeMR changes the seeded maps under mu.
func (s *Server) listMRs(w http.ResponseWriter, r *http.Request) {
	state, branch := r.URL.Query().Get("state"), r.URL.Query().Get("source_branch")
	s.mu.Lock()
	var out []any
	for _, mr := range s.mrs {
		if st, _ := mr["state"].(string); state != "" && state != "all" && state != st {
			continue
		}
		if b, _ := mr["source_branch"].(string); branch != "" && branch != b {
			continue
		}
		cp := maps.Clone(mr)
		delete(cp, "head_pipeline")
		out = append(out, cp)
	}
	failPage2 := s.failMRsPage2
	s.mu.Unlock()
	paginateOrFail(w, r, out, failPage2)
}

// FailMRsPage2 makes the second page of the merge request list answer a 500.
func (s *Server) FailMRsPage2() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failMRsPage2 = true
}

// getMR serves one merge request, head_pipeline included; an unknown iid answers 404.
func (s *Server) getMR(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	var found map[string]any
	for _, mr := range s.mrs {
		if toInt(mr["iid"]) == iid {
			found = maps.Clone(mr)
		}
	}
	s.mu.Unlock()
	if found == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
		return
	}
	writeJSON(w, http.StatusOK, found)
}

// mergeMR marks the merge request merged, as GitLab does for a squash merge: 409 when the sha
// is not the head, 405 for a merge request that is not open, 406 for one with conflicts. A
// merge without squash=true is an unexpected request: lighthouse only squash-merges. It is
// here because forge.Forge requires MergePR.
func (s *Server) mergeMR(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	var body struct {
		SHA    string `json:"sha"`
		Squash bool   `json:"squash"`
	}
	if !s.decode(w, r, &body) {
		return
	}
	if !body.Squash {
		s.unexpected(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mr := range s.mrs {
		if toInt(mr["iid"]) != iid {
			continue
		}
		if head, _ := mr["sha"].(string); body.SHA != "" && head != body.SHA {
			writeJSON(w, http.StatusConflict, map[string]any{"message": "SHA does not match HEAD of source branch: " + head})
			return
		}
		if st, _ := mr["state"].(string); st != "opened" {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "405 Method Not Allowed"})
			return
		}
		if c, _ := mr["has_conflicts"].(bool); c {
			writeJSON(w, http.StatusNotAcceptable, map[string]any{"message": "Branch cannot be merged"})
			return
		}
		mr["state"] = "merged"
		writeJSON(w, http.StatusOK, mr)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
}

// decode reads a JSON object body; a body that is none fails the test and answers 400.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, into any) bool {
	raw, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(raw, into); err != nil {
		s.t.Errorf("gitlabtest: %s %s: body is no JSON object: %v", r.Method, r.URL.Path, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request"})
		return false
	}
	return true
}

// findNote returns the note with the ID on the issue; the caller holds mu.
func (s *Server) findNote(iid int, id int64) map[string]any {
	for _, n := range s.notes[iid] {
		if n["id"] == id {
			return n
		}
	}
	return nil
}

// AddIssue seeds an issue in GitLab's REST shape; m["iid"] is its number.
func (s *Server) AddIssue(m map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.issues[toInt(m["iid"])] = m
}

// AddNote seeds a note on issue iid and returns its ID.
func (s *Server) AddNote(iid int, author, body string, system bool) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.newID()
	s.notes[iid] = append(s.notes[iid], map[string]any{
		"id": id, "body": body, "system": system,
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"author":     map[string]any{"username": author},
	})
	return id
}

// AddAward seeds an award emoji on a note; iid is the issue the note belongs to.
func (s *Server) AddAward(iid int, noteID int64, name, user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.awards[noteID] = append(s.awards[noteID], map[string]any{
		"id": s.newID(), "name": name, "user": map[string]any{"username": user},
	})
}

// SetMember makes user a member of the project with the GitLab access level (10 to 50).
func (s *Server) SetMember(user string, accessLevel int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userIDLocked(user)
	s.members[user] = accessLevel
}

// AddLink seeds an issue link (a blocker or related issue) on issue iid.
func (s *Server) AddLink(iid int, link map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[iid] = append(s.links[iid], link)
}

// AddClosedBy seeds a merge request that closes issue iid of project, which may be another
// project than Project.
func (s *Server) AddClosedBy(project string, iid int, mr map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s#%d", project, iid)
	s.closedBy[key] = append(s.closedBy[key], mr)
}

// AddRelatedMR seeds a merge request related to issue iid of Project: GitLab lists one that
// closes the issue there as well as one that mentions it.
func (s *Server) AddRelatedMR(iid int, mr map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.related[iid] = append(s.related[iid], mr)
}

// AddStateEvent seeds a resource state event of issue iid (state, source_merge_request), after
// the ones seeded before.
func (s *Server) AddStateEvent(iid int, ev map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateEvents[iid] = append(s.stateEvents[iid], ev)
}

// FailIssueMRsPage2 makes page two of closed_by and related_merge_requests of issue iid of
// Project answer a 500.
func (s *Server) FailIssueMRsPage2(iid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failIssueMRsPage2[iid] = true
}

// FailNotes makes creating a note on issue iid answer a 500.
func (s *Server) FailNotes(iid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNotes == nil {
		s.failNotes = map[int]bool{}
	}
	s.failNotes[iid] = true
}

// AddMR seeds a merge request in GitLab's REST shape. A head_pipeline in it is served only by
// the single merge request, not by the list.
func (s *Server) AddMR(m map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mrs = append(s.mrs, m)
}

// AddLabel adds a label to issue iid, which must be seeded.
func (s *Server) AddLabel(iid int, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.issues[iid]
	var labels []any
	switch ls := it["labels"].(type) {
	case []string:
		for _, l := range ls {
			labels = append(labels, l)
		}
	case []any:
		labels = ls
	}
	it["labels"] = append(labels, label)
}

// SetStatuses sets the status lifecycle of the top-level group, the names a work item can have.
func (s *Server) SetStatuses(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = slices.Clone(names)
}

// SetWorkItemStatus sets the status of issue iid.
func (s *Server) SetWorkItemStatus(iid int, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status[iid] = name
}

// WorkItemStatus returns the status of issue iid, "" when it has none.
func (s *Server) WorkItemStatus(iid int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status[iid]
}

// paginate serves items as one page of a JSON array, as GitLab does: page (default 1) and
// per_page (default 20, at most 100) pick the page, and x-next-page names the next one, or is
// empty on the last.
func paginate(w http.ResponseWriter, r *http.Request, items []any) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	per, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if per < 1 {
		per = 20
	}
	per = min(per, 100)
	start := min((page-1)*per, len(items))
	end := min(start+per, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(page + 1)
	}
	w.Header().Set("x-next-page", next)
	out := items[start:end]
	if out == nil {
		out = []any{}
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func toAny(in []map[string]any) []any {
	out := make([]any, len(in))
	for i, m := range in {
		out[i] = m
	}
	return out
}

func sortedKeys(m map[int]map[string]any) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

// applyUpdate applies the fields of a PUT to an issue or merge request; the caller holds mu.
func applyUpdate(it, body map[string]any) {
	for _, k := range []string{"title", "description"} {
		if v, ok := body[k]; ok {
			it[k] = v
		}
	}
	switch body["state_event"] {
	case "close":
		it["state"], it["closed_at"] = "closed", now()
	case "reopen":
		it["state"] = "opened"
		delete(it, "closed_at")
	}
	it["updated_at"] = now()
}

// updateMR changes description, title and state_event of a merge request.
func (s *Server) updateMR(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	var body map[string]any
	if !s.decode(w, r, &body) {
		return
	}
	s.mu.Lock()
	var cp map[string]any
	for _, mr := range s.mrs {
		if toInt(mr["iid"]) == iid {
			applyUpdate(mr, body)
			cp = maps.Clone(mr)
		}
	}
	s.mu.Unlock()
	if cp == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
		return
	}
	writeJSON(w, http.StatusOK, cp)
}

func (s *Server) listMRNotes(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	s.mu.Lock()
	out := toAny(s.mrNotes[iid])
	s.mu.Unlock()
	paginate(w, r, out)
}

// MRNotes lists the bodies of the notes on merge request iid, oldest first.
func (s *Server) MRNotes(iid int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, n := range s.mrNotes[iid] {
		out = append(out, fmt.Sprint(n["body"]))
	}
	return out
}
