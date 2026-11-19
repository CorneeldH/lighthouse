package worktreeaudit

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
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

// epoch is datetime.fromisoformat(stamp).timestamp() for the extended ISO forms: a date, a time
// after any one separator (`HH[:MM[:SS[.f]]]`) and an offset (`Z`, `±HH[:MM[:SS]]`, `±HHMM`). A
// stamp without an offset is local time. Anything else, and a value that is no string, is nil.
func epoch(stamp any) *float64 {
	s, ok := stamp.(string)
	if !ok || s == "" {
		return nil
	}
	t, ok := parseISO(s)
	if !ok {
		return nil
	}
	return ptr(float64(t.UnixMicro()) / 1e6) // whole microseconds over 1e6, as timedelta.total_seconds
}

func fixedDigits(s string, n int) (int, bool) {
	if len(s) != n {
		return 0, false
	}
	v := 0
	for i := 0; i < n; i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
		v = v*10 + int(s[i]-'0')
	}
	return v, true
}

func parseISO(s string) (time.Time, bool) {
	if len(s) < 10 || s[4] != '-' || s[7] != '-' {
		return time.Time{}, false
	}
	year, ok1 := fixedDigits(s[0:4], 4)
	month, ok2 := fixedDigits(s[5:7], 2)
	day, ok3 := fixedDigits(s[8:10], 2)
	if !ok1 || !ok2 || !ok3 || year < 1 || month < 1 || month > 12 || day < 1 || day > daysIn(year, month) {
		return time.Time{}, false
	}
	loc := time.Local
	var hour, minute, second, nanos int
	rest := s[10:]
	if rest != "" {
		_, size := firstRune(rest)
		clock := rest[size:]
		if i := strings.IndexAny(clock, "Z+-"); i >= 0 {
			var ok bool
			if loc, ok = parseOffset(clock[i:]); !ok {
				return time.Time{}, false
			}
			clock = clock[:i]
		}
		var ok bool
		if hour, minute, second, nanos, ok = parseClock(clock); !ok {
			return time.Time{}, false
		}
	}
	return time.Date(year, time.Month(month), day, hour, minute, second, nanos, loc), true
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

func daysIn(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// parseClock is `HH[:MM[:SS[.f]]]` (a comma also starts the fraction); the fraction keeps
// microseconds.
func parseClock(clock string) (h, m, sec, nanos int, ok bool) {
	main, frac, hasFrac := strings.Cut(strings.Replace(clock, ",", ".", 1), ".")
	parts := strings.Split(main, ":")
	if len(parts) > 3 || (hasFrac && len(parts) != 3) {
		return 0, 0, 0, 0, false
	}
	vals := [3]int{}
	for i, p := range parts {
		v, good := fixedDigits(p, 2)
		if !good {
			return 0, 0, 0, 0, false
		}
		vals[i] = v
	}
	if vals[0] > 23 || vals[1] > 59 || vals[2] > 59 {
		return 0, 0, 0, 0, false
	}
	if hasFrac {
		if frac == "" || len(frac) > 9 {
			return 0, 0, 0, 0, false
		}
		micro := frac + "000000"
		v, good := fixedDigits(micro[:6], 6)
		if _, all := fixedDigits(frac, len(frac)); !good || !all {
			return 0, 0, 0, 0, false
		}
		nanos = v * 1000
	}
	return vals[0], vals[1], vals[2], nanos, true
}

func parseOffset(tz string) (*time.Location, bool) {
	if tz == "Z" {
		return time.UTC, true
	}
	sign := 1
	if tz[0] == '-' {
		sign = -1
	}
	body := tz[1:]
	if len(body) == 4 && !strings.Contains(body, ":") {
		body = body[:2] + ":" + body[2:]
	}
	parts := strings.Split(body, ":")
	if len(parts) > 3 {
		return nil, false
	}
	secs := 0
	for i, p := range parts {
		v, ok := fixedDigits(p, 2)
		if !ok || (i > 0 && v > 59) {
			return nil, false
		}
		secs = secs*60 + v
	}
	for i := len(parts); i < 3; i++ {
		secs *= 60
	}
	if secs >= 24*3600 {
		return nil, false
	}
	return time.FixedZone("", sign*secs), true
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
