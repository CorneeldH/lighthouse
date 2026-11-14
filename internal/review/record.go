package review

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/datadir"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// SchemaVersion is the schema_version of the record that Write stores.
const SchemaVersion = 5

// DefaultSkillDirs is the skill directory per kind, relative to the repo root.
var DefaultSkillDirs = map[string]string{
	"plan": ".claude/skills/lighthouse-review-plan",
	"spec": ".claude/skills/lighthouse-review-spec",
}

// Kinds are the two reviews that share one record format.
var Kinds = []string{"plan", "spec"}

var (
	decisionsByKind = map[string][]string{
		"plan": {"accept", "reject", "spec", "accepted_open", "minor_yes", "minor_no"},
		"spec": {"accept", "reject", "defer", "accepted_open", "minor_yes", "minor_no"},
	}
	// closedByKind are the decisions that close a BLOCKER/MAJOR finding.
	closedByKind = map[string][]string{"plan": {"reject"}, "spec": {"reject", "defer"}}
	sourceNames  = []string{"codex", "fallback", "both"}
	severities   = []string{"BLOCKER", "MAJOR", "MINOR"}
	budgetFields = []string{"decision", "reason", "session_pct", "week_pct", "source", "thresholds"}
	// pairFiles are the files of the paired round-1 experiment that Write takes along.
	pairFiles = []string{
		"pair.json", "round-1.codex.json", "round-1.fallback.json",
		"round-1.blind-decisions.json", "round-1.sources.json",
	}
	roundFile  = regexp.MustCompile(`^round-(\d+)\.json$`)
	budgetFile = regexp.MustCompile(`^round-(\d+)\..+\.budget\.json$`)
	codexModel = regexp.MustCompile(`^(model|model_reasoning_effort)\s*=\s*"([^"]*)"`)
)

// InvalidError is a problem with the input that the operator can fix: the exit-2 class of
// Start, as ValueError was in the script.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

// Deps are the seams of the record: the host judgment, the Codex version and the clock.
type Deps struct {
	HostRefusal  func(kind string) *string
	CodexVersion func() *string
	Now          func() time.Time
}

// DefaultDeps is the production wiring.
func DefaultDeps() Deps {
	return Deps{HostRefusal: HostRefusal, CodexVersion: CodexVersion, Now: time.Now}
}

// HostRefusal is nil on the controller host: host.controller is true, or without it host.tailscale_ip is on this machine (host.toml), otherwise the stderr line that refuses the
// measurement.
func HostRefusal(kind string) *string { return hostRefusalWith(kind, config.Recognize) }

// hostRefusalWith is HostRefusal with the recognition of the host as a parameter.
func hostRefusalWith(kind string, recognize func() (bool, error)) *string {
	var cause string
	local, err := recognize()
	switch {
	case err != nil:
		cause = err.Error()
	case local:
		return nil
	default:
		cause = "not on the host from host.toml"
	}
	msg := fmt.Sprintf("%s-REVIEW NOT RECORDED: %s — this chain falls outside "+
		"the measurement. The review itself is valid; carry on.", strings.ToUpper(kind), cause)
	return &msg
}

// CodexVersion is the output of `codex --version`, or nil when codex is not there or says nothing.
func CodexVersion() *string {
	out, err := exec.Command("codex", "--version").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return nil
	}
	return &v
}

