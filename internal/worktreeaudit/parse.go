package worktreeaudit

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/prbody"
	"github.com/CorneeldH/lighthouse/internal/pytext"
)

func itoa(n int) string { return strconv.Itoa(n) }

// truthy is Python's bool() of a decoded JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// object is v as a JSON object, or nil.
func object(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// jsonInt is int() of a JSON number or a decimal string; false for anything else.
func jsonInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= 1<<62 {
			return 0, false
		}
		return int(x), true // int() truncates toward zero
	case string:
		n, err := strconv.Atoi(pytext.Strip(x))
		return n, err == nil
	}
	return 0, false
}

// prRank orders the PRs of one head branch: open weighs heaviest, then merged, then closed. Any
// other state has no rank.
func prRank(state string) (int, bool) {
	switch state {
	case "open":
		return 0, true
	case "merged":
		return 1, true
	case "closed":
		return 2, true
	}
	return 0, false
}

// ParsePulls keys the pull or merge requests on their head branch. A set MergedAt is merged,
// whatever the state says. Per head the heaviest state wins, then the highest number. A change
// without a head is skipped; a state outside open, merged and closed is an error: skipping an
// open PR could let a merged one win.
func ParsePulls(changes []forge.Change) (map[string]PullInfo, error) {
	best := map[string]PullInfo{}
	for _, ch := range changes {
		if ch.HeadRef == "" {
			continue
		}
		info := PullInfo{Number: ch.Number, State: ch.State}
		if !ch.MergedAt.IsZero() {
			info.State = "merged"
			info.MergedAt = ptr(float64(ch.MergedAt.UnixMicro()) / 1e6)
		}
		rank, known := prRank(info.State)
		if !known {
			return nil, fmt.Errorf("change #%d has an unknown state %q", ch.Number, info.State)
		}
		cur, seen := best[ch.HeadRef]
		curRank, _ := prRank(cur.State)
		if !seen || rank < curRank || (rank == curRank && ch.Number > cur.Number) {
			best[ch.HeadRef] = info
		}
	}
	return best, nil
}

// pyStr is str() of a decoded JSON scalar.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case string:
		return x
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e16 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return ""
}

// unmeasurablePID stands for a pid that cannot be read as a process id: PidAlive gives nil for
// it, so the session counts as live and its row blocks.
const unmeasurablePID = -1

// sessionPID is int(pid) of a truthy JSON value. `true` is 1, as in Python. A pid Python could
// not signal (beyond an int) or could not read at all (a non-decimal string, where Python stops
// the whole run) becomes unmeasurablePID: kept and blocking, never dropped.
func sessionPID(v any) int {
	if b, ok := v.(bool); ok && b {
		return 1
	}
	if pid, ok := jsonInt(v); ok {
		return pid
	}
	return unmeasurablePID
}

// ParseSessions keys running sessions on their cwd; a record without a cwd, pid or session id is
// skipped, and a later record for the same cwd wins. Whatever the type of the pid and the session
// id, a record that has them is kept: a dropped session would let its worktree read as unused.
func ParseSessions(records []any) map[string]SessionInfo {
	out := map[string]SessionInfo{}
	for _, item := range records {
		record := object(item)
		cwd, _ := record["cwd"].(string)
		if cwd == "" || !truthy(record["sessionId"]) || !truthy(record["pid"]) {
			continue
		}
		sid := pyStr(record["sessionId"])
		if _, isScalar := record["sessionId"].(string); !isScalar && sid == "" {
			sid = fmt.Sprint(record["sessionId"]) // a list or object: str() of it, roughly
		}
		out[cwd] = SessionInfo{PID: sessionPID(record["pid"]), SessionID: sid, Cwd: cwd}
	}
	return out
}

// parseTipShape reads `git rev-list -1 --timestamp --parents <ref>`: `<epoch> <sha> [<parent>…]`.
// Two fields is a root commit. A failed call (nil) or another shape measures nothing.
func parseTipShape(out *string) (*float64, *bool) {
	if out == nil || pytext.Strip(*out) == "" {
		return nil, nil
	}
	first, _, _ := strings.Cut(*out, "\n")
	fields := pytext.Fields(first)
	if len(fields) < 2 || !allDigits(fields[0]) {
		return nil, nil
	}
	n, ok := pyDigits(fields[0])
	if !ok {
		return nil, nil
	}
	return ptr(float64(n)), ptr(len(fields) == 2)
}

// allDigits is str.isdigit for the decimal digits git can print.
func allDigits(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Nd, r) {
			return false
		}
	}
	return s != ""
}

// ParseIssuePRHeads is the head branches of the merged pull or merge requests of issue nr in
// repo slug, each once, in the order of changes: one that closes the issue, and one that mentions
// it with a `Refs #nr` line in its body (prbody.RefsTargets). One from another repository, or
// with its head in a fork, never counts. The repository compares without case.
func ParseIssuePRHeads(changes []forge.IssueChange, slug string, nr int) []string {
	heads := []string{}
	for _, c := range changes {
		switch {
		case c.State != "MERGED", c.HeadRef == "", c.CrossRepo, !strings.EqualFold(c.Repo, slug):
			continue
		case !c.Closes && !slices.Contains(prbody.RefsTargets(c.Body), nr):
			continue
		case !slices.Contains(heads, c.HeadRef):
			heads = append(heads, c.HeadRef)
		}
	}
	return heads
}
