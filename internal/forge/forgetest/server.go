// Package forgetest is a fake GitHub for tests of forge and its callers. It answers the REST
// routes and GraphQL operations the forge client uses, from plain fields, and counts requests.
// It does not import forge: the shapes it serves are GitHub's, not the client's.
package forgetest

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Server is a fake GitHub API. Set its fields before the first request; read Posts and Count
// after the requests returned. A POSTed comment or 🚀 lands in Comments or Rockets, so the
// next GET returns it; read those through IssueComments while requests may still run.
type Server struct {
	// Login is the login GET /user answers for the token, and the author of what is POSTed.
	Login string
	// Now stamps the created_at of a POSTed comment; nil is time.Now.
	Now func() time.Time
	// Comments are the comments per issue number, in GitHub's REST shape (id, body, created_at,
	// html_url, user, reactions). A comment without a reactions key gets reactions.rocket from
	// the length of Rockets[id].
	Comments map[int][]map[string]any
	// BeforeComments, when set, runs before a GET of the comments of an issue is served, with
	// its number: a test can change the forge or the database between two requests.
	BeforeComments func(number int)
	// Issues are the issues per number in GitHub's REST shape, served by GET
	// /repos/{owner}/{name}/issues/{number}; a missing number answers 404.
	Issues map[int]map[string]any
	// Pulls are the open pull requests in REST shape (number, title, body, head.ref), served by
	// GET /repos/{owner}/{name}/pulls in the order given.
	Pulls []map[string]any
	// BlockedBy are the blockers per issue number in REST issue shape (number, state,
	// repository_url), served by GET .../issues/{number}/dependencies/blocked_by.
	BlockedBy map[int][]map[string]any
	// Fail maps "METHOD path", such as "POST /repos/o/r/issues/7/comments", to the status that
	// request answers instead. A failed POST is still in Posts, but changes nothing.
	Fail map[string]int
	// Roles maps a login to its role_name on the repo; a login that is missing answers 404.
	Roles map[string]string
	// RoleStatus, when not 0, is the status every permission request answers.
	RoleStatus int
	// Rockets are the logins with a 🚀 per comment ID.
	Rockets map[int64][]string
	// Branches are the pull request nodes per head branch, in GraphQL shape (number, state,
	// isDraft, mergeable, headRefOid, headRefName, statusCheckRollup). BranchPRs serves the
	// first 20, like the query asks.
	Branches map[string][]map[string]any
	// BoardNodes are the project items in GraphQL shape, served by Board in pages of 100. A
	// nested connection carries its own pageInfo when a test sets one. A node with
	// "isArchived": true is left out of Board, as ProjectV2.items lists no archived item, and
	// served by IssueItems, as Issue.projectItems includes them.
	BoardNodes []map[string]any
	// ProjectID is the project every BoardNodes item belongs to, as IssueItems serves it in
	// project { id }.
	ProjectID string
	// BoardNotAProject makes Board answer {"node": {}}, as GitHub does for an ID of another type.
	BoardNotAProject bool
	// BoardStuckCursor, when not nil, makes every Board page claim a next page with this
	// endCursor, whatever the cursor asked.
	BoardStuckCursor *string
	// GraphQLErrors, when set, makes every GraphQL operation answer 200 with these errors and
	// null data.
	GraphQLErrors []string
	// FailOps maps a GraphQL operation name, such as "SetStatus", to an error message: only
	// that operation answers 200 with that error and null data; the others work as usual.
	FailOps map[string]string

	// StatusSets are the variables of every SetStatus, in order; a failed one is in it too, but
	// changes nothing. Read it after the requests returned.
	StatusSets []map[string]any

	// Posts are the JSON bodies of all POST requests, in order.
	Posts []Post

	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	counts map[string]int
}

// Post is one POST request as the server received it.
type Post struct {
	Path string
	Body map[string]any
}