// Git runs git -C repo with args and returns its stdout.
func Git(repo string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// Sum is the hex sha256 of data.
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func blobSum(repo, sha, path string) (string, error) {
	blob, err := Git(repo, "show", sha+":"+path)
	if err != nil {
		return "", err
	}
	return Sum(blob), nil
}

// ChainKey is the file name of the document without directory and extension.
func ChainKey(doc string) string {
	base := doc
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	return base
}

// Start reserves a new attempt directory under tmpRoot, atomically through mkdir, and writes
// attempt.json with the commit that round 1 reads. A document that is missing or uncommitted
// is an *InvalidError.
func Start(doc, repo, tmpRoot, kind string, deps Deps) (string, error) {
	info, err := os.Stat(filepath.Join(repo, doc))
	if err != nil || !info.Mode().IsRegular() {
		return "", &InvalidError{fmt.Sprintf("%s not found under the repository root: %s", kind, filepath.Join(repo, doc))}
	}
	dirty, err := Git(repo, "status", "--porcelain", "--", doc)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(dirty)) != "" {
		return "", &InvalidError{fmt.Sprintf("%s has uncommitted changes, commit first: %s", kind, doc)}
	}
	base := filepath.Join(tmpRoot, kind+"-review", ChainKey(doc))
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	stamp := deps.Now().UTC().Format("20060102T150405Z")
	var attempt string
	for {
		var token [3]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", err
		}
		attempt = filepath.Join(base, stamp+"-"+hex.EncodeToString(token[:]))
		err := os.Mkdir(attempt, 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	head, err := Git(repo, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	meta := orderedjson.NewObject()
	meta.Set("kind", kind)
	meta.Set("chain_key", ChainKey(doc))
	meta.Set("attempt", filepath.Base(attempt))
	meta.Set(kind, doc)
	meta.Set("reviewed_sha", strings.TrimSpace(string(head)))
	data, err := orderedjson.Marshal(meta)
	if err != nil {
		return "", err
	}
	return attempt, os.WriteFile(filepath.Join(attempt, "attempt.json"), data, 0o644)
}

// ReadMeta reads attempt.json; a missing kind (a v1/v2 attempt) is "plan".
func ReadMeta(attemptDir string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(attemptDir, "attempt.json"))
	if err != nil {
		return nil, err
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("attempt.json: %w", err)
	}
	if meta == nil {
		return nil, errors.New("attempt.json: not an object")
	}
	if _, ok := meta["kind"]; !ok {
		meta["kind"] = "plan"
	}
	return meta, nil
}

// metaString is the string at key of meta, or an error naming the key.
func metaString(meta map[string]any, key string) (string, error) {
	s, ok := meta[key].(string)
	if !ok {
		return "", fmt.Errorf("attempt.json: missing key '%s'", key)
	}
	return s, nil
}

func stagingDir(dataRoot, kind, chain, attempt string) string {
	return filepath.Join(dataRoot, kind+"-review", chain, "."+attempt+".tmp")
}

// Reserve puts the new attempt in the data directory at once: per chain the newest attempt is
// authoritative, even without a record. A failed reservation does not block the review: Write
// creates the directory after all. Every message goes to stderr.
func Reserve(attemptDir string, deps Deps, stderr io.Writer) {
	meta, err := ReadMeta(attemptDir)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "PLAN-REVIEW-RESERVATION FAILED: could not read attempt.json: %v\n", err)
		return
	}
	kind, _ := meta["kind"].(string)
	if reason := deps.HostRefusal(kind); reason != nil {
		_, _ = fmt.Fprintln(stderr, *reason)
		return
	}
	if err := reserve(attemptDir, meta, kind); err != nil {
		_, _ = fmt.Fprintf(stderr, "%s-REVIEW-RESERVATION FAILED: %v. The review carries on; "+
			"`write` creates the staging directory after all.\n", strings.ToUpper(kind), err)
	}
}

func reserve(attemptDir string, meta map[string]any, kind string) error {
	chain, err := metaString(meta, "chain_key")
	if err != nil {
		return err
	}
	attempt, err := metaString(meta, "attempt")
	if err != nil {
		return err
	}
	root, err := datadir.Dir(os.Getenv)
	if err != nil {
		return err
	}
	staging := stagingDir(root, kind, chain, attempt)
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(staging, 0o755); err != nil {
		return err
	}
	return copyFile(filepath.Join(attemptDir, "attempt.json"), filepath.Join(staging, "attempt.json"))
}

// rounds lists the round-<n>.json files of attemptDir by round number.
func rounds(attemptDir string) (map[int]string, []int, error) {
	entries, err := os.ReadDir(attemptDir)
	if err != nil {
		return nil, nil, err
	}
	found := map[int]string{}
	for _, e := range entries {
		if m := roundFile.FindStringSubmatch(e.Name()); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, nil, err
			}
			found[n] = filepath.Join(attemptDir, e.Name())
		}
	}
	order := make([]int, 0, len(found))
	for n := range found {
		order = append(order, n)
	}
	sort.Ints(order)
	return found, order, nil
}

