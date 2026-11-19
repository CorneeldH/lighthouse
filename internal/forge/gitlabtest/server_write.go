package gitlabtest

import (
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Write is a request that is no GET, as the server received it: Path is the escaped path, Body
// the decoded JSON object (nil when the body was none).
type Write struct {
	Method, Path string
	Body         map[string]any
}

// Writes returns every non-GET request received, in order.
func (s *Server) Writes() []Write {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.writes)
}

// AddMilestone seeds a milestone in GitLab's REST shape: id (global), iid, title, state (active
// or closed), description, and project_id or group_id. A group milestone only shows up in the
// list of a project with include_ancestors=true.
func (s *Server) AddMilestone(m map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.milestones = append(s.milestones, m)
}

// SetLabels sets the names of the project labels.
func (s *Server) SetLabels(names ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.labels = slices.Clone(names)
}

// The write routes refuse a field GitLab would not take, with 400, so a test cannot pass on a
// field GitLab ignores.
var (
	issueCreateFields = []string{"title", "description", "labels", "milestone_id"}
	issueEditFields   = []string{"title", "description", "add_labels", "remove_labels", "milestone_id", "state_event"}
	mrCreateFields    = []string{"title", "description", "source_branch", "target_branch", "milestone_id"}
	milestoneFields   = []string{"title", "description", "state_event"}
)

func (s *Server) writeRoutes(mux *http.ServeMux, p string) {
	s.route(mux, "POST "+p+"/issues", s.project(s.createIssue))
	s.route(mux, "PUT "+p+"/issues/{iid}", s.project(s.editIssue))
	s.route(mux, "POST "+p+"/issues/{iid}/links", s.project(s.createLink))
	s.route(mux, "GET "+p+"/milestones", s.project(s.listMilestones))
	s.route(mux, "POST "+p+"/milestones", s.project(s.createMilestone))
	s.route(mux, "PUT "+p+"/milestones/{id}", s.project(s.editMilestone))
	s.route(mux, "GET "+p+"/labels", s.project(s.listLabels))
	s.route(mux, "POST "+p+"/merge_requests", s.project(s.createMR))
	s.route(mux, "POST "+p+"/merge_requests/{iid}/notes", s.project(s.createMRNote))
}

// body decodes the JSON object and answers 400 for a field outside allowed.
func (s *Server) body(w http.ResponseWriter, r *http.Request, allowed []string) (map[string]any, bool) {
	var body map[string]any
	if !s.decode(w, r, &body) {
		return nil, false
	}
	for k := range body {
		if !slices.Contains(allowed, k) {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request - unknown parameter " + k})
			return nil, false
		}
	}
	return body, true
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]any{"message": "404 Not found"})
}

// milestoneObject is the milestone with global id v, as an issue embeds it. Caller holds mu.
func (s *Server) milestoneObject(v any) (map[string]any, bool) {
	id := toInt(v)
	for _, m := range s.milestones {
		if toInt(m["id"]) == id {
			return map[string]any{"id": m["id"], "iid": m["iid"], "title": m["title"], "state": m["state"], "description": m["description"]}, true
		}
	}
	return nil, false
}

// labelsOf is the label names of an issue, whatever type the seed used.
func labelsOf(it map[string]any) []string {
	switch ls := it["labels"].(type) {
	case []string:
		return slices.Clone(ls)
	case []any:
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = fmt.Sprint(l)
		}
		return out
	}
	return nil
}

func hasLabels(it map[string]any, csv string) bool {
	have := labelsOf(it)
	for _, l := range strings.Split(csv, ",") {
		if !slices.Contains(have, l) {
			return false
		}
	}
	return true
}

func splitCSV(v any) []string {
	s, _ := v.(string)
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Server) createIssue(w http.ResponseWriter, r *http.Request) {
	body, ok := s.body(w, r, issueCreateFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iid := 0
	for k := range s.issues {
		iid = max(iid, k)
	}
	iid++
	it := map[string]any{"iid": iid, "id": s.newID(), "title": body["title"], "description": body["description"], "state": "opened",
		"labels": splitCSV(body["labels"]), "created_at": now(), "updated_at": now(),
		"web_url": fmt.Sprintf("https://gitlab.example/%s/-/issues/%d", s.Project, iid),
		"author":  map[string]any{"username": s.Login}}
	if id, set := body["milestone_id"]; set {
		m, found := s.milestoneObject(id)
		if !found {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request - milestone not found"})
			return
		}
		it["milestone"] = m
	}
	s.issues[iid] = it
	writeJSON(w, http.StatusCreated, it)
}

func (s *Server) editIssue(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	body, ok := s.body(w, r, issueEditFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, found := s.issues[iid]
	if !found {
		notFound(w)
		return
	}
	for _, k := range []string{"title", "description"} {
		if v, set := body[k]; set {
			it[k] = v
		}
	}
	labels := labelsOf(it)
	for _, l := range splitCSV(body["add_labels"]) {
		if !slices.Contains(labels, l) {
			labels = append(labels, l)
		}
	}
	for _, l := range splitCSV(body["remove_labels"]) {
		labels = slices.DeleteFunc(labels, func(x string) bool { return x == l })
	}
	it["labels"] = labels
	if id, set := body["milestone_id"]; set {
		if toInt(id) == 0 {
			it["milestone"] = nil
		} else if m, found := s.milestoneObject(id); found {
			it["milestone"] = m
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request - milestone not found"})
			return
		}
	}
	applyUpdate(it, map[string]any{"state_event": body["state_event"]})
	writeJSON(w, http.StatusOK, it)
}

// createLink adds a link on issue iid to an issue of target_project_id. On a Free plan it
// refuses with 403, as GitLab does for a blocking link.
func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	body, ok := s.body(w, r, []string{"target_project_id", "target_issue_iid", "link_type"})
	if !ok {
		return
	}
	if s.Free {
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "403 Forbidden - blocking links need a paid plan"})
		return
	}
	project, _ := body["target_project_id"].(string)
	target, _ := strconv.Atoi(fmt.Sprint(body["target_issue_iid"]))
	s.mu.Lock()
	defer s.mu.Unlock()
	state := "opened"
	if project == s.Project {
		if it, found := s.issues[target]; found {
			state, _ = it["state"].(string)
		}
	}
	link := map[string]any{"iid": target, "state": state, "link_type": body["link_type"],
		"references": map[string]any{"full": fmt.Sprintf("%s#%d", project, target)}}
	s.links[iid] = append(s.links[iid], link)
	writeJSON(w, http.StatusCreated, map[string]any{"link_type": body["link_type"]})
}

