package forgetest

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// placeholderMergedAt is the merged_at of a MERGED node without mergedAt or closedAt: REST
// tells a merged pull request from a closed one only by merged_at.
const placeholderMergedAt = "2000-01-01T00:00:00Z"

// pullRoutes registers the routes that serve the PR nodes of Branches and BoardNodes as REST:
// the checks of a commit. The list by head and one pull request go through pulls and pull.
func (s *Server) pullRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /repos/{owner}/{name}/commits/{sha}/check-runs", s.checkRuns)
	mux.HandleFunc("GET /repos/{owner}/{name}/commits/{sha}/status", s.commitStatus)
	mux.HandleFunc("GET /repos/{owner}/{name}/commits/{sha}/check-suites", s.checkSuites)
}

// branchPulls serves GET .../pulls?head=<owner>:<branch>: the nodes of Branches[branch],
// translated, filtered on state, in pages. It counts "/repos/{owner}/{name}/pulls?head=<branch>".
// The caller holds s.mu.
func (s *Server) branchPulls(w http.ResponseWriter, r *http.Request, head string) {
	owner, branch, ok := strings.Cut(head, ":")
	if !ok || owner != r.PathValue("owner") {
		s.t.Errorf("forgetest: pulls head = %q, want %s:<branch>", head, r.PathValue("owner"))
	}
	s.counts[r.URL.Path+"?head="+branch]++
	state := cmp.Or(r.URL.Query().Get("state"), "open")
	repo := r.PathValue("owner") + "/" + r.PathValue("name")
	var out []any
	for _, n := range s.Branches[branch] {
		p := restPull(n, repo)
		if state != "all" && state != p["state"] {
			continue
		}
		out = append(out, p)
	}
	s.page(w, r, out)
}

// prNodes is every PR node of Branches (by branch name) and of the closing PRs in BoardNodes.
// The caller holds s.mu.
func (s *Server) prNodes() []map[string]any {
	var out []map[string]any
	branches := make([]string, 0, len(s.Branches))
	for b := range s.Branches {
		branches = append(branches, b)
	}
	slices.Sort(branches)
	for _, b := range branches {
		out = append(out, s.Branches[b]...)
	}
	for _, n := range s.BoardNodes {
		content, _ := n["content"].(map[string]any)
		closedBy, _ := content["closedByPullRequestsReferences"].(map[string]any)
		switch nodes := closedBy["nodes"].(type) {
		case []any:
			for _, p := range nodes {
				if m, ok := p.(map[string]any); ok {
					out = append(out, m)
				}
			}
		case []map[string]any:
			out = append(out, nodes...)
		}
	}
	return out
}

// nodePull is the PR node with number n in REST shape with mergeable, as GET .../pulls/{n}
// answers it; nil when no node has that number. The caller holds s.mu.
func (s *Server) nodePull(n int64, repo string) map[string]any {
	for _, node := range s.prNodes() {
		if commentID(node["number"]) == n {
			p := restPull(node, repo)
			switch node["mergeable"] {
			case "MERGEABLE":
				p["mergeable"] = true
			case "CONFLICTING":
				p["mergeable"] = false
			default:
				p["mergeable"] = nil
			}
			return p
		}
	}
	return nil
}

// restPull translates a PR node in GraphQL shape into the REST shape of the pulls list, in
// repo. MERGED and CLOSED are state closed; a MERGED node without mergedAt gets merged_at from
// closedAt, else placeholderMergedAt. isCrossRepository puts the head in fork/<name>.
func restPull(n map[string]any, repo string) map[string]any {
	state := "open"
	mergedAt := n["mergedAt"]
	switch n["state"] {
	case "MERGED":
		state = "closed"
		if mergedAt == nil {
			mergedAt = n["closedAt"]
		}
		if mergedAt == nil {
			mergedAt = placeholderMergedAt
		}
	case "CLOSED":
		state = "closed"
	}
	headRepo := repo
	if cross, _ := n["isCrossRepository"].(bool); cross {
		_, name, _ := strings.Cut(repo, "/")
		headRepo = "fork/" + name
	}
	draft, _ := n["isDraft"].(bool)
	return map[string]any{
		"number": n["number"], "state": state, "draft": draft, "merged_at": mergedAt, "closed_at": n["closedAt"],
		"html_url": n["url"],
		"head":     map[string]any{"sha": n["headRefOid"], "ref": n["headRefName"], "repo": map[string]any{"full_name": headRepo}},
		"base":     map[string]any{"ref": n["baseRefName"], "repo": map[string]any{"full_name": repo}},
	}
}

