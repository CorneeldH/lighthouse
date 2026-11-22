package checkpointscan

import (
	"slices"
	"strings"
)

// RemoteExclusion returns the rev-list arguments for what a push to remote need not send: its
// remote-tracking refs, `--remotes=<remote>`. Those follow the fetch URL, so only for a
// configured remote whose push goes to that one URL and nowhere else; else none, and a scan
// reads more, never less. One rule for the hook and `scan`.
func RemoteExclusion(repo, remote string) ([]string, error) {
	names, err := git(repo, nil, "remote")
	if err != nil {
		return nil, err
	}
	if !slices.Contains(lines(names), remote) {
		return nil, nil
	}
	fetch, err := git(repo, nil, "remote", "get-url", "--all", remote)
	if err != nil {
		return nil, nil //nolint:nilerr // unreadable URLs: exclude nothing, scan more
	}
	push, err := git(repo, nil, "remote", "get-url", "--push", "--all", remote)
	if err != nil {
		return nil, nil //nolint:nilerr // see above
	}
	f := lines(fetch)
	if len(f) == 1 && slices.Equal(lines(push), f) {
		return []string{"--remotes=" + remote}, nil
	}
	return nil, nil
}

// lines splits like Python's str.splitlines for git output: no trailing empty element.
func lines(s string) []string {
	if s == "" {
		return nil
	}
	return splitLines(s)
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
