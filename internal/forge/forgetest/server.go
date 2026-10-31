// Package forgetest is a fake GitHub for tests of forge and its callers. It answers the REST
// routes and GraphQL operations the forge client uses, from plain fields, and counts requests.
// It does not import forge: the shapes it serves are GitHub's, not the client's.
package forgetest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Server is a fake GitHub API. Set its fields before the first request; read Posts and Count
// after the requests returned.
type Server struct {
	// Login is the login GET /user answers for the token.
	Login string
	// Comments are the comments per issue number, in GitHub's REST shape (id, body, created_at,
	// html_url, user, reactions). A comment without a reactions key gets reactions.rocket from
	// the length of Rockets[id].
	Comments map[int][]map[string]any
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
	// nested connection carries its own pageInfo when a test sets one.
	BoardNodes []map[string]any
	// BoardNotAProject makes Board answer {"node": {}}, as GitHub does for an ID of another type.
	BoardNotAProject bool
	// BoardStuckCursor, when not nil, makes every Board page claim a next page with this
	// endCursor, whatever the cursor asked.
	BoardStuckCursor *string
	// GraphQLErrors, when set, makes every GraphQL operation answer 200 with these errors and
	// null data.
	GraphQLErrors []string

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
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/{number}/comments", s.comments)
	mux.HandleFunc("GET /repos/{owner}/{name}/issues/comments/{id}/reactions", s.reactions)
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

// record counts the path, refuses a request without a token, and keeps every POST body.
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
		next.ServeHTTP(w, r)
	})
}

func (s *Server) user(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"login": s.Login})
}

func (s *Server) comments(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
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
	var out []any
	for _, login := range s.Rockets[id] {
		out = append(out, map[string]any{"content": "rocket", "user": map[string]any{"login": login}})
	}
	s.page(w, r, out)
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
	if len(s.GraphQLErrors) > 0 {
		var errs []any
		for _, m := range s.GraphQLErrors {
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
	start := 0
	if after, _ := vars["after"].(string); after != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(after, "cursor-"))
		if err != nil {
			s.t.Errorf("forgetest: Board after = %q, not a cursor this server gave", after)
		}
		start = min(n, len(s.BoardNodes))
	}
	end := min(start+100, len(s.BoardNodes))
	more, cursor := end < len(s.BoardNodes), fmt.Sprintf("cursor-%d", end)
	if s.BoardStuckCursor != nil {
		more, cursor = true, *s.BoardStuckCursor
	}
	return map[string]any{"node": map[string]any{"items": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": more, "endCursor": cursor},
		"nodes":    append([]map[string]any{}, s.BoardNodes[start:end]...),
	}}}
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