// budgetChecks is one entry per budget file of the runner; a file that is unreadable or not a
// JSON object becomes decision "unreadable".
func budgetChecks(attemptDir string) ([]any, error) {
	entries, err := os.ReadDir(attemptDir)
	if err != nil {
		return nil, err
	}
	type check struct {
		round int
		file  string
		obj   *orderedjson.Object
	}
	var checks []check
	for _, e := range entries {
		m := budgetFile.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, err
		}
		doc := orderedjson.NewObject()
		doc.Set("decision", "unreadable")
		if data, err := os.ReadFile(filepath.Join(attemptDir, e.Name())); err == nil {
			if parsed, err := orderedjson.Parse(data); err == nil {
				if obj, ok := parsed.(*orderedjson.Object); ok {
					doc = obj
				}
			}
		}
		checks = append(checks, check{n, e.Name(), doc})
	}
	sort.Slice(checks, func(i, j int) bool {
		if checks[i].round != checks[j].round {
			return checks[i].round < checks[j].round
		}
		return checks[i].file < checks[j].file
	})
	out := make([]any, 0, len(checks))
	for _, c := range checks {
		entry := orderedjson.NewObject()
		entry.Set("round", json.Number(strconv.Itoa(c.round)))
		entry.Set("file", c.file)
		for _, k := range budgetFields {
			v, _ := c.obj.Get(k) // a missing field is nil: null
			entry.Set(k, v)
		}
		out = append(out, entry)
	}
	return out, nil
}

// finding is one finding of a round, as the record keeps it.
type finding struct {
	id       string
	round    int
	severity string
	cat      any
	decision any // string or nil
	sameAs   any
	source   any // string or nil
	sevBySrc any // *orderedjson.Object or nil
}

func (f finding) object() *orderedjson.Object {
	o := orderedjson.NewObject()
	o.Set("finding_id", f.id)
	o.Set("round", json.Number(strconv.Itoa(f.round)))
	o.Set("severity", f.severity)
	o.Set("cat", f.cat)
	o.Set("decision", f.decision)
	o.Set("same_as", f.sameAs)
	o.Set("source", f.source)
	o.Set("severity_by_source", f.sevBySrc)
	return o
}

// roundDoc is a parsed round-<n>.json: the findings as objects, and whether it is a
// verification round (top-level key "verified").
type roundDoc struct {
	findings []map[string]any
	verified bool
}

func readRound(path string) (roundDoc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return roundDoc{}, err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return roundDoc{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	raw, ok := doc["findings"].([]any)
	if !ok {
		return roundDoc{}, fmt.Errorf("%s: missing key 'findings'", filepath.Base(path))
	}
	out := roundDoc{}
	_, out.verified = doc["verified"]
	for _, f := range raw {
		obj, ok := f.(map[string]any)
		if !ok {
			return roundDoc{}, fmt.Errorf("%s: a finding is not an object", filepath.Base(path))
		}
		out.findings = append(out.findings, obj)
	}
	return out, nil
}

func (r roundDoc) severity(i int) (string, error) {
	s, ok := r.findings[i]["severity"].(string)
	if !ok {
		return "", errors.New("missing key 'severity'")
	}
	return s, nil
}

func findingID(n, index int) string { return "r" + strconv.Itoa(n) + "-f" + strconv.Itoa(index) }

// pyStr quotes s the way Python's repr does for the plain strings that appear in messages.
func pyStr(v any) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	if v == nil {
		return "None"
	}
	return fmt.Sprint(v)
}

func readJSONList(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	parsed, err := orderedjson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	list, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a list, got %s", filepath.Base(path), jsonTypeName(parsed))
	}
	return list, nil
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case *orderedjson.Object:
		return "dict"
	case []any:
		return "list"
	case string:
		return "str"
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	default:
		return "number"
	}
}