// listMilestones serves the project milestones, and the group milestones with
// include_ancestors=true, filtered on state (active or closed).
func (s *Server) listMilestones(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.mu.Lock()
	var out []any
	for _, m := range s.milestones {
		if _, group := m["group_id"]; group && q.Get("include_ancestors") != "true" {
			continue
		}
		if st := q.Get("state"); st != "" && m["state"] != st {
			continue
		}
		out = append(out, m)
	}
	s.mu.Unlock()
	paginate(w, r, out)
}

func (s *Server) createMilestone(w http.ResponseWriter, r *http.Request) {
	body, ok := s.body(w, r, milestoneFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iid := 1
	for _, m := range s.milestones {
		if _, group := m["group_id"]; !group {
			iid = max(iid, toInt(m["iid"])+1)
		}
	}
	m := map[string]any{"id": int(s.newID()), "iid": iid, "title": body["title"], "description": body["description"],
		"state": "active", "project_id": 1}
	s.milestones = append(s.milestones, m)
	writeJSON(w, http.StatusCreated, m)
}

// editMilestone edits a project milestone by its global id, as GitLab's :milestone_id is; a
// group milestone is not reachable here.
func (s *Server) editMilestone(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	body, ok := s.body(w, r, milestoneFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.milestones {
		if _, group := m["group_id"]; !group && toInt(m["id"]) == id {
			if ev, set := body["state_event"]; set {
				state, valid := map[any]string{"close": "closed", "activate": "active"}[ev]
				if !valid {
					writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request - state_event does not have a valid value"})
					return
				}
				m["state"] = state
				delete(body, "state_event")
			}
			for k, v := range body {
				m[k] = v
			}
			writeJSON(w, http.StatusOK, m)
			return
		}
	}
	notFound(w)
}

func (s *Server) listLabels(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	out := make([]any, len(s.labels))
	for i, l := range s.labels {
		out[i] = map[string]any{"name": l}
	}
	s.mu.Unlock()
	paginate(w, r, out)
}

func (s *Server) createMR(w http.ResponseWriter, r *http.Request) {
	body, ok := s.body(w, r, mrCreateFields)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	iid := 0
	for _, mr := range s.mrs {
		iid = max(iid, toInt(mr["iid"]))
	}
	iid++
	title, _ := body["title"].(string)
	mr := map[string]any{"iid": iid, "id": s.newID(), "title": body["title"], "description": body["description"], "state": "opened",
		"draft": strings.HasPrefix(title, "Draft:"), "detailed_merge_status": "mergeable",
		"source_branch": body["source_branch"], "target_branch": body["target_branch"], "labels": []string{},
		"web_url": fmt.Sprintf("https://gitlab.example/%s/-/merge_requests/%d", s.Project, iid), "created_at": now(),
		"author": map[string]any{"username": s.Login}, "sha": "head" + strconv.Itoa(iid),
		"source_project_id": 1, "target_project_id": 1}
	if id, set := body["milestone_id"]; set {
		m, found := s.milestoneObject(id)
		if !found {
			writeJSON(w, http.StatusBadRequest, map[string]any{"message": "400 Bad request - milestone not found"})
			return
		}
		mr["milestone"] = m
	}
	s.mrs = append(s.mrs, mr)
	writeJSON(w, http.StatusCreated, maps.Clone(mr))
}

func (s *Server) createMRNote(w http.ResponseWriter, r *http.Request) {
	iid, _ := strconv.Atoi(r.PathValue("iid"))
	body, ok := s.body(w, r, []string{"body"})
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := map[string]any{"id": s.newID(), "body": body["body"], "system": false, "created_at": now(),
		"author": map[string]any{"username": s.Login}}
	s.mrNotes[iid] = append(s.mrNotes[iid], n)
	writeJSON(w, http.StatusCreated, n)
}
