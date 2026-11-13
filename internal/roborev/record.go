package roborev

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

var (
	rangeRe  = regexp.MustCompile(`^[0-9a-f]{7,64}\.\.[0-9a-f]{7,64}$`)
	fixedRe  = regexp.MustCompile(`(?m)^roborev: (\S+?):(\d+) \((\w+)\)\s*$`)
	outcomes = []string{"fixed", "refuted", "left"}
)

// Fixed is a finding named by a `roborev: <path>:<line> (<severity>)` line in a commit body.
type Fixed struct {
	Path     string
	Line     int
	Severity string
	Commit   string
}

// FixedFindings lists the findings fixed in the commits of `<base>..HEAD`, oldest first.
func FixedFindings(root string) ([]Fixed, error) {
	mergeBase, err := base(root)
	if err != nil {
		return nil, err
	}
	log, err := git(root, false, "log", "--reverse", "--format=%x01%H%n%B", mergeBase+"..HEAD")
	if err != nil {
		return nil, err
	}
	fixed := []Fixed{}
	entries := strings.Split(log, "\x01")
	for _, entry := range entries[1:] {
		commit, body, _ := strings.Cut(entry, "\n")
		for _, m := range fixedRe.FindAllStringSubmatch(body, -1) {
			fixed = append(fixed, Fixed{Path: m[1], Line: atoi(m[2]), Severity: m[3], Commit: strings.TrimSpace(commit)})
		}
	}
	return fixed, nil
}

// FixedJSON is the list `fixed` prints.
func FixedJSON(fixed []Fixed) []any {
	out := []any{}
	for _, f := range fixed {
		o := orderedjson.NewObject()
		o.Set("path", f.Path)
		o.Set("line", f.Line)
		o.Set("severity", f.Severity)
		o.Set("commit", f.Commit)
		out = append(out, o)
	}
	return out
}

// truthy is Python truthiness of a JSON value.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case interface{ String() string }: // json.Number
		f, _ := strconv.ParseFloat(x.String(), 64)
		return f != 0
	}
	return true
}

type triple struct {
	path     string
	line     int
	severity string
}

func (t triple) repr() string {
	return fmt.Sprintf("(%s, %d, %s)", pyRepr(t.path), t.line, pyRepr(t.severity))
}