// rollupOf is the statusCheckRollup.state of the PR nodes whose headRefOid is sha, "" when
// none has one. Two nodes that disagree are a fixture error.
func (s *Server) rollupOf(sha string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := ""
	for _, n := range s.prNodes() {
		if n["headRefOid"] != sha {
			continue
		}
		roll, _ := n["statusCheckRollup"].(map[string]any)
		st, _ := roll["state"].(string)
		if st == "" {
			continue
		}
		if state != "" && state != st {
			s.t.Errorf("forgetest: head %s has statusCheckRollup %s and %s", sha, state, st)
		}
		state = st
	}
	return state
}

// checkLists are the check runs, statuses and suites a statusCheckRollup.state stands for:
// SUCCESS one successful run in a completed suite, FAILURE one failed run, ERROR one status
// error, PENDING and EXPECTED one run in progress in a suite in progress.
func (s *Server) checkLists(sha string) (runs, statuses, suites []any) {
	run := func(status string, conclusion any) any {
		return map[string]any{"id": 1, "name": "ci", "status": status, "conclusion": conclusion,
			"app": map[string]any{"slug": "github-actions"}}
	}
	suite := func(status string) any {
		return map[string]any{"status": status, "latest_check_runs_count": 1, "app": map[string]any{"slug": "github-actions"}}
	}
	switch st := s.rollupOf(sha); st {
	case "":
	case "SUCCESS":
		runs, suites = []any{run("completed", "success")}, []any{suite("completed")}
	case "FAILURE":
		runs = []any{run("completed", "failure")}
	case "ERROR":
		statuses = []any{map[string]any{"state": "error", "context": "ci"}}
	case "PENDING", "EXPECTED":
		runs, suites = []any{run("in_progress", nil)}, []any{suite("in_progress")}
	default:
		s.t.Errorf("forgetest: head %s: statusCheckRollup state %q is not served", sha, st)
	}
	return runs, statuses, suites
}

// counted serves items under key with total_count, in pages, as the check lists come.
func (s *Server) counted(w http.ResponseWriter, r *http.Request, key string, items []any, extra map[string]any) {
	s.pageAs(w, r, items, func(p []any) any {
		out := map[string]any{"total_count": len(items), key: p}
		for k, v := range extra {
			out[k] = v
		}
		return out
	})
}

func (s *Server) checkRuns(w http.ResponseWriter, r *http.Request) {
	runs, _, _ := s.checkLists(r.PathValue("sha"))
	s.counted(w, r, "check_runs", runs, nil)
}

// commitStatus serves the combined status. Without statuses its state is "pending", as GitHub
// answers it.
func (s *Server) commitStatus(w http.ResponseWriter, r *http.Request) {
	_, statuses, _ := s.checkLists(r.PathValue("sha"))
	state := "pending"
	if len(statuses) > 0 {
		state = fmt.Sprint(statuses[0].(map[string]any)["state"])
	}
	s.counted(w, r, "statuses", statuses, map[string]any{"state": state, "sha": r.PathValue("sha")})
}

func (s *Server) checkSuites(w http.ResponseWriter, r *http.Request) {
	_, _, suites := s.checkLists(r.PathValue("sha"))
	s.counted(w, r, "check_suites", suites, nil)
}