// New starts a server that closes when the test ends.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{t: t, counts: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user", s.user)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues", s.listIssues)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/{number}", s.issue)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/{number}/dependencies/blocked_by", s.blockedBy)
	mux.HandleFunc("GET /repos/{owner}/{name}/pulls", s.pulls)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/{number}/comments", s.comments)
	mux.HandleFunc("POST /repos/{owner}/{name}/issues/{number}/comments", s.createComment)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/comments/{id}/reactions", s.reactions)
	mux.HandleFunc("POST /repos/{owner}/{name}/issues/comments/{id}/reactions", s.addReaction)
	mux.HandleFunc("GET /repos/{owner}/{name}/collaborators/{user}/permission", s.permission)
	mux.HandleFunc("POST /graphql", s.graphql)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("forgetest: unexpected request %s %s", r.Method, r.URL)
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
	})
	s.srv = httptest.NewServer(s.record(mux))
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the base URL to give the client.
func (s *Server) URL() string { return s.srv.URL }

// Count returns how many requests hit key: a REST path such as "/user" or
// "/repos/o/r/issues/7/comments", or a GraphQL operation name such as "Board".
func (s *Server) Count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[key]
}

func (s *Server) inc(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[key]++
}

// record counts the path, refuses a request without a token, keeps every POST body, and
// answers the status of Fail for the request when it names one.
func (s *Server) record(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.inc(r.URL.Path)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Requires authentication"})
			return
		}
		if r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				s.t.Errorf("forgetest: POST %s: body is no JSON object: %v", r.URL.Path, err)
			}
			s.mu.Lock()
			s.Posts = append(s.Posts, Post{Path: r.URL.Path, Body: body})
			s.mu.Unlock()
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}
		if status := s.Fail[r.Method+" "+r.URL.Path]; status != 0 {
			writeJSON(w, status, map[string]any{"message": http.StatusText(status)})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) user(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"login": s.Login})
}

func (s *Server) issue(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("number"))
	issue, ok := s.Issues[n]
	if err != nil || !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, issue)
}

// listIssues serves Issues by number, filtered on state (default open; an item without state
// is open) and milestone (the milestone.number), in pages.
func (s *Server) listIssues(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	milestone := r.URL.Query().Get("milestone")
	var nums []int
	for n := range s.Issues {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	var out []any
	for _, n := range nums {
		it := s.Issues[n]
		if st, _ := it["state"].(string); state != "all" && state != cmp.Or(st, "open") {
			continue
		}
		if milestone != "" {
			m, _ := it["milestone"].(map[string]any)
			if m == nil || fmt.Sprint(m["number"]) != milestone {
				continue
			}
		}
		out = append(out, it)
	}
	s.page(w, r, out)
}

func (s *Server) blockedBy(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	var out []any
	for _, b := range s.BlockedBy[n] {
		out = append(out, b)
	}
	s.page(w, r, out)
}

func (s *Server) pulls(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, p := range s.Pulls {
		out = append(out, p)
	}
	s.page(w, r, out)
}

func (s *Server) comments(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	if s.BeforeComments != nil {
		s.BeforeComments(n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	for _, c := range s.Comments[n] {
		if _, ok := c["reactions"]; !ok {
			cp := make(map[string]any, len(c)+1)
			for k, v := range c {
				cp[k] = v
			}
			cp["reactions"] = map[string]any{"rocket": len(s.Rockets[commentID(c["id"])])}
			c = cp
		}
		out = append(out, c)
	}
	s.page(w, r, out)
}

func (s *Server) reactions(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("content") != "rocket" {
		s.t.Errorf("forgetest: reactions request %s without content=rocket", r.URL)
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []any
	for _, login := range s.Rockets[id] {
		out = append(out, map[string]any{"content": "rocket", "user": map[string]any{"login": login}})
	}
	s.page(w, r, out)
}

// createComment stores the comment as Login's and answers 201 with it, as GitHub does.
func (s *Server) createComment(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("number"))
	var req struct {
		Body string `json:"body"`
	}
	if err == nil {
		err = json.NewDecoder(r.Body).Decode(&req)
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed"})
		return
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	writeJSON(w, http.StatusCreated, s.addComment(repo, n, s.Login, req.Body))
}

// AddComment appends a comment by login to issue n, with the next free id, and returns it in
// GitHub's REST shape; its html_url names repo o/r. Safe to call while requests run, also
// from BeforeComments.
func (s *Server) AddComment(n int, login, body string) map[string]any {
	return s.addComment("o/r", n, login, body)
}

// addComment keeps comments per issue number, whatever repo; repo only shapes html_url.
func (s *Server) addComment(repo string, n int, login, body string) map[string]any {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var id int64
	for _, cs := range s.Comments {
		for _, c := range cs {
			id = max(id, commentID(c["id"]))
		}
	}
	id++
	if s.Comments == nil {
		s.Comments = map[int][]map[string]any{}
	}
	c := map[string]any{"id": id, "body": body, "created_at": now().UTC().Format(time.RFC3339),
		"html_url": fmt.Sprintf("https://github.com/%s/issues/%d#issuecomment-%d", repo, n, id),
		"user":     map[string]any{"login": login}}
	s.Comments[n] = append(s.Comments[n], c)
	return c
}

// IssueComments returns the comments the server holds on issue n, posted ones included.
func (s *Server) IssueComments(n int) []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any{}, s.Comments[n]...)
}

// addReaction adds Login's 🚀 to the comment once, as GitHub keeps one reaction per user.
func (s *Server) addReaction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Content string `json:"content"`
	}
	if err == nil {
		err = json.NewDecoder(r.Body).Decode(&req)
	}
	if err != nil || req.Content != "rocket" {
		s.t.Errorf("forgetest: reaction %s: %v, content %q; only rocket is served", r.URL, err, req.Content)
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Rockets == nil {
		s.Rockets = map[int64][]string{}
	}
	status := http.StatusOK
	if !slices.Contains(s.Rockets[id], s.Login) {
		s.Rockets[id] = append(s.Rockets[id], s.Login)
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"content": "rocket", "user": map[string]any{"login": s.Login}})
}

