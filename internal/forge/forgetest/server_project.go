package forgetest

import (
	"fmt"
	"net/http"
	"slices"
)

// DefaultFieldIDs are the node IDs of the status and priority fields the tests of this
// repository use; a Server without FieldIDs serves them as single-select fields.
var DefaultFieldIDs = []string{"F", "F_prio", "F_status", "FIELD_test", "PF", "PVTSSF_p", "PVTSSF_s", "SF"}

// linkedPRsFieldID is the REST ID of the field "Linked pull requests".
const linkedPRsFieldID = 99

// projectRoutes registers the project and its fields under /users/ and /orgs/.
func (s *Server) projectRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"users", "orgs"} {
		atOrg := kind == "orgs"
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}", s.projectRoute(atOrg, s.project))
		mux.HandleFunc("GET /"+kind+"/{owner}/projectsV2/{number}/fields", s.projectRoute(atOrg, s.fields))
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
