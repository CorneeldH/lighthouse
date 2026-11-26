package review

import (
	"regexp"
	"strconv"
)

// ServedRepo is a repo the host serves: its slug and a local clone.
type ServedRepo struct{ Slug, Dir string }

var chainIssueRe = regexp.MustCompile(`issue-(\d+)`)

// hasCommit reports whether the clone in dir contains the commit sha. A var so that tests can
// replace the git call.
var hasCommit = func(dir, sha string) bool {
	_, err := Git(dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// Trace returns the trajectory of a record without one: the issue from "issue-<n>" in the
// chain_key, else issue_spec.issue; the repo is the only served repo whose clone contains
// reviewed_sha. ok is false when the issue or the repo is not unambiguous.
func Trace(chainKey string, issueSpec *int, reviewedSHA string, repos []ServedRepo, hasCommit func(dir, sha string) bool) (Trajectory, bool) {
	issue := 0
	for _, m := range chainIssueRe.FindAllStringSubmatch(chainKey, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || (issue != 0 && n != issue) {
			return Trajectory{}, false
		}
		issue = n
	}
	if issue == 0 && issueSpec != nil {
		issue = *issueSpec
	}
	if issue < 1 || reviewedSHA == "" {
		return Trajectory{}, false
	}
	var found *ServedRepo
	for i := range repos {
		if !hasCommit(repos[i].Dir, reviewedSHA) {
			continue
		}
		if found != nil {
			return Trajectory{}, false
		}
		found = &repos[i]
	}
	if found == nil {
		return Trajectory{}, false
	}
	return Trajectory{Repo: found.Slug, Issue: issue}, true
}
