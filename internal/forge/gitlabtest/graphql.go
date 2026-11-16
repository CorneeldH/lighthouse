package gitlabtest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// gqlRequest is a GraphQL request body.
type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// operationName reads the name after "query" or "mutation" at the start of a query.
var operationName = regexp.MustCompile(`^\s*(?:query|mutation)\s+(\w+)`)

// workItemFields are the fields a work item answer holds; a query that does not select one of
// them would get nothing on GitLab, so the fake fails the test instead of answering it.
var workItemFields = []string{
	"id", "iid", "title", "state", "updatedAt", "widgets", "type",
	"WorkItemWidgetStatus", "status", "name",
	"WorkItemWidgetLabels", "labels", "title",
	"WorkItemWidgetLinkedItems", "linkedItems(filter: BLOCKED_BY", "workItem", "namespace", "fullPath",
	"WorkItemWidgetDevelopment", "closingMergeRequests", "mergeRequest",
	"draft", "webUrl", "diffHeadSha", "sourceBranch", "headPipeline", "detailedMergeStatus", "mergedAt", "sourceProjectId", "targetProjectId",
}

// graphql serves the GraphQL operations lighthouse sends, recognised by their operation name.
// Any other operation is an unexpected request.
func (s *Server) graphql(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var req gqlRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		s.unexpected(w, r)
		return
	}
	m := operationName.FindStringSubmatch(req.Query)
	if m == nil {
		s.unexpected(w, r)
		return
	}
	op := m[1]
	if op == "LighthouseSetStatus" {
		s.gqlSetStatus(w, req.Variables)
		return
	}
	if op != "LighthouseBoardItems" && op != "LighthouseIssueItem" && op != "LighthouseWorkItemID" {
		s.unexpected(w, r)
		return
	}
	if p, _ := req.Variables["project"].(string); p != s.Project {
		s.t.Errorf("gitlabtest: GraphQL %s for project %q, want %q", op, p, s.Project)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"project": nil}})
		return
	}
	switch op {
	case "LighthouseBoardItems":
		s.requireFields(op, req.Query, workItemFields)
		s.gqlBoardItems(w, req.Variables)
	case "LighthouseIssueItem":
		s.requireFields(op, req.Query, workItemFields)
		s.gqlByIID(w, req.Variables, s.workItemLocked)
	default: // LighthouseWorkItemID
		s.gqlByIID(w, req.Variables, func(it map[string]any) map[string]any { return map[string]any{"id": gid(it)} })
	}
}

// requireFields fails the test for a query that does not name every field the answer holds.
func (s *Server) requireFields(op, query string, fields []string) {
	for _, f := range fields {
		if !strings.Contains(query, f) {
			s.t.Errorf("gitlabtest: GraphQL %s does not select %q", op, f)
		}
	}
}

// gqlBoardItems serves one page of 100 issue work items, filtered on state (opened or closed;
// anything else fails the test) and on updatedAfter. after is the offset of the page.
func (s *Server) gqlBoardItems(w http.ResponseWriter, vars map[string]any) {
	state, _ := vars["state"].(string)
	want := map[string]string{"opened": "opened", "closed": "closed"}[state]
	if want == "" {
		s.t.Errorf("gitlabtest: LighthouseBoardItems state %q, want opened or closed", state)
	}
	var after time.Time
	if a, ok := vars["updatedAfter"].(string); ok {
		after, _ = time.Parse(time.RFC3339, a)
	}
	offset := 0
	if c, ok := vars["after"].(string); ok {
		offset, _ = strconv.Atoi(c)
	}
	s.mu.Lock()
	var nodes []any
	for _, iid := range sortedKeys(s.issues) {
		it := s.issues[iid]
		if it["state"] != want {
			continue
		}
		if at, _ := time.Parse(time.RFC3339, fmt.Sprint(it["updated_at"])); !after.IsZero() && at.Before(after) {
			continue
		}
		nodes = append(nodes, s.workItemLocked(it))
	}
	s.mu.Unlock()
	offset = min(offset, len(nodes))
	end := min(offset+100, len(nodes))
	page := nodes[offset:end]
	if page == nil {
		page = []any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"project": map[string]any{"workItems": map[string]any{
		"pageInfo": map[string]any{"hasNextPage": end < len(nodes), "endCursor": strconv.Itoa(end)},
		"nodes":    page,
	}}}})
}

// gqlByIID serves the work item with variable iid, as answer makes it, or no node.
func (s *Server) gqlByIID(w http.ResponseWriter, vars map[string]any, answer func(map[string]any) map[string]any) {
	iid, _ := strconv.Atoi(fmt.Sprint(vars["iid"]))
	s.mu.Lock()
	nodes := []any{}
	if it, ok := s.issues[iid]; ok {
		nodes = append(nodes, answer(it))
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"project": map[string]any{"workItems": map[string]any{
		"nodes": nodes,
	}}}})
}

