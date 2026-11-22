package forgetest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// DefaultFieldIDs are the node IDs of the status and priority fields the tests of this
// repository use; a Server without FieldIDs serves them as single-select fields.
var DefaultFieldIDs = []string{"F", "F_prio", "F_status", "FIELD_test", "PF", "PVTSSF_p", "PVTSSF_s", "SF"}

// linkedPRsFieldID is the REST ID of the field "Linked pull requests".
const linkedPRsFieldID = 99

// firstItemID is the REST id of BoardNodes[0]; node i has firstItemID+i.
const firstItemID = 5000

// archivedAt is the archived_at of a node with "isArchived": true.
const archivedAt = "2026-01-01T00:00:00Z"

// projectRoutes registers the project and its fields under /users/ and /orgs/.
func (s *Server) projectRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"users", "orgs"} {
		atOrg := kind == "orgs"
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}", s.projectRoute(atOrg, s.project))
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}/fields", s.projectRoute(atOrg, s.fields))
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}/items", s.projectRoute(atOrg, s.items))
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}/items/{id}", s.projectRoute(atOrg, s.item))
		mux.HandleFunc("POST /"+kind+"/{owner}/projectsV2/{number}/items", s.projectRoute(atOrg, s.addItem))
		mux.HandleFunc("PATCH /"+kind+"/{owner}/projectsV2/{number}/items/{id}", s.projectRoute(atOrg, s.patchItem))
	}
}

// projectRoute answers 404 when the project lives under the other kind of owner.
func (s *Server) projectRoute(atOrg bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if atOrg != s.ProjectAtOrg {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		h(w, r)
	}
}

func (s *Server) project(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"id": 1, "node_id": s.ProjectID})
}

func (s *Server) fields(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	ids := s.fieldNodeIDs()
	s.mu.Unlock()
	out := []any{map[string]any{"id": linkedPRsFieldID, "node_id": "PVTF_linked", "name": "Linked pull requests", "data_type": "linked_pull_requests"}}
	for i, id := range ids {
		out = append(out, map[string]any{"id": 100 + i, "node_id": id, "name": fmt.Sprintf("Field %s", id), "data_type": "single_select"})
	}
	s.page(w, r, out)
}

