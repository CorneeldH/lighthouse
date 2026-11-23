package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ghClient reads GitHub; the real one runs `gh api`, a test supplies a fake.
type ghClient interface {
	// OpenLoopIssues maps the open issues (no PRs) with the label to their milestone title ("": none).
	OpenLoopIssues(repo string) (map[int]string, error)
	// ClosedLoopIssues is the set of `loop` issues of repo that PR pr closes.
	ClosedLoopIssues(repo string, pr int) (map[int]bool, error)
	// TouchedFiles is the set of paths PR pr changes.
	TouchedFiles(repo string, pr int) (map[string]bool, error)
}

type ghCLI struct{}

func ghAPI(args ...string) (string, error) {
	cmd := exec.Command("gh", append([]string{"api"}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh api %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (ghCLI) OpenLoopIssues(repo string) (map[int]string, error) {
	out, err := ghAPI(fmt.Sprintf("repos/%s/issues?labels=%s&state=open&per_page=100", repo, label), "--paginate",
		"--jq", `.[] | select(.pull_request | not) | "\(.number)\t\(.milestone.title // "")"`)
	if err != nil {
		return nil, err
	}
	return parseIssueLines(out)
}

func parseIssueLines(out string) (map[int]string, error) {
	issues := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		number, title, _ := strings.Cut(line, "\t")
		nr, err := strconv.Atoi(strings.TrimSpace(number))
		if err != nil {
			return nil, fmt.Errorf("unexpected gh issue line %q", line)
		}
		issues[nr] = title
	}
	return issues, nil
}

const closingQuery = "query($owner: String!, $name: String!, $pr: Int!) { repository(owner: $owner, name: $name) " +
	"{ pullRequest(number: $pr) { closingIssuesReferences(first: 100) " +
	"{ nodes { number repository { nameWithOwner } labels(first: 50) { nodes { name } } } } } } }"

func (ghCLI) ClosedLoopIssues(repo string, pr int) (map[int]bool, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("repository %q is not owner/name", repo)
	}
	out, err := ghAPI("graphql", "-f", "query="+closingQuery, "-f", "owner="+owner, "-f", "name="+name,
		"-F", "pr="+strconv.Itoa(pr))
	if err != nil {
		return nil, err
	}
	return parseClosing(out, repo, pr)
}

// parseClosing keeps the issues of repo with the label from a graphql reply; a reply without
// closingIssuesReferences (a null pull request, a missing key) is an error.
func parseClosing(reply, repo string, pr int) (map[int]bool, error) {
	var r struct {
		Data struct {
			Repository *struct {
				PullRequest *struct {
					Closing *struct {
						Nodes *[]struct {
							Number     int `json:"number"`
							Repository struct {
								NameWithOwner string `json:"nameWithOwner"`
							} `json:"repository"`
							Labels struct {
								Nodes []struct {
									Name string `json:"name"`
								} `json:"nodes"`
							} `json:"labels"`
						} `json:"nodes"`
					} `json:"closingIssuesReferences"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(reply), &r); err != nil {
		return nil, fmt.Errorf("unexpected gh graphql reply for PR #%d: %w", pr, err)
	}
	repoNode := r.Data.Repository
	if repoNode == nil || repoNode.PullRequest == nil || repoNode.PullRequest.Closing == nil || repoNode.PullRequest.Closing.Nodes == nil {
		return nil, fmt.Errorf("unexpected gh graphql reply for PR #%d: no closingIssuesReferences", pr)
	}
	closed := map[int]bool{}
	for _, n := range *repoNode.PullRequest.Closing.Nodes {
		if !strings.EqualFold(n.Repository.NameWithOwner, repo) {
			continue
		}
		for _, l := range n.Labels.Nodes {
			if l.Name == label {
				closed[n.Number] = true
			}
		}
	}
	return closed, nil
}

func (ghCLI) TouchedFiles(repo string, pr int) (map[string]bool, error) {
	out, err := ghAPI(fmt.Sprintf("repos/%s/pulls/%d/files", repo, pr), "--paginate", "--jq", ".[].filename")
	if err != nil {
		return nil, err
	}
	files := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if line != "" {
			files[line] = true
		}
	}
	return files, nil
}