// decisions are the decisions of round n per finding_id: every ID exists and occurs once.
func decisions(attemptDir string, n, nFindings int, kind string) (map[string]*orderedjson.Object, error) {
	name := fmt.Sprintf("round-%d.decisions.json", n)
	sidecar := filepath.Join(attemptDir, name)
	if !isFile(sidecar) {
		return map[string]*orderedjson.Object{}, nil
	}
	allowed, ok := decisionsByKind[kind]
	if !ok {
		return nil, fmt.Errorf("unknown kind %s", pyStr(kind))
	}
	rows, err := readJSONList(sidecar)
	if err != nil {
		return nil, err
	}
	byID := map[string]*orderedjson.Object{}
	for _, r := range rows {
		row, ok := r.(*orderedjson.Object)
		if !ok {
			return nil, fmt.Errorf("%s: row is no object but %s", name, jsonTypeName(r))
		}
		fid, _ := row.Get("finding_id")
		d, _ := row.Get("decision")
		if ds, isStr := d.(string); !isStr || !slices.Contains(allowed, ds) {
			return nil, fmt.Errorf("%s: unknown decision %s", name, pyStr(d))
		}
		id, isStr := fid.(string)
		if !isStr || !validFindingID(id, n, nFindings) {
			return nil, fmt.Errorf("%s: finding_id %s does not exist in round %d", name, pyStr(fid), n)
		}
		if _, dup := byID[id]; dup {
			return nil, fmt.Errorf("%s: finding_id %s occurs twice", name, pyStr(fid))
		}
		byID[id] = row
	}
	return byID, nil
}

func validFindingID(id string, n, nFindings int) bool {
	for i := 1; i <= nFindings; i++ {
		if id == findingID(n, i) {
			return true
		}
	}
	return false
}

// sourceRow is the source of one round-1 finding.
type sourceRow struct {
	source string
	bySrc  *orderedjson.Object
}

// sources is the source per round-1 finding from round-1.sources.json, nil without a pair.
func sources(attemptDir string, nFindings int) (map[string]sourceRow, error) {
	const name = "round-1.sources.json"
	sidecar := filepath.Join(attemptDir, name)
	if !isFile(sidecar) {
		return nil, nil
	}
	rows, err := readJSONList(sidecar)
	if err != nil {
		return nil, err
	}
	byID := map[string]sourceRow{}
	for _, r := range rows {
		row, ok := r.(*orderedjson.Object)
		if !ok {
			return nil, fmt.Errorf("%s: row is no object but %s", name, jsonTypeName(r))
		}
		fid, _ := row.Get("finding_id")
		src, _ := row.Get("source")
		srcName, isStr := src.(string)
		if !isStr || !slices.Contains(sourceNames, srcName) {
			return nil, fmt.Errorf("%s: unknown source %s at %s", name, pyStr(src), pyStr(fid))
		}
		bySrc, err := severityBySource(row)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid severity_by_source at %s: %w", name, pyStr(fid), err)
		}
		id, isStr := fid.(string)
		if !isStr || !validFindingID(id, 1, nFindings) {
			return nil, fmt.Errorf("%s: finding_id %s does not exist in round 1", name, pyStr(fid))
		}
		if _, dup := byID[id]; dup {
			return nil, fmt.Errorf("%s: finding_id %s occurs twice", name, pyStr(fid))
		}
		byID[id] = sourceRow{srcName, bySrc}
	}
	var missing []string
	for i := 1; i <= nFindings; i++ {
		if _, ok := byID[findingID(1, i)]; !ok {
			missing = append(missing, findingID(1, i))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("%s: misses finding_id(s) %s", name, strings.Join(missing, ", "))
	}
	return byID, nil
}

// falsy is Python's truthiness for a parsed JSON value: null, false, 0, "", [] and {}.
func falsy(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return !t
	case string:
		return t == ""
	case json.Number:
		f, err := t.Float64()
		return err == nil && f == 0
	case []any:
		return len(t) == 0
	case *orderedjson.Object:
		return len(t.Keys()) == 0
	}
	return false
}

// severityBySource is the severity_by_source of a sources row: a missing or falsy value is {};
// otherwise an object of codex/fallback to a severity.
func severityBySource(row *orderedjson.Object) (*orderedjson.Object, error) {
	v, _ := row.Get("severity_by_source")
	if falsy(v) {
		return orderedjson.NewObject(), nil
	}
	obj, ok := v.(*orderedjson.Object)
	if !ok {
		return nil, fmt.Errorf("%v", v)
	}
	for _, k := range obj.Keys() {
		sev, _ := obj.Get(k)
		s, isStr := sev.(string)
		if (k != "codex" && k != "fallback") || !isStr || !slices.Contains(severities, s) {
			return nil, fmt.Errorf("%s: %v", k, sev)
		}
	}
	return obj, nil
}