func sortTriples(ts []triple) {
	sort.Slice(ts, func(i, j int) bool {
		a, b := ts[i], ts[j]
		if a.path != b.path {
			return a.path < b.path
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.severity < b.severity
	})
}

func reprTriples(ts []triple) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.repr()
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func numberOf(v any) int {
	if n, ok := v.(interface{ String() string }); ok {
		f, _ := strconv.ParseFloat(n.String(), 64)
		return int(f)
	}
	return 0
}

// validate checks every decision and, with findings, that they match one to one.
func validate(decisions []any, findings []Finding, check bool) ([]any, error) {
	clean := []any{}
	var have []triple
	for _, raw := range decisions {
		d, ok := raw.(*orderedjson.Object)
		if !ok {
			return nil, valuef("decision is not a JSON object: %s", pyRepr(raw))
		}
		get := func(k string) any { v, _ := d.Get(k); return v }
		outcome, commit, reason := get("uitkomst"), get("commit"), get("reden")
		where := pyStr(get("path")) + ":" + pyStr(get("line"))
		if !contains(outcomes, outcome) {
			return nil, valuef("%s: uitkomst must be one of %s, not %s", where, strings.Join(outcomes, ", "), pyRepr(outcome))
		}
		if outcome == "fixed" && (!truthy(commit) || reason != nil) {
			return nil, valuef("%s: fixed needs a commit and reden null", where)
		}
		if outcome != "fixed" && (!truthy(reason) || commit != nil) {
			return nil, valuef("%s: %s needs a reden and commit null", where, outcome)
		}
		row := orderedjson.NewObject()
		for _, k := range []string{"path", "line", "severity", "uitkomst", "commit", "reden"} {
			row.Set(k, get(k))
		}
		clean = append(clean, row)
		path, _ := get("path").(string)
		sev, _ := get("severity").(string)
		have = append(have, triple{path, numberOf(get("line")), sev})
	}
	if check {
		var want []triple
		for _, f := range findings {
			t := triple{severity: f.Severity}
			if f.Path != nil {
				t.path = *f.Path
			}
			if f.Line != nil {
				t.line = *f.Line
			}
			want = append(want, t)
		}
		sortTriples(want)
		sortTriples(have)
		if reprTriples(want) != reprTriples(have) {
			return nil, valuef("decisions do not match the findings one to one: want %s, got %s", reprTriples(want), reprTriples(have))
		}
	}
	return clean, nil
}

func contains(list []string, v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// RecordOptions are the arguments of Record. Out and Raw are "" when not given.
type RecordOptions struct {
	Skip      *string
	Sha       *string
	Decisions []any // *orderedjson.Object each
	Out       string
	Raw       string
	Range     *string
}

// Record writes the state file: a skip with its reason, or done with one decision per finding,
// and returns the path it wrote.
//
// The decisions are checked one to one against the findings of a raw review: Raw when given
// (fix-pr passes the CI comment), else the branch's local <stem>.md, but only without Out: a
// record elsewhere is not about the local review. That raw review must be a `done` with the
// same sha, so the marker never claims a failed or another review. Range is the reviewed range
// as `gate` printed it; without it the state holds `<base>..HEAD` of now.
func Record(root string, o RecordOptions) (string, error) {
	if (o.Skip == nil) == (o.Sha == nil) {
		return "", valuef("give either skip or sha")
	}
	if o.Range != nil && !rangeRe.MatchString(*o.Range) {
		return "", valuef("--range must be <base>..<head> as gate printed it, not %s", pyRepr(*o.Range))
	}
	state, local, err := StatePaths(root)
	if err != nil {
		return "", err
	}
	target, raw := state, o.Raw
	if o.Out != "" {
		target = o.Out
	}
	if raw != "" && !isFile(raw) {
		return "", valuef("%s: no such raw review", raw)
	}
	if raw == "" && o.Out == "" {
		raw = local
	}
	data := orderedjson.NewObject()
	if o.Range != nil {
		data.Set("range", *o.Range)
	} else {
		mergeBase, err := base(root)
		if err != nil {
			return "", err
		}
		head, err := git(root, true, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		data.Set("range", mergeBase+".."+head)
	}
	if o.Skip != nil {
		data.Set("uitkomst", "skip")
		data.Set("reden", *o.Skip)
	} else {
		var found []Finding
		checked := false
		if raw != "" && isFile(raw) {
			text, err := os.ReadFile(raw)
			if err != nil {
				return "", checkf("%s: unreadable: %v", raw, err)
			}
			verdict := Classify(string(text))
			if verdict.Verdict != "done" {
				return "", valuef("%s: no review to record as done (%s)", raw, verdict.Reason)
			}
			if verdict.Sha != *o.Sha {
				return "", valuef("--done %s is not the reviewed sha %s", *o.Sha, verdict.Sha)
			}
			found, checked = verdict.Findings, true
		}
		clean, err := validate(o.Decisions, found, checked)
		if err != nil {
			return "", err
		}
		data.Set("uitkomst", "done")
		data.Set("sha", *o.Sha)
		data.Set("bevindingen", clean)
	}
	if err := writeState(target, data); err != nil {
		return "", err
	}
	return target, nil
}

// BodyLine is the line the PR body carries: empty when off, never silent when `local`.
func BodyLine(root string) (string, error) {
	setting, _, err := settings(root)
	if err != nil {
		return "", err
	}
	if setting != "local" {
		return "", nil
	}
	state, _, err := StatePaths(root)
	if err != nil {
		return "", err
	}
	recorded, err := readState(state)
	if err != nil {
		return "", err
	}
	if recorded == nil {
		return "Roborev overgeslagen: geen reviewuitkomst vastgelegd", nil
	}
	if verdict, _ := recorded.Get("verdict"); verdict == "done" {
		sha, _ := recorded.Get("sha")
		return fmt.Sprintf("<!-- roborev-lokaal: %s -->", pyStr(sha)), nil
	}
	reason, _ := recorded.Get("reason")
	return "Roborev overgeslagen: " + pyStr(reason), nil
}