// legacyPermission is GitHub's permission field per role: it knows no maintain or triage,
// which is why the client reads role_name.
var legacyPermission = map[string]string{"admin": "admin", "maintain": "write", "write": "write", "triage": "read", "read": "read"}

func (s *Server) permission(w http.ResponseWriter, r *http.Request) {
	if s.RoleStatus != 0 {
		writeJSON(w, s.RoleStatus, map[string]any{"message": http.StatusText(s.RoleStatus)})
		return
	}
	role, ok := s.Roles[r.PathValue("user")]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permission": legacyPermission[role], "role_name": role})
}

// page serves one page of items as GitHub does: per_page (default 30, at most 100) and page
// from the query, and a Link rel="next" while more items follow.
func (s *Server) page(w http.ResponseWriter, r *http.Request, items []any) {
	q := r.URL.Query()
	per, err := strconv.Atoi(q.Get("per_page"))
	if err != nil || per <= 0 {
		per = 30
	}
	per = min(per, 100)
	page, err := strconv.Atoi(q.Get("page"))
	if err != nil || page < 1 {
		page = 1
	}
	start := min((page-1)*per, len(items))
	end := min(start+per, len(items))
	if end < len(items) {
		q.Set("page", strconv.Itoa(page+1))
		next := url.URL{Path: r.URL.Path, RawQuery: q.Encode()}
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, s.srv.URL, next.String()))
	}
	writeJSON(w, http.StatusOK, append([]any{}, items[start:end]...))
}

