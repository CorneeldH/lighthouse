package bashwrites

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// epochRE reads `%gd` of the reflog, `HEAD@{<epoch>}` with --date=unix.
var epochRE = regexp.MustCompile(`HEAD@\{(\d+)\}`)

// splitFields splits on NUL and drops the empty field after the last separator.
func splitFields(data []byte) [][]byte {
	fields := bytes.Split(data, []byte{0})
	if n := len(fields); n > 0 && len(fields[n-1]) == 0 {
		fields = fields[:n-1]
	}
	return fields
}

// ParsePorcelain turns `git status --porcelain -z -uall` into (changed, deleted). A record is
// `XY path\0`; an R or C in X or Y is followed by one more field with the old path. A D means
// deleted. The old path of an R counts as deleted; the old path of a C still exists and is
// skipped. Paths stay bytes: a name with a newline or non-ASCII bytes must not fall apart.
func ParsePorcelain(data []byte) (changed, deleted map[string]bool) {
	changed, deleted = map[string]bool{}, map[string]bool{}
	fields := splitFields(data)
	for i := 0; i < len(fields); i++ {
		field := fields[i]
		if len(field) < 3 {
			continue
		}
		status, path := field[:2], string(field[3:])
		if bytes.ContainsAny(status, "RC") && i+1 < len(fields) {
			i++
			if bytes.Contains(status, []byte("R")) {
				deleted[string(fields[i])] = true
			}
		}
		if bytes.Contains(status, []byte("D")) {
			deleted[path] = true
		} else {
			changed[path] = true
		}
	}
	return changed, deleted
}

// ParseDiffTree turns `diff-tree --name-status -z` into (changed, deleted). Records are
// `status\0path\0`; a combined diff has one status letter per parent, and a D anywhere in it means
// deleted.
func ParseDiffTree(data []byte) (changed, deleted map[string]bool) {
	changed, deleted = map[string]bool{}, map[string]bool{}
	fields := splitFields(data)
	for i := 0; i+1 < len(fields); i += 2 {
		path := string(fields[i+1])
		if bytes.Contains(fields[i], []byte("D")) {
			deleted[path] = true
		} else {
			changed[path] = true
		}
	}
	return changed, deleted
}

// ReflogEntry is one line of `git reflog --format='%H%x1f%gd%x1f%gs'`.
type ReflogEntry struct {
	SHA     string
	Epoch   int64
	Subject string
}

// ParseReflog turns `%H\x1f%gd\x1f%gs` lines into entries, newest first. It splits on "\n" only:
// git keeps other control characters literally in `%gs`. A line without two \x1f is an error.
func ParseReflog(text string) ([]ReflogEntry, error) {
	var entries []ReflogEntry
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 3)
		if len(parts) != 3 {
			return nil, fmt.Errorf("cannot parse reflog line %q", line)
		}
		var epoch int64
		if m := epochRE.FindStringSubmatch(parts[1]); m != nil {
			epoch, _ = strconv.ParseInt(m[1], 10, 64)
		}
		entries = append(entries, ReflogEntry{SHA: parts[0], Epoch: epoch, Subject: parts[2]})
	}
	return entries, nil
}

// Commit is a reflog commit to check; Merge is true for `commit (merge)`.
type Commit struct {
	SHA   string
	Merge bool
}

func isMerge(subject string) bool { return strings.HasPrefix(subject, "commit (merge)") }

// commitsUntilStop walks to the entry with sha == stop. An entry with an empty subject (the first
// entry of a new worktree) is no movement: it sets no notice and hides none.
func commitsUntilStop(entries []ReflogEntry, stop string) ([]Commit, []string) {
	var collected []Commit
	var moved string
	for _, e := range entries {
		if e.SHA == stop {
			if moved != "" {
				return collected, []string{"HEAD moved without a commit (" + moved + ")"}
			}
			return collected, nil
		}
		if strings.HasPrefix(e.Subject, "commit") {
			collected = append(collected, Commit{e.SHA, isMerge(e.Subject)})
		} else if moved == "" && e.Subject != "" {
			moved = e.Subject
		}
	}
	return nil, []string{"reflog gap: ledger HEAD " + stop + " not found"}
}

func commitsFromUnborn(entries []ReflogEntry) []Commit {
	var collected []Commit
	for _, e := range entries {
		if strings.HasPrefix(e.Subject, "commit") {
			collected = append(collected, Commit{e.SHA, isMerge(e.Subject)})
		}
	}
	return collected
}

// commitsSince is the fallback on reflog time: commit entries with epoch >= since, newest first.
func commitsSince(entries []ReflogEntry, since *int64) []Commit {
	var collected []Commit
	for _, e := range entries {
		if since != nil && e.Epoch < *since {
			break
		}
		if strings.HasPrefix(e.Subject, "commit") {
			collected = append(collected, Commit{e.SHA, isMerge(e.Subject)})
		}
	}
	return collected
}

// CommitsToCheck says which reflog commits belong to this walk. With stop set (not "unborn") it
// walks to the first entry with that sha, plus one notice for the first movement that is no
// commit, or a gap notice when stop never occurs. With stop "unborn" every commit entry counts.
// With stop nil it falls back to reflog time (since, whole epoch seconds). With stop set and a
// since, the result is the union of both walks, deduplicated on sha, stop walk first.
func CommitsToCheck(entries []ReflogEntry, stop *string, since *int64) ([]Commit, []string) {
	switch {
	case stop != nil && *stop != "unborn":
		collected, notes := commitsUntilStop(entries, *stop)
		if since == nil {
			return collected, notes
		}
		known := map[string]bool{}
		for _, c := range collected {
			known[c.SHA] = true
		}
		for _, c := range commitsSince(entries, since) {
			if !known[c.SHA] {
				known[c.SHA] = true
				collected = append(collected, c)
			}
		}
		return collected, notes
	case stop != nil:
		return commitsFromUnborn(entries), nil
	default:
		return commitsSince(entries, since), nil
	}
}
