package review

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// The paired round-1 shadow run: a reviewer from the same model family runs alongside Codex on
// the same SHA and prompt. Blind labels both reviews A/B; only Merge unblinds.

var (
	pairReviewers = []string{"codex", "fallback"}
	minorDecision = []string{"minor_yes", "minor_no"}
)

const (
	blindDecisionsFile = "round-1.blind-decisions.json"
	blindHeader        = "| ID | Sev | Cat | Section | Problem | Failure | Evidence | Proposed fix |\n" +
		"|----|-----|-----|---------|---------|---------|----------|--------------|\n"
	mergeHeader = "| ID | Source | Sev | Decision | Reason |\n|----|--------|-----|----------|--------|\n"
)

// PairDeps are the seams of the pair: the draw of who is A and the state of the work tree.
type PairDeps struct {
	// Coin is true when Codex is A.
	Coin func() bool
	// TreeState is HEAD and whether the work tree is clean, untracked files included.
	TreeState func(root string) (head string, clean bool, err error)
}

// DefaultPairDeps is the production wiring.
func DefaultPairDeps() PairDeps {
	return PairDeps{Coin: randomCoin, TreeState: gitTreeState}
}

func randomCoin() bool {
	var b [1]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return b[0]&1 == 1
}

func gitTreeState(root string) (string, bool, error) {
	head, err := Git(root, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	status, err := Git(root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(head)), strings.TrimSpace(string(status)) == "", nil
}

// RunPair runs `blind` or `merge` and returns the exit code: 0, or 4 with
// `SHADOW-PAIR FAILED: <reason>` on stderr. On 4 nothing reaches stdout.
func RunPair(sub, attemptDir string, repoRoot func() (string, error), deps PairDeps, stdout, stderr io.Writer) int {
	var table string
	var err error
	switch sub {
	case "blind":
		table, err = Blind(attemptDir, repoRoot, deps)
	case "merge":
		table, err = Merge(attemptDir, repoRoot)
	default:
		err = fmt.Errorf("unknown subcommand %q", sub)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "SHADOW-PAIR FAILED: %v\n", err)
		return 4
	}
	_, _ = fmt.Fprint(stdout, table)
	return 0
}

// pairCell makes reviewer text one table cell: a pipe or line break does not shift a column.
func pairCell(text string) string {
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "\r\n", " ", "\r", " ", "\n", " ").Replace(text)
}

// pairFinding is one finding of a raw file, in the order of that file.
type pairFinding struct {
	obj      *orderedjson.Object
	severity string
}

func (f pairFinding) str(key string) string {
	s, _ := f.obj.Get(key)
	text, _ := s.(string)
	return text
}

// blindEntry is what a blind ID stands for.
type blindEntry struct {
	id       string
	reviewer string
	pos      int // from 1
	finding  pairFinding
}

func severityRank(sev string) int { return slices.Index(severities, sev) }

// pairKind is the kind of the attempt and the problem with its attempt.json, if any.
func pairKind(attemptDir string) (map[string]any, string, error) {
	meta, err := ReadMeta(attemptDir)
	if err != nil {
		return nil, "", err
	}
	kind, _ := meta["kind"].(string)
	if _, ok := DefaultSkillDirs[kind]; !ok {
		return nil, "", fmt.Errorf("unknown kind %s", pyRepr(meta["kind"]))
	}
	return meta, kind, nil
}

func loadRaws(attemptDir, kind string, repoRoot func() (string, error)) (map[string]*orderedjson.Object, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(root, DefaultSkillDirs[kind], "findings.schema.json"))
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	raws := map[string]*orderedjson.Object{}
	for _, reviewer := range pairReviewers {
		raw, err := loadRaw(attemptDir, reviewer, schema)
		if err != nil {
			return nil, err
		}
		raws[reviewer] = raw
	}
	return raws, nil
}