// fieldNodeIDs is the sorted, distinct node IDs of the single-select fields; the caller holds s.mu.
func (s *Server) fieldNodeIDs() []string {
	ids := slices.Clone(s.FieldIDs)
	if s.FieldIDs == nil {
		ids = slices.Clone(DefaultFieldIDs)
		for _, n := range s.BoardNodes {
			collectFieldIDs(n, &ids)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// collectFieldIDs appends every "field": {"id": …} found anywhere in v.
func collectFieldIDs(v any, ids *[]string) {
	switch v := v.(type) {
	case map[string]any:
		if f, ok := v["field"].(map[string]any); ok {
			if id, ok := f["id"].(string); ok {
				*ids = append(*ids, id)
			}
		}
		for _, x := range v {
			collectFieldIDs(x, ids)
		}
	case []any:
		for _, x := range v {
			collectFieldIDs(x, ids)
		}
	case []map[string]any:
		for _, x := range v {
			collectFieldIDs(x, ids)
		}
	}
}

// items serves every BoardNodes item in REST shape in pages, with only the fields the query
// names in fields=. An archived item is left out, as GitHub's list never gives one, unless
// ListArchived is set.
func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := s.wantFields(r)
	ids := s.fieldNodeIDs()
	var out []any
	for i, n := range s.BoardNodes {
		if archived, _ := n["isArchived"].(bool); archived && !s.ListArchived {
			continue
		}
		out = append(out, s.restItem(i, n, ids, want))
	}
	s.page(w, r, out)
}

// item serves the BoardNodes item whose REST id is {id}, archived or not, or 404.
func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := strconv.Atoi(r.PathValue("id"))
	i := id - firstItemID
	if err != nil || i < 0 || i >= len(s.BoardNodes) {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, s.restItem(i, s.BoardNodes[i], s.fieldNodeIDs(), s.wantFields(r)))
}

// wantFields is the set of REST field IDs in fields=; a value that is no number is a test error.
func (s *Server) wantFields(r *http.Request) map[int]bool {
	want := map[int]bool{}
	for _, f := range strings.Split(r.URL.Query().Get("fields"), ",") {
		if f == "" {
			continue
		}
		id, err := strconv.Atoi(f)
		if err != nil {
			s.t.Errorf("forgetest: fields=%q: %q is not a numeric field ID", r.URL.Query().Get("fields"), f)
		}
		want[id] = true
	}
	return want
}

// restItem translates BoardNodes item i into the REST shape of a project item. The content of
// a node with a number is an issue of its repository.nameWithOwner, any other content a draft.
// A single-select field of ids gets REST ID 100+index; "Linked pull requests" carries the
// closing PR nodes as pulls. The caller holds s.mu.
func (s *Server) restItem(i int, n map[string]any, ids []string, want map[int]bool) map[string]any {
	out := map[string]any{"id": firstItemID + i, "node_id": n["id"], "updated_at": n["updatedAt"],
		"archived_at": nil, "content_type": "DraftIssue", "content": nil}
	if archived, _ := n["isArchived"].(bool); archived {
		out["archived_at"] = archivedAt
	}
	content, _ := n["content"].(map[string]any)
	repo := nameWithOwner(content)
	if content != nil {
		out["content"] = map[string]any{"title": content["title"]}
		if _, ok := content["number"]; ok {
			out["content_type"], out["content"] = "Issue", s.restIssue(content, repo)
		}
	}
	values := map[string]map[string]any{}
	for _, v := range nodesOf(n["fieldValues"]) {
		if f, ok := v["field"].(map[string]any); ok && v["optionId"] != nil {
			values[fmt.Sprint(f["id"])] = v
		}
	}
	fields := []any{}
	for idx, id := range ids {
		if !want[100+idx] {
			continue
		}
		var value any
		if v, ok := values[id]; ok {
			name, _ := v["name"].(string)
			value = map[string]any{"id": v["optionId"], "name": map[string]any{"raw": name, "html": name}}
		}
		fields = append(fields, map[string]any{"id": 100 + idx, "name": "Field " + id, "data_type": "single_select", "value": value})
	}
	if want[linkedPRsFieldID] {
		var pulls []any
		for _, p := range nodesOf(content["closedByPullRequestsReferences"]) {
			pulls = append(pulls, restPull(p, repo))
		}
		fields = append(fields, map[string]any{"id": linkedPRsFieldID, "name": "Linked pull requests",
			"data_type": "linked_pull_requests", "value": pulls})
	}
	out["fields"] = fields
	return out
}

// restIssue is the content of an issue node in REST shape, its state in lower case and its
// dependency summary counted from its blockedBy nodes.
func (s *Server) restIssue(content map[string]any, repo string) map[string]any {
	labels := []any{}
	for _, l := range nodesOf(content["labels"]) {
		labels = append(labels, map[string]any{"name": l["name"]})
	}
	blockers := nodesOf(content["blockedBy"])
	open := 0
	for _, b := range blockers {
		if b["state"] == "OPEN" {
			open++
		}
	}
	state, _ := content["state"].(string)
	return map[string]any{
		"number": content["number"], "title": content["title"], "state": strings.ToLower(state),
		"updated_at": content["updatedAt"], "repository_url": s.repoURL(repo), "labels": labels,
		"issue_dependencies_summary": map[string]any{"blocked_by": open, "total_blocked_by": len(blockers)},
	}
}

// nodeBlockers is the blockedBy nodes of the BoardNodes item of issue number in repo, in the
// REST shape of the dependencies list. The caller holds s.mu.
func (s *Server) nodeBlockers(repo string, number int64) []any {
	for _, n := range s.BoardNodes {
		content, _ := n["content"].(map[string]any)
		if content == nil || commentID(content["number"]) != number || nameWithOwner(content) != repo {
			continue
		}
		var out []any
		for _, b := range nodesOf(content["blockedBy"]) {
			state, _ := b["state"].(string)
			out = append(out, map[string]any{"number": b["number"], "state": strings.ToLower(state),
				"repository_url": s.repoURL(nameWithOwner(b))})
		}
		return out
	}
	return nil
}

// repoURL is the repository_url of repo on this server, "" for no repo.
func (s *Server) repoURL(repo string) string {
	if repo == "" {
		return ""
	}
	return s.srv.URL + "/repos/" + repo
}

// nameWithOwner is the repository.nameWithOwner of a node, "" without one.
func nameWithOwner(node map[string]any) string {
	repo, _ := node["repository"].(map[string]any)
	name, _ := repo["nameWithOwner"].(string)
	return name
}

// nodesOf is the nodes of a connection in GraphQL shape, as a test may have written them.
func nodesOf(conn any) []map[string]any {
	c, _ := conn.(map[string]any)
	switch nodes := c["nodes"].(type) {
	case []map[string]any:
		return nodes
	case []any:
		var out []map[string]any
		for _, n := range nodes {
			if m, ok := n.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// itemPath matches the path of one project item, capturing its REST id.
var itemPath = regexp.MustCompile(`^/(?:users|orgs)/[^/]+/projectsV2/\d+/items/(\d+)$`)

// itemFieldUpdate is the first field of a PATCH body {"fields":[{"id":<field>,"value":"<option>"}]}.
func (s *Server) itemFieldUpdate(body map[string]any) (field int, option string, ok bool) {
	fields, _ := body["fields"].([]any)
	if len(fields) != 1 {
		return 0, "", false
	}
	f, _ := fields[0].(map[string]any)
	id, isNum := f["id"].(float64)
	option, isStr := f["value"].(string)
	return int(id), option, isNum && isStr
}

// recordStatusSet adds a PATCH of an item to StatusSets, as the middleware sees it before Fail
// can answer, so a failed one is in it too.
func (s *Server) recordStatusSet(path, itemID string, body map[string]any) {
	id, err := strconv.Atoi(itemID)
	field, option, ok := s.itemFieldUpdate(body)
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ids := id-firstItemID, s.fieldNodeIDs()
	if err != nil || !ok || i < 0 || i >= len(s.BoardNodes) || field-100 < 0 || field-100 >= len(ids) {
		s.t.Errorf("forgetest: PATCH %s: item or field not served, or body %v malformed", path, body)
		return
	}
	s.StatusSets = append(s.StatusSets, map[string]any{"project": s.ProjectID, "item": s.BoardNodes[i]["id"],
		"field": ids[field-100], "option": option})
}

// patchItem sets the single-select value the PATCH body names on the BoardNodes item {id}: the
// optionId of its fieldValues node for that field, whose name it clears (the fake knows no
// option names). It answers the item.
func (s *Server) patchItem(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	field, option, ok := s.itemFieldUpdate(body)
	id, err := strconv.Atoi(r.PathValue("id"))
	i, ids := id-firstItemID, s.fieldNodeIDs()
	if err != nil || !ok || i < 0 || i >= len(s.BoardNodes) || field-100 < 0 || field-100 >= len(ids) {
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	n, fieldID := s.BoardNodes[i], ids[field-100]
	fv, _ := n["fieldValues"].(map[string]any)
	if fv == nil {
		fv = map[string]any{}
		n["fieldValues"] = fv
	}
	var nodes []any
	found := false
	for _, v := range nodesOf(fv) {
		if f, _ := v["field"].(map[string]any); f != nil && f["id"] == fieldID {
			v = map[string]any{"optionId": option, "name": "", "field": f}
			found = true
		}
		nodes = append(nodes, v)
	}
	if !found {
		nodes = append(nodes, map[string]any{"optionId": option, "name": "", "field": map[string]any{"id": fieldID}})
	}
	fv["nodes"] = nodes
	writeJSON(w, http.StatusOK, s.restItem(i, n, ids, s.wantFields(r)))
}

// addItem adds a BoardNodes item "item-<node_id>" for the Issues entry the body {"type":
// "Issue","owner","repo","number"} names, and answers {"id", "node_id"}.
func (s *Server) addItem(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body struct {
		Type, Owner, Repo string
		Number            int
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	node, _ := s.Issues[body.Number]["node_id"].(string)
	if body.Type != "Issue" || node == "" {
		s.t.Errorf("forgetest: POST %s: %+v is no issue of Issues with a node_id", r.URL.Path, body)
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	item := "item-" + node
	s.BoardNodes = append(s.BoardNodes, map[string]any{"id": item, "content": map[string]any{
		"number": body.Number, "repository": map[string]any{"nameWithOwner": body.Owner + "/" + body.Repo}}})
	writeJSON(w, http.StatusCreated, map[string]any{"id": firstItemID + len(s.BoardNodes) - 1, "node_id": item})
}