// buildFindings is all findings over all rounds, plus round_kinds and the round-1 sources.
func buildFindings(attemptDir string, paths map[int]string, order []int, kind string) ([]finding, []string, map[string]sourceRow, error) {
	var findings []finding
	var roundKinds []string
	var srcs map[string]sourceRow
	for _, n := range order {
		doc, err := readRound(paths[n])
		if err != nil {
			return nil, nil, nil, err
		}
		if doc.verified {
			roundKinds = append(roundKinds, "verify")
		} else {
			roundKinds = append(roundKinds, "review")
		}
		byID, err := decisions(attemptDir, n, len(doc.findings), kind)
		if err != nil {
			return nil, nil, nil, err
		}
		if n == 1 {
			if srcs, err = sources(attemptDir, len(doc.findings)); err != nil {
				return nil, nil, nil, err
			}
		}
		for i, raw := range doc.findings {
			sev, err := doc.severity(i)
			if err != nil {
				return nil, nil, nil, err
			}
			f := finding{id: findingID(n, i+1), round: n, severity: sev, cat: raw["cat"]}
			if row := byID[f.id]; row != nil {
				f.decision, _ = row.Get("decision")
				f.sameAs, _ = row.Get("same_as")
			}
			if s, ok := srcs[f.id]; ok && n == 1 {
				f.source, f.sevBySrc = s.source, s.bySrc
			}
			findings = append(findings, f)
		}
	}
	return findings, roundKinds, srcs, nil
}

func codexConfig(codexHome string) (map[string]string, error) {
	values := map[string]string{}
	path := filepath.Join(codexHome, "config.toml")
	if !isFile(path) {
		return values, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if m := codexModel.FindStringSubmatch(line); m != nil {
			values[m[1]] = m[2]
		}
	}
	return values, nil
}

// Issue is the issue that stands in for the specs of a plan review.
type Issue struct {
	Number    int
	FetchedAt string
	Sum       string // sha256 of the issue body
}

// RecordInput is what BuildRecord needs besides the attempt directory.
type RecordInput struct {
	Repo          string
	Outcome       string
	ReviewMode    string
	Specs         []string
	FinalSHA      string
	Level         string
	ReviewerModel string
	SkillDir      string // repo-relative, or absolute
	CodexHome     string
	Issue         *Issue
	ShadowModel   string
}

func closes(kind, decision string) bool { return slices.Contains(closedByKind[kind], decision) }

