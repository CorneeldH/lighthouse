package forgetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The write routes below keep their state in the exported fields, so the next GET shows it.
// They refuse a field GitHub would not take, so a test cannot pass on a field GitHub ignores.

var (
	issueFields = []string{"title", "body", "labels", "milestone", "state", "assignees"}
	pullFields  = []string{"title", "body", "head", "base", "draft", "maintainer_can_modify"}
)

func (s *Server) decode(w http.ResponseWriter, r *http.Request, allowed []string) (map[string]any, bool) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Problems parsing JSON"})
		return nil, false
	}
	for k := range body {
		if !slices.Contains(allowed, k) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed: unknown field " + k})
			return nil, false
		}
	}
	return body, true
}

// nextNumber is the next free issue or pull request number. Caller holds s.mu.
func (s *Server) nextNumber() int {
	n := 0
	for k := range s.Issues {
		n = max(n, k)
	}
	for _, p := range s.Pulls {
		n = max(n, int(commentID(p["number"])))
	}
	return n + 1
}

// milestoneObject is the milestone with this number, as the issue shape embeds it. Caller holds s.mu.
func (s *Server) milestoneObject(v any) (any, bool) {
	if v == nil {
		return nil, true
	}
	num, ok := v.(float64)
	if !ok {
		return nil, false
	}
	for _, m := range s.Milestones {
		if fmt.Sprint(m["number"]) == strconv.Itoa(int(num)) {
			return m, true
		}
	}
	return nil, false
}

func labelObjects(names []string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

// labelNames reads the label names of an issue in either shape a test may have written.
func labelNames(v any) []string {
	var out []string
	switch ls := v.(type) {
	case []any:
		for _, l := range ls {
			if m, ok := l.(map[string]any); ok {
				out = append(out, fmt.Sprint(m["name"]))
			}
		}
	case []map[string]any:
		for _, m := range ls {
			out = append(out, fmt.Sprint(m["name"]))
		}
	}
	return out
}

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, issueFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ms, ok := s.milestoneObject(body["milestone"])
	if !ok {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed: milestone does not exist"})
		return
	}
	var names []string
	if ls, ok := body["labels"].([]any); ok {
		for _, l := range ls {
			names = append(names, fmt.Sprint(l))
		}
	}
	n := s.nextNumber()
	if s.Issues == nil {
		s.Issues = map[int]map[string]any{}
	}
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	it := map[string]any{"number": n, "id": 9000 + n, "node_id": fmt.Sprintf("I_%d", n), "title": body["title"], "body": body["body"], "state": "open",
		"html_url": fmt.Sprintf("https://github.com/%s/issues/%d", repo, n), "labels": labelObjects(names),
		"milestone": ms, "created_at": time.Now().UTC().Format(time.RFC3339), "user": map[string]any{"login": s.Login}}
	s.Issues[n] = it
	writeJSON(w, http.StatusCreated, it)
}

// target is the issue or pull request with this number, whose issue-side fields (title,
// milestone, labels) PATCH and the label routes change. Caller holds s.mu.
func (s *Server) target(r *http.Request) map[string]any {
	n, err := strconv.Atoi(r.PathValue("number"))
	if err != nil {
		return nil
	}
	if it, ok := s.Issues[n]; ok {
		return it
	}
	for _, p := range s.Pulls {
		if p["number"] == n {
			return p
		}
	}
	return nil
}

func (s *Server) patchIssue(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, issueFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.target(r)
	if it == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	for k, v := range body {
		if k == "milestone" {
			ms, ok := s.milestoneObject(v)
			if !ok {
				writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed: milestone does not exist"})
				return
			}
			v = ms
		}
		it[k] = v
		// A pull request kept apart in Issues and Pulls shows the change on both sides.
		if pi := s.pullIndex(int(commentID(it["number"]))); pi >= 0 {
			s.Pulls[pi][k] = v
		}
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) addLabels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Labels []string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Problems parsing JSON"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.target(r)
	if it == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	names := labelNames(it["labels"])
	for _, l := range req.Labels {
		if !slices.Contains(names, l) {
			names = append(names, l)
		}
	}
	it["labels"] = labelObjects(names)
	writeJSON(w, http.StatusOK, it["labels"])
}

func (s *Server) removeLabel(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it := s.target(r)
	if it == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	label := r.PathValue("label")
	names := labelNames(it["labels"])
	if !slices.Contains(names, label) {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Label does not exist"})
		return
	}
	names = slices.DeleteFunc(names, func(n string) bool { return n == label })
	it["labels"] = labelObjects(names)
	writeJSON(w, http.StatusOK, it["labels"])
}