// gqlSetStatus sets the status of the work item with global ID id to the lifecycle status named
// status, case-insensitively as GitLab does. An id that is no gid://gitlab/WorkItem/ of a known
// issue fails the test; a name outside the lifecycle answers an error in the payload.
func (s *Server) gqlSetStatus(w http.ResponseWriter, vars map[string]any) {
	id, _ := vars["id"].(string)
	name, _ := vars["status"].(string)
	s.mu.Lock()
	defer s.mu.Unlock()
	iid := 0
	for n, it := range s.issues {
		if gid(it) == id {
			iid = n
		}
	}
	if iid == 0 {
		s.t.Errorf("gitlabtest: workItemUpdate of %q, which is no gid://gitlab/WorkItem/ of a known issue", id)
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"workItemUpdate": nil}, "errors": []any{map[string]any{
			"message": "The resource that you are attempting to access does not exist or you don't have permission to perform this action",
		}}})
		return
	}
	i := slices.IndexFunc(s.statuses, func(st string) bool { return strings.EqualFold(st, name) })
	if i < 0 {
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"workItemUpdate": map[string]any{
			"errors": []string{fmt.Sprintf("Status '%s' not found", name)}, "workItem": nil,
		}}})
		return
	}
	s.status[iid] = s.statuses[i]
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"workItemUpdate": map[string]any{
		"errors": []string{}, "workItem": map[string]any{"id": id},
	}}})
}

// gid is the global work item ID of an issue: its id, not its iid.
func gid(it map[string]any) string { return fmt.Sprintf("gid://gitlab/WorkItem/%d", toInt(it["id"])) }

// workItemLocked answers an issue as the work item GitLab's GraphQL returns, with the status,
// labels, linked items (only is_blocked_by, as the filter asks) and development widgets. The
// caller holds mu.
func (s *Server) workItemLocked(it map[string]any) map[string]any {
	iid := toInt(it["iid"])
	var status any
	if name := s.status[iid]; name != "" {
		status = map[string]any{"name": name}
	}
	labels := []any{}
	switch ls := it["labels"].(type) {
	case []string:
		for _, l := range ls {
			labels = append(labels, map[string]any{"title": l})
		}
	case []any:
		for _, l := range ls {
			labels = append(labels, map[string]any{"title": l})
		}
	}
	blockers := []any{}
	for _, l := range s.links[iid] {
		if l["link_type"] != "is_blocked_by" {
			continue
		}
		refs, _ := l["references"].(map[string]any)
		project, _, _ := strings.Cut(fmt.Sprint(refs["full"]), "#")
		var closing []any
		for _, mr := range s.closedBy[fmt.Sprintf("%s#%d", project, toInt(l["iid"]))] {
			closing = append(closing, map[string]any{"mergeRequest": map[string]any{"state": mr["state"]}})
		}
		blockers = append(blockers, map[string]any{"workItem": map[string]any{
			"iid": strconv.Itoa(toInt(l["iid"])), "state": workItemState(l["state"]),
			"namespace": map[string]any{"fullPath": project},
			"widgets":   []any{map[string]any{"type": "DEVELOPMENT", "closingMergeRequests": connection(closing)}},
		}})
	}
	var closing []any
	for _, mr := range s.closedBy[fmt.Sprintf("%s#%d", s.Project, iid)] {
		closing = append(closing, map[string]any{"mergeRequest": gqlMR(mr)})
	}
	return map[string]any{
		"id": gid(it), "iid": strconv.Itoa(iid), "title": it["title"], "state": workItemState(it["state"]),
		"updatedAt": it["updated_at"],
		"widgets": []any{
			map[string]any{"type": "STATUS", "status": status},
			map[string]any{"type": "LABELS", "labels": connection(labels)},
			map[string]any{"type": "LINKED_ITEMS", "linkedItems": connection(blockers)},
			map[string]any{"type": "DEVELOPMENT", "closingMergeRequests": connection(closing)},
			map[string]any{"type": "DESCRIPTION"}, // a widget the client does not read
		},
	}
}

func connection(nodes []any) map[string]any {
	if nodes == nil {
		nodes = []any{}
	}
	return map[string]any{"pageInfo": map[string]any{"hasNextPage": false}, "nodes": nodes}
}

// workItemState is the WorkItemState enum of a REST issue state.
func workItemState(rest any) string {
	if rest == "closed" {
		return "CLOSED"
	}
	return "OPEN"
}

// gqlMR answers a merge request seeded in GitLab's REST shape in its GraphQL shape: camelCase,
// iid a string, the pipeline and merge status as upper-case enum values.
func gqlMR(m map[string]any) map[string]any {
	out := map[string]any{
		"iid": strconv.Itoa(toInt(m["iid"])), "state": m["state"], "draft": m["draft"] == true, "webUrl": m["web_url"],
		"diffHeadSha":  m["sha"],
		"sourceBranch": m["source_branch"], "mergedAt": m["merged_at"],
		"sourceProjectId": m["source_project_id"], "targetProjectId": m["target_project_id"],
		"detailedMergeStatus": nil, "headPipeline": nil,
	}
	if s, ok := m["detailed_merge_status"].(string); ok {
		out["detailedMergeStatus"] = strings.ToUpper(s)
	}
	if p, ok := m["head_pipeline"].(map[string]any); ok {
		out["headPipeline"] = map[string]any{"status": strings.ToUpper(fmt.Sprint(p["status"]))}
	}
	return out
}