// BuildRecord reads the attempt directory and returns the record, validated: every finding
// decided (unless aborted), nothing still open at converged, every open point accepted at
// round_limit.
func BuildRecord(attemptDir string, in RecordInput, deps Deps) (*orderedjson.Object, error) {
	meta, err := ReadMeta(attemptDir)
	if err != nil {
		return nil, err
	}
	kind, _ := meta["kind"].(string)
	if _, ok := decisionsByKind[kind]; !ok {
		return nil, fmt.Errorf("unknown kind %s", pyStr(meta["kind"]))
	}
	docPath, err := metaString(meta, kind)
	if err != nil {
		return nil, err
	}
	reviewedSHA, err := metaString(meta, "reviewed_sha")
	if err != nil {
		return nil, err
	}
	chain, err := metaString(meta, "chain_key")
	if err != nil {
		return nil, err
	}
	attempt, err := metaString(meta, "attempt")
	if err != nil {
		return nil, err
	}
	paths, order, err := rounds(attemptDir)
	if err != nil {
		return nil, err
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("no round-<n>.json in %s", attemptDir)
	}
	last := order[len(order)-1]
	var missing []string
	for n := 1; n <= last; n++ {
		if _, ok := paths[n]; !ok {
			missing = append(missing, strconv.Itoa(n))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing round(s) in %s: %s", attemptDir, strings.Join(missing, ", "))
	}

	findings, roundKinds, srcs, err := buildFindings(attemptDir, paths, order, kind)
	if err != nil {
		return nil, err
	}
	paired := srcs != nil
	if paired && in.ShadowModel == "" {
		return nil, errors.New("--shadow-model is required: round-1.sources.json exists (round 1 is paired)")
	}
	if !paired && in.ShadowModel != "" {
		return nil, errors.New("--shadow-model belongs only to a paired round 1 (round-1.sources.json is missing)")
	}
	var undecided []string
	for _, f := range findings {
		if f.decision == nil {
			undecided = append(undecided, f.id)
		}
	}
	if len(undecided) > 0 && in.Outcome != "aborted" {
		return nil, fmt.Errorf("no decision for %s; at %s every finding has one (one sidecar per round, the last one too)",
			strings.Join(undecided, ", "), in.Outcome)
	}
	lastOpen := []any{}
	var stillOpen, unaccepted []string
	for _, f := range findings {
		if f.round != last || (f.severity != "BLOCKER" && f.severity != "MAJOR") {
			continue
		}
		o := orderedjson.NewObject()
		o.Set("finding_id", f.id)
		o.Set("severity", f.severity)
		o.Set("decision", f.decision)
		lastOpen = append(lastOpen, o)
		d, _ := f.decision.(string)
		if !closes(kind, d) {
			stillOpen = append(stillOpen, f.id)
			if d != "accepted_open" {
				unaccepted = append(unaccepted, f.id)
			}
		}
	}
	if in.Outcome == "converged" && len(stillOpen) > 0 {
		return nil, fmt.Errorf("converged, but %s is still open: a fix after the last round is unreviewed (skill step 8, *Open*)",
			strings.Join(stillOpen, ", "))
	}
	if in.Outcome == "round_limit" && len(unaccepted) > 0 {
		return nil, fmt.Errorf("round_limit, but %s carries no accepted_open: run 'lighthouse plan-review-record accept-open <ATTEMPT> --doc <DOC>' first",
			strings.Join(unaccepted, ", "))
	}

	cfg, err := codexConfig(in.CodexHome)
	if err != nil {
		return nil, err
	}
	reviewer := orderedjson.NewObject()
	if in.ReviewMode == "fallback" {
		if in.ReviewerModel == "" {
			return nil, errors.New("--reviewer-model is required with --review-mode fallback")
		}
		reviewer.Set("model", in.ReviewerModel)
		reviewer.Set("codex_version", nil)
		reviewer.Set("same_family", true)
	} else {
		model := "unknown"
		if m, ok := cfg["model"]; ok {
			model = m
		}
		reviewer.Set("model", model)
		var version any
		if v := deps.CodexVersion(); v != nil {
			version = *v
		}
		reviewer.Set("codex_version", version)
		reviewer.Set("same_family", in.ReviewMode == "codex_same_family")
	}

	skill := in.SkillDir
	if !filepath.IsAbs(skill) {
		skill = filepath.Join(in.Repo, skill)
	}
	rec := orderedjson.NewObject()
	rec.Set("schema_version", json.Number(strconv.Itoa(SchemaVersion)))
	rec.Set("kind", kind)
	rec.Set("chain_key", chain)
	rec.Set("attempt", attempt)
	rec.Set("review_mode", in.ReviewMode)
	rec.Set(kind, docPath)
	rec.Set("reviewed_sha", reviewedSHA)
	rec.Set("final_sha", in.FinalSHA)
	for _, h := range []struct{ key, sha string }{{kind + "_sha256_reviewed", reviewedSHA}, {kind + "_sha256_final", in.FinalSHA}} {
		s, err := blobSum(in.Repo, h.sha, docPath)
		if err != nil {
			return nil, err
		}
		rec.Set(h.key, s)
	}
	specs := []any{}
	for _, s := range in.Specs {
		hash, err := blobSum(in.Repo, in.FinalSHA, s)
		if err != nil {
			return nil, err
		}
		o := orderedjson.NewObject()
		o.Set("path", s)
		o.Set("sha256", hash)
		specs = append(specs, o)
	}
	rec.Set("specs", specs)
	if in.Issue == nil {
		rec.Set("issue_spec", nil)
	} else {
		o := orderedjson.NewObject()
		o.Set("issue", json.Number(strconv.Itoa(in.Issue.Number)))
		o.Set("fetched_at", in.Issue.FetchedAt)
		o.Set("sha256", in.Issue.Sum)
		o.Set("file", fmt.Sprintf("issue-%d.md", in.Issue.Number))
		rec.Set("issue_spec", o)
	}
	for _, f := range []struct {
		key, file string
		required  bool
	}{
		{"prompt_sha256", "reviewer-prompt.md", true},
		{"verifier_prompt_sha256", "verifier-prompt.md", false},
		{"schema_sha256", "findings.schema.json", true},
		{"verification_schema_sha256", "verification.schema.json", false},
	} {
		path := filepath.Join(skill, f.file)
		if !f.required && !isFile(path) {
			rec.Set(f.key, nil)
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		rec.Set(f.key, Sum(data))
	}
	rec.Set("reviewer", reviewer)
	switch {
	case in.Level != "":
		rec.Set("level", in.Level)
	case in.ReviewMode == "fallback":
		rec.Set("level", nil)
	default:
		if effort, ok := cfg["model_reasoning_effort"]; ok {
			rec.Set("level", effort)
		} else {
			rec.Set("level", nil)
		}
	}
	rec.Set("rounds", json.Number(strconv.Itoa(len(order))))
	kinds := make([]any, len(roundKinds))
	for i, k := range roundKinds {
		kinds[i] = k
	}
	rec.Set("round_kinds", kinds)
	rec.Set("outcome", in.Outcome)
	rec.Set("paired", paired)
	if paired {
		shadow := orderedjson.NewObject()
		shadow.Set("model", in.ShadowModel)
		shadow.Set("same_family", true)
		rec.Set("shadow", shadow)
	} else {
		rec.Set("shadow", nil)
	}
	checks, err := budgetChecks(attemptDir)
	if err != nil {
		return nil, err
	}
	rec.Set("budget_checks", checks)
	objects := make([]any, len(findings))
	for i, f := range findings {
		objects[i] = f.object()
	}
	rec.Set("findings", objects)
	rec.Set("last_round_open", lastOpen)
	rec.Set("written_at", deps.Now().UTC().Format("2006-01-02T15:04:05")+"+00:00")
	return rec, nil
}

// rename is os.Rename; a test replaces it to make the last step of Write fail.
var rename = os.Rename

func recordString(record *orderedjson.Object, key string) (string, error) {
	v, _ := record.Get(key)
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("record: missing key '%s'", key)
	}
	return s, nil
}

// Write fills the staging directory of Reserve with the attempt files, record.json last, and
// renames it to the final directory in one step. The final directory is never overwritten.
func Write(attemptDir string, record *orderedjson.Object, dataRoot, issueBody string) (string, error) {
	kind, err := recordString(record, "kind")
	if err != nil {
		return "", err
	}
	chain, err := recordString(record, "chain_key")
	if err != nil {
		return "", err
	}
	attempt, err := recordString(record, "attempt")
	if err != nil {
		return "", err
	}
	final := filepath.Join(dataRoot, kind+"-review", chain, attempt)
	staging := stagingDir(dataRoot, kind, chain, attempt)
	if _, err := os.Lstat(final); err == nil {
		return "", fmt.Errorf("attempt already exists in the data directory: %s", final)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(attemptDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries { // ReadDir sorts by name
		name := e.Name()
		if roundFile.MatchString(name) || budgetFile.MatchString(name) ||
			strings.HasSuffix(name, ".decisions.json") || name == "attempt.json" || slices.Contains(pairFiles, name) {
			if err := copyFile(filepath.Join(attemptDir, name), filepath.Join(staging, name)); err != nil {
				return "", err
			}
		}
	}
	if issueBody != "" {
		spec, _ := record.Get("issue_spec")
		obj, ok := spec.(*orderedjson.Object)
		if !ok {
			return "", errors.New("record: issue body given, but no issue_spec")
		}
		file, _ := obj.Get("file")
		name, _ := file.(string)
		if err := copyFile(issueBody, filepath.Join(staging, name)); err != nil {
			return "", err
		}
	}
	data, err := orderedjson.Marshal(record)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(staging, "record.json"), data, 0o644); err != nil {
		return "", err
	}
	return final, rename(staging, final)
}