// addBlockedBy takes issue_id as a number, the database id of the blocker, as GitHub does.
func (s *Server) addBlockedBy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IssueID json.Number `json:"issue_id"`
	}
	n, err := strconv.Atoi(r.PathValue("number"))
	if err == nil {
		err = json.NewDecoder(r.Body).Decode(&req)
	}
	if err == nil {
		_, err = req.IssueID.Int64()
	}
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed: issue_id must be an integer"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for num, it := range s.Issues {
		if fmt.Sprint(it["id"]) != req.IssueID.String() {
			continue
		}
		if s.BlockedBy == nil {
			s.BlockedBy = map[int][]map[string]any{}
		}
		state, _ := it["state"].(string)
		s.BlockedBy[n] = append(s.BlockedBy[n], map[string]any{"number": num, "state": state,
			"repository_url": "https://api.github.com/repos/" + r.PathValue("owner") + "/" + r.PathValue("name")})
		writeJSON(w, http.StatusCreated, it)
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Validation Failed: no issue with that id"})
}

// searchIssues serves Issues (pull requests included, which the client must drop) whose title
// or body holds the text of q after "is:issue", ignoring case.
func (s *Server) searchIssues(w http.ResponseWriter, r *http.Request) {
	_, text, _ := strings.Cut(r.URL.Query().Get("q"), "is:issue ")
	text = strings.ToLower(text)
	var nums []int
	for n := range s.Issues {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	var out []any
	for _, n := range nums {
		it := s.Issues[n]
		hay := strings.ToLower(fmt.Sprint(it["title"], " ", it["body"]))
		if strings.Contains(hay, text) {
			out = append(out, it)
		}
	}
	s.pageAs(w, r, out, func(items []any) any {
		return map[string]any{"total_count": len(out), "items": items}
	})
}

// createPull refuses any field POST pulls does not take, milestone included. The pull request
// is also an issue, as on GitHub: Issues holds the same map under its number, so the issue
// routes change it and the issues list shows it with a pull_request key.
func (s *Server) createPull(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, pullFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.nextNumber()
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	url := fmt.Sprintf("https://github.com/%s/pull/%d", repo, n)
	draft, _ := body["draft"].(bool)
	p := map[string]any{"number": n, "node_id": fmt.Sprintf("PR_%d", n), "title": body["title"], "body": body["body"],
		"state": "open", "draft": draft, "merged_at": nil, "html_url": url,
		"created_at": time.Now().UTC().Format(time.RFC3339), "user": map[string]any{"login": s.Login},
		"head": map[string]any{"ref": body["head"], "sha": fmt.Sprintf("sha-%d", n)}, "base": map[string]any{"ref": body["base"]},
		"labels": []any{}, "milestone": nil, "pull_request": map[string]any{"url": url}}
	s.Pulls = append(s.Pulls, p)
	if s.Issues == nil {
		s.Issues = map[int]map[string]any{}
	}
	s.Issues[n] = p
	writeJSON(w, http.StatusCreated, p)
}

// patchPull changes the title, body, state or base of a pull request, and of its issue side
// when Issues holds that apart.
func (s *Server) patchPull(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, []string{"title", "body", "state", "base", "maintainer_can_modify"})
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, _ := strconv.Atoi(r.PathValue("number"))
	pi := s.pullIndex(n)
	if pi < 0 {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	for k, v := range body {
		if k == "base" {
			v = map[string]any{"ref": v}
		}
		s.Pulls[pi][k] = v
		if it, ok := s.Issues[n]; ok && k != "base" {
			it[k] = v
		}
	}
	writeJSON(w, http.StatusOK, s.Pulls[pi])
}

func (s *Server) listMilestones(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	var out []any
	for _, m := range s.Milestones {
		if st, _ := m["state"].(string); state == "all" || st == state {
			out = append(out, m)
		}
	}
	s.page(w, r, out)
}

func (s *Server) milestone(w http.ResponseWriter, r *http.Request) {
	for _, m := range s.Milestones {
		if fmt.Sprint(m["number"]) == r.PathValue("number") {
			writeJSON(w, http.StatusOK, m)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
}

func (s *Server) createMilestone(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, []string{"title", "description", "state", "due_on"})
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.Milestones {
		if v, ok := m["number"].(int); ok {
			n = max(n, v)
		}
	}
	m := map[string]any{"number": n + 1, "title": body["title"], "state": "open", "description": body["description"]}
	s.Milestones = append(s.Milestones, m)
	writeJSON(w, http.StatusCreated, m)
}

func (s *Server) patchMilestone(w http.ResponseWriter, r *http.Request) {
	body, ok := s.decode(w, r, []string{"title", "description", "state", "due_on"})
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.Milestones {
		if fmt.Sprint(m["number"]) == r.PathValue("number") {
			for k, v := range body {
				m[k] = v
			}
			writeJSON(w, http.StatusOK, m)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
}

func (s *Server) labels(w http.ResponseWriter, r *http.Request) {
	var out []any
	for _, n := range s.Labels {
		out = append(out, map[string]any{"name": n})
	}
	s.page(w, r, out)
}