func loadRaw(attemptDir, reviewer string, schema map[string]any) (*orderedjson.Object, error) {
	name := "round-1." + reviewer + ".json"
	data, err := os.ReadFile(filepath.Join(attemptDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing", name)
	}
	if err != nil {
		return nil, err
	}
	doc, err := orderedjson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if problems := rawViolations(doc, schema); len(problems) > 0 {
		return nil, fmt.Errorf("%s does not match findings.schema.json: %s", name, strings.Join(problems, "; "))
	}
	return doc.(*orderedjson.Object), nil
}

func checkTree(meta map[string]any, repoRoot func() (string, error), deps PairDeps) error {
	want, ok := meta["reviewed_sha"]
	if !ok {
		return errors.New("attempt.json misses reviewed_sha")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	head, clean, err := deps.TreeState(root)
	if err != nil {
		return err
	}
	if head != want || !clean {
		return errors.New("work tree changed since start or not clean")
	}
	return nil
}

// readPair is the pairing from pair.json: letter to reviewer. It is never redrawn.
func readPair(attemptDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(attemptDir, "pair.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("pair.json is missing: run `blind` first")
	}
	if err != nil {
		return nil, err
	}
	var pair map[string]string
	valid := json.Unmarshal(data, &pair) == nil && len(pair) == 2 &&
		(pair["A"] == "codex" && pair["B"] == "fallback" || pair["A"] == "fallback" && pair["B"] == "codex")
	if !valid {
		return nil, errors.New("pair.json invalid: no valid A/B pairing")
	}
	return pair, nil
}

// ensurePair draws who is A once, writes pair.json and reads it back; an existing file wins.
func ensurePair(attemptDir string, deps PairDeps) (map[string]string, error) {
	path := filepath.Join(attemptDir, "pair.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		a, b := "fallback", "codex"
		if deps.Coin() {
			a, b = "codex", "fallback"
		}
		text := fmt.Sprintf("{\"A\": %q, \"B\": %q}\n", a, b)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return readPair(attemptDir)
}

// blindIndex lists blind ID to entry, A first and then B, each in its own order.
func blindIndex(pair map[string]string, raws map[string]*orderedjson.Object) []blindEntry {
	var entries []blindEntry
	for _, letter := range []string{"A", "B"} {
		list, _ := raws[pair[letter]].Get("findings")
		for i, item := range list.([]any) {
			obj := item.(*orderedjson.Object)
			sev, _ := obj.Get("severity")
			entries = append(entries, blindEntry{
				id: fmt.Sprintf("%s-f%d", letter, i+1), reviewer: pair[letter], pos: i + 1,
				finding: pairFinding{obj: obj, severity: sev.(string)},
			})
		}
	}
	return entries
}

func entryByID(entries []blindEntry, id any) (blindEntry, bool) {
	s, ok := id.(string)
	if !ok {
		return blindEntry{}, false
	}
	for _, e := range entries {
		if e.id == s {
			return e, true
		}
	}
	return blindEntry{}, false
}

func blindTable(entries []blindEntry, kind string) string {
	sorted := slices.Clone(entries)
	slices.SortStableFunc(sorted, func(a, b blindEntry) int {
		if c := severityRank(a.finding.severity) - severityRank(b.finding.severity); c != 0 {
			return c
		}
		if c := strings.Compare(a.id[:1], b.id[:1]); c != 0 {
			return c
		}
		return a.pos - b.pos
	})
	var sb strings.Builder
	sb.WriteString(blindHeader)
	for _, e := range sorted {
		f := e.finding
		cat := "-"
		if c, ok := f.obj.Get("cat"); ok {
			cat, _ = c.(string)
		}
		cells := []string{e.id, f.severity, cat, f.str(kind + "_section"), f.str("problem"),
			f.str("failure_scenario"), f.str("evidence"), f.str("proposed_fix")}
		for i, c := range cells {
			cells[i] = pairCell(c)
		}
		sb.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	return sb.String()
}

// Blind checks the work tree and both raw files, draws who is A once, and writes and returns
// round-1.blind.md: the findings of both as A-f<i>/B-f<i>, without source.
func Blind(attemptDir string, repoRoot func() (string, error), deps PairDeps) (string, error) {
	meta, kind, err := pairKind(attemptDir)
	if err != nil {
		return "", err
	}
	if err := checkTree(meta, repoRoot, deps); err != nil {
		return "", err
	}
	raws, err := loadRaws(attemptDir, kind, repoRoot)
	if err != nil {
		return "", err
	}
	pair, err := ensurePair(attemptDir, deps)
	if err != nil {
		return "", err
	}
	table := blindTable(blindIndex(pair, raws), kind)
	if err := os.WriteFile(filepath.Join(attemptDir, "round-1.blind.md"), []byte(table), 0o644); err != nil {
		return "", err
	}
	return table, nil
}

// pairGroup is one merged finding: the blind IDs one decision covers.
type pairGroup struct {
	ids      []string
	bySource map[string]blindEntry
	severity string
	row      *orderedjson.Object
}

func loadBlindDecisions(attemptDir string) ([]*orderedjson.Object, error) {
	data, err := os.ReadFile(filepath.Join(attemptDir, blindDecisionsFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s is missing", blindDecisionsFile)
	}
	if err != nil {
		return nil, err
	}
	doc, err := orderedjson.Parse(data)
	if err != nil {
		return nil, err
	}
	list, ok := doc.([]any)
	rows := make([]*orderedjson.Object, 0, len(list))
	for _, item := range list {
		row, isObj := item.(*orderedjson.Object)
		if !isObj {
			ok = false
			break
		}
		rows = append(rows, row)
	}
	if !ok {
		return nil, fmt.Errorf("%s: expected a list of objects", blindDecisionsFile)
	}
	return rows, nil
}

// covered are the entries one decision covers, ordered by letter and position; `also` points to
// the other letter.
func covered(row *orderedjson.Object, entries []blindEntry) ([]blindEntry, error) {
	fid, _ := row.Get("finding_id")
	also, _ := row.Get("also")
	var got []blindEntry
	valid := fid != nil
	for _, id := range []any{fid, also} {
		if id == nil {
			continue
		}
		e, ok := entryByID(entries, id)
		if !ok {
			valid = false
		}
		got = append(got, e)
	}
	if !valid {
		return nil, fmt.Errorf("unknown or missing blind ID in %s", pyRepr(row))
	}
	if len(got) == 2 && got[0].id[0] == got[1].id[0] {
		return nil, fmt.Errorf("%s also %s: `also` points to the same reviewer", got[0].id, got[1].id)
	}
	slices.SortFunc(got, func(a, b blindEntry) int {
		if c := strings.Compare(a.id[:1], b.id[:1]); c != 0 {
			return c
		}
		return a.pos - b.pos
	})
	return got, nil
}

func checkDecision(row *orderedjson.Object, severity, kind string) error {
	decision, _ := row.Get("decision")
	fid, _ := row.Get("finding_id")
	d, isString := decision.(string)
	if !isString || d == "accepted_open" || !slices.Contains(decisionsByKind[kind], d) {
		return fmt.Errorf("%s: decision %s does not belong to %s", pyText(fid), pyRepr(decision), kind)
	}
	if (severity == "MINOR") != slices.Contains(minorDecision, d) {
		return fmt.Errorf("%s: decision %s does not fit %s", pyText(fid), pyRepr(decision), severity)
	}
	if reason, _ := row.Get("reason"); !isText(reason) {
		return fmt.Errorf("%s: `reason` is missing or no text", pyText(fid))
	}
	return nil
}

func isText(v any) bool { _, ok := v.(string); return ok }

// groupDecisions makes one group per merged finding, in the order of round-1.json.
func groupDecisions(rows []*orderedjson.Object, entries []blindEntry, kind string) ([]pairGroup, error) {
	seen := map[string]bool{}
	var groups []pairGroup
	for _, row := range rows {
		got, err := covered(row, entries)
		if err != nil {
			return nil, err
		}
		var double []string
		for _, e := range got {
			if seen[e.id] {
				double = append(double, e.id)
			}
		}
		if len(double) > 0 {
			return nil, fmt.Errorf("%s is covered more than once", slices.Min(double))
		}
		g := pairGroup{bySource: map[string]blindEntry{}, severity: "MINOR", row: row}
		for i, e := range got {
			seen[e.id] = true
			g.ids = append(g.ids, e.id)
			g.bySource[e.reviewer] = e
			if i == 0 || severityRank(e.finding.severity) < severityRank(g.severity) {
				g.severity = e.finding.severity
			}
		}
		if err := checkDecision(row, g.severity, kind); err != nil {
			return nil, err
		}
		groups = append(groups, g)
	}
	var missing []string
	for _, e := range entries {
		if !seen[e.id] {
			missing = append(missing, e.id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no decision for %s", strings.Join(missing, ", "))
	}
	slices.SortStableFunc(groups, func(a, b pairGroup) int { return mergeOrder(a) - mergeOrder(b) })
	return groups, nil
}

// mergeOrder puts the groups with a Codex finding first, in its own order, then the fallback
// findings without a Codex partner.
func mergeOrder(g pairGroup) int {
	if e, ok := g.bySource["codex"]; ok {
		return e.pos
	}
	return 1<<20 + g.bySource["fallback"].pos
}

func strList(v any) []any {
	list, _ := v.([]any)
	return list
}

func mergedRound(raws map[string]*orderedjson.Object, groups []pairGroup) *orderedjson.Object {
	findings := []any{}
	status := "Clean"
	for _, g := range groups {
		source := "fallback"
		if _, ok := g.bySource["codex"]; ok {
			source = "codex"
		}
		f := orderedjson.NewObject()
		for _, k := range g.bySource[source].finding.obj.Keys() {
			v, _ := g.bySource[source].finding.obj.Get(k)
			f.Set(k, v)
		}
		f.Set("severity", g.severity)
		findings = append(findings, f)
		if g.severity == "BLOCKER" {
			status = "Blockers found"
		} else if g.severity == "MAJOR" && status == "Clean" {
			status = "Majors only"
		}
	}
	text := func(r, key string) string {
		v, _ := raws[r].Get(key)
		s, _ := v.(string)
		return s
	}
	var cnv []any
	for _, r := range pairReviewers {
		v, _ := raws[r].Get("could_not_verify")
		for _, item := range strList(v) {
			if !slices.Contains(cnv, item) {
				cnv = append(cnv, item)
			}
		}
	}
	if cnv == nil {
		cnv = []any{}
	}
	top3, _ := raws["codex"].Get("top3")
	round := orderedjson.NewObject()
	round.Set("review_markdown", "## Codex\n\n"+text("codex", "review_markdown")+"\n\n## Fallback\n\n"+text("fallback", "review_markdown"))
	round.Set("status", status)
	round.Set("findings", findings)
	round.Set("top3", top3)
	round.Set("verdict", text("codex", "verdict"))
	round.Set("could_not_verify", cnv)
	return round
}

func sourceRows(groups []pairGroup) []any {
	rows := []any{}
	for n, g := range groups {
		source := "both"
		if len(g.bySource) == 1 {
			for r := range g.bySource {
				source = r
			}
		}
		ids := make([]any, len(g.ids))
		for i, id := range g.ids {
			ids[i] = id
		}
		bySev := orderedjson.NewObject()
		for _, r := range pairReviewers {
			if e, ok := g.bySource[r]; ok {
				bySev.Set(r, e.finding.severity)
			}
		}
		row := orderedjson.NewObject()
		row.Set("finding_id", fmt.Sprintf("r1-f%d", n+1))
		row.Set("source", source)
		row.Set("blind_ids", ids)
		row.Set("severity_by_source", bySev)
		rows = append(rows, row)
	}
	return rows
}

func writeOrdered(path string, v any) error {
	data, err := orderedjson.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Merge unblinds: it reads pair.json, both raw files and the blind decisions, writes the
// canonical round-1.json with round-1.decisions.json and round-1.sources.json, and returns the
// unblinded table. Everything is built before anything is written.
func Merge(attemptDir string, repoRoot func() (string, error)) (string, error) {
	_, kind, err := pairKind(attemptDir)
	if err != nil {
		return "", err
	}
	pair, err := readPair(attemptDir)
	if err != nil {
		return "", err
	}
	raws, err := loadRaws(attemptDir, kind, repoRoot)
	if err != nil {
		return "", err
	}
	rows, err := loadBlindDecisions(attemptDir)
	if err != nil {
		return "", err
	}
	groups, err := groupDecisions(rows, blindIndex(pair, raws), kind)
	if err != nil {
		return "", err
	}
	sources := sourceRows(groups)
	decisions := []any{}
	var sb strings.Builder
	sb.WriteString(mergeHeader)
	for n, g := range groups {
		id := fmt.Sprintf("r1-f%d", n+1)
		decision, _ := g.row.Get("decision")
		reason, _ := g.row.Get("reason")
		d := orderedjson.NewObject()
		d.Set("finding_id", id)
		d.Set("decision", decision)
		d.Set("reason", reason)
		decisions = append(decisions, d)
		src, _ := sources[n].(*orderedjson.Object).Get("source")
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", id, src, g.severity, pairCell(decision.(string)), pairCell(reason.(string)))
	}
	merged := mergedRound(raws, groups)
	for _, w := range []struct {
		name string
		v    any
	}{{"round-1.decisions.json", decisions}, {"round-1.sources.json", sources}, {"round-1.json", merged}} {
		if err := writeOrdered(filepath.Join(attemptDir, w.name), w.v); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// jsonTypeOK reports whether v has the JSON type name of a findings schema.
func jsonTypeOK(name string, v any) (known, ok bool) {
	switch name {
	case "string":
		_, ok = v.(string)
	case "array":
		_, ok = v.([]any)
	case "object":
		_, ok = v.(*orderedjson.Object)
	default:
		return false, false
	}
	return true, ok
}

// validateFindings is what is wrong with obj against one object node of the schema, without a
// schema library. Only what the findings schemas use: `required`, `additionalProperties: false`,
// per property `type` and `enum`, and for an array one level of `items.type`. The texts are those
// of review_pair.py, so a skill that reads them sees the same.
func validateFindings(obj any, node map[string]any) []string {
	o, isObj := obj.(*orderedjson.Object)
	if !isObj {
		return []string{"no object but " + pyTypeName(obj)}
	}
	props, _ := node["properties"].(map[string]any)
	var problems []string
	required, _ := node["required"].([]any)
	for _, key := range required {
		if _, ok := o.Get(key.(string)); !ok {
			problems = append(problems, "misses "+pyRepr(key))
		}
	}
	if extra, _ := node["additionalProperties"].(bool); node["additionalProperties"] != nil && !extra {
		for _, key := range o.Keys() {
			if _, ok := props[key]; !ok {
				problems = append(problems, "unknown field "+pyRepr(key))
			}
		}
	}
	for _, key := range o.Keys() {
		value, _ := o.Get(key)
		spec, _ := props[key].(map[string]any)
		typeName, _ := spec["type"].(string)
		known, ok := jsonTypeOK(typeName, value)
		enum, hasEnum := spec["enum"].([]any)
		switch {
		case known && !ok:
			problems = append(problems, fmt.Sprintf("%s is no %s", pyRepr(key), typeName))
		case hasEnum && !slices.ContainsFunc(enum, func(e any) bool { return pyRepr(e) == pyRepr(value) }):
			problems = append(problems, fmt.Sprintf("%s = %s is not in %s", pyRepr(key), pyRepr(value), pyRepr(enum)))
		default:
			list, isList := value.([]any)
			items, _ := spec["items"].(map[string]any)
			itemType, _ := items["type"].(string)
			if !isList {
				continue
			}
			for _, item := range list {
				if k, itemOK := jsonTypeOK(itemType, item); k && !itemOK {
					problems = append(problems, fmt.Sprintf("%s holds an item that is no %s", pyRepr(key), itemType))
					break
				}
			}
		}
	}
	return problems
}

// rawViolations checks a raw file against the schema, then each finding against its items node.
func rawViolations(doc any, schema map[string]any) []string {
	if problems := validateFindings(doc, schema); len(problems) > 0 {
		return problems
	}
	props, _ := schema["properties"].(map[string]any)
	findings, _ := props["findings"].(map[string]any)
	items, _ := findings["items"].(map[string]any)
	list, _ := doc.(*orderedjson.Object).Get("findings")
	var problems []string
	for i, f := range strList(list) {
		for _, p := range validateFindings(f, items) {
			problems = append(problems, fmt.Sprintf("finding %d: %s", i+1, p))
		}
	}
	return problems
}

func pyTypeName(v any) string {
	switch t := v.(type) {
	case string:
		return "str"
	case bool:
		return "bool"
	case nil:
		return "NoneType"
	case []any:
		return "list"
	case *orderedjson.Object:
		return "dict"
	case json.Number:
		if strings.ContainsAny(t.String(), ".eE") {
			return "float"
		}
		return "int"
	}
	return "object"
}

// pyText is str(v) of a Python value that came from JSON.
func pyText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return pyRepr(v)
}

// pyRepr is repr(v) of a Python value that came from JSON, as review_pair.py puts it in messages.
func pyRepr(v any) string {
	switch t := v.(type) {
	case string:
		return pyQuote(t)
	case bool:
		if t {
			return "True"
		}
		return "False"
	case nil:
		return "None"
	case json.Number:
		return t.String()
	case []any:
		parts := make([]string, len(t))
		for i, item := range t {
			parts[i] = pyRepr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *orderedjson.Object:
		parts := make([]string, 0, len(t.Keys()))
		for _, k := range t.Keys() {
			item, _ := t.Get(k)
			parts = append(parts, pyQuote(k)+": "+pyRepr(item))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// pyQuote is the repr of a Python str: single quotes unless the text holds one and no double.
func pyQuote(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote || r == '\\':
			sb.WriteRune('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&sb, `\x%02x`, r)
		default:
			sb.WriteRune(r)
		}
	}
	sb.WriteRune(quote)
	return sb.String()
}