func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "Problems parsing JSON"})
		return
	}
	op := operationName(req.Query)
	s.inc(op)
	if op == "SetStatus" {
		s.mu.Lock()
		s.StatusSets = append(s.StatusSets, req.Variables)
		s.mu.Unlock()
	}
	msgs := s.GraphQLErrors
	if m, ok := s.FailOps[op]; ok {
		msgs = []string{m}
	}
	if len(msgs) > 0 {
		var errs []any
		for _, m := range msgs {
			errs = append(errs, map[string]any{"message": m})
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": errs})
		return
	}
	var data any
	switch op {
	case "Board":
		data = s.board(req.Variables)
	case "BranchPRs":
		branch, _ := req.Variables["branch"].(string)
		nodes := s.Branches[branch]
		more := len(nodes) > 20
		nodes = nodes[:min(len(nodes), 20)]
		data = map[string]any{"repository": map[string]any{"pullRequests": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": more},
			"nodes":    append([]map[string]any{}, nodes...),
		}}}
	case "IssueItems":
		data = s.issueItems(req.Query, req.Variables)
	case "AddToBoard":
		data = s.addToBoard(req.Variables)
	case "SetStatus":
		data = map[string]any{"updateProjectV2ItemFieldValue": map[string]any{
			"projectV2Item": map[string]any{"id": req.Variables["item"]},
		}}
	default:
		s.t.Errorf("forgetest: unknown GraphQL operation %q", op)
		writeJSON(w, http.StatusOK, map[string]any{"errors": []any{map[string]any{"message": "unknown operation " + op}}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// board serves one page of 100 BoardNodes after the cursor "cursor-<offset>".
func (s *Server) board(vars map[string]any) any {
	if s.BoardNotAProject {
		return map[string]any{"node": map[string]any{}}
	}
	var live []map[string]any
	for _, n := range s.BoardNodes {
		if archived, _ := n["isArchived"].(bool); !archived {
			live = append(live, n)
		}
	}
	start := 0
	if after, _ := vars["after"].(string); after != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(after, "cursor-"))
		if err != nil {
			s.t.Errorf("forgetest: Board after = %q, not a cursor this server gave", after)
		}
		start = min(n, len(live))
	}
	end := min(start+100, len(live))
	more, cursor := end < len(live), fmt.Sprintf("cursor-%d", end)
	if s.BoardStuckCursor != nil {
		more, cursor = true, *s.BoardStuckCursor
	}
	return map[string]any{"node": map[string]any{"items": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor},
		"nodes":    append([]map[string]any{}, live[start:end]...),
	}}}
}

// issueItems serves the BoardNodes whose content.number is $number, each with project { id }
// from ProjectID. The content is left out, as the real query does not select it, and so is
// isArchived when the query does not select it.
func (s *Server) issueItems(query string, vars map[string]any) any {
	number, _ := vars["number"].(float64)
	s.mu.Lock()
	defer s.mu.Unlock()
	nodes := []map[string]any{}
	for _, n := range s.BoardNodes {
		content, _ := n["content"].(map[string]any)
		if num, ok := content["number"].(int); !ok || float64(num) != number {
			continue
		}
		cp := map[string]any{"project": map[string]any{"id": s.ProjectID}}
		for k, v := range n {
			if k != "content" && (k != "isArchived" || strings.Contains(query, "isArchived")) {
				cp[k] = v
			}
		}
		nodes = append(nodes, cp)
	}
	return map[string]any{"repository": map[string]any{"issue": map[string]any{"projectItems": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": false},
		"nodes":    nodes,
	}}}}
}

// addToBoard appends a BoardNodes item "item-<content>" for the Issues item whose node_id is
// $content, and answers its id.
func (s *Server) addToBoard(vars map[string]any) any {
	content, _ := vars["content"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	for n, it := range s.Issues {
		if id, _ := it["node_id"].(string); id == content {
			item := "item-" + content
			s.BoardNodes = append(s.BoardNodes, map[string]any{"id": item, "content": map[string]any{"number": n}})
			return map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": item}}}
		}
	}
	s.t.Errorf("forgetest: AddToBoard content %q matches no Issues node_id", content)
	return map[string]any{"addProjectV2ItemById": nil}
}

// operationName is the name after "query " or "mutation " at the start of a query.
func operationName(query string) string {
	q := strings.TrimSpace(query)
	for _, kw := range []string{"query ", "mutation "} {
		if rest, ok := strings.CutPrefix(q, kw); ok {
			end := strings.IndexFunc(rest, func(r rune) bool {
				return r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
			})
			if end < 0 {
				return rest
			}
			return rest[:end]
		}
	}
	return ""
}

// commentID reads a comment's id as a test may have written it.
func commentID(v any) int64 {
	switch id := v.(type) {
	case int:
		return int64(id)
	case int64:
		return id
	case float64:
		return int64(id)
	}
	return 0
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
