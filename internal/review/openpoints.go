package review

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// ErrNoOpenPoints is returned by AcceptOpen when the last round has no open BLOCKER/MAJOR.
var ErrNoOpenPoints = errors.New("no open BLOCKER/MAJOR in the last round: nothing to accept")

// openText is the text under the heading of the block, per [project] language.
var openText = map[string][2]string{
	"nl": {
		"Ronde {n} haalde de rondegrens met deze punten open. Ze zijn aanvaard als `accepted_open`;",
		"de volgende fase of de PR-review weegt ze opnieuw.",
	},
	"en": {
		"Round {n} reached the round limit with these points open. They are accepted as `accepted_open`;",
		"the next phase or the PR review weighs them again.",
	},
}

var (
	closingKeyword = regexp.MustCompile(`(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?)(:?\s+)#(\d+)`)
	fence          = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
	specLink       = regexp.MustCompile(`\]\(([^)\s]+\.md)\)`)
)

// Point is an open BLOCKER/MAJOR of the last round.
type Point struct {
	FindingID string
	Severity  string
	Section   string
	Problem   string
}

// openPoints are the BLOCKER/MAJOR of the last round without a closing decision, in ID order.
func openPoints(attemptDir string) (int, []Point, error) {
	meta, err := ReadMeta(attemptDir)
	if err != nil {
		return 0, nil, err
	}
	kind, _ := meta["kind"].(string)
	if _, ok := decisionsByKind[kind]; !ok {
		return 0, nil, fmt.Errorf("unknown kind %s", pyStr(meta["kind"]))
	}
	paths, order, err := rounds(attemptDir)
	if err != nil {
		return 0, nil, err
	}
	if len(order) == 0 {
		return 0, nil, fmt.Errorf("no round-<n>.json in %s", attemptDir)
	}
	last := order[len(order)-1]
	doc, err := readRound(paths[last])
	if err != nil {
		return 0, nil, err
	}
	byID, err := decisions(attemptDir, last, len(doc.findings), kind)
	if err != nil {
		return 0, nil, err
	}
	var points []Point
	for i, f := range doc.findings {
		sev, err := doc.severity(i)
		if err != nil {
			return 0, nil, err
		}
		id := findingID(last, i+1)
		decision := ""
		if row := byID[id]; row != nil {
			d, _ := row.Get("decision")
			decision, _ = d.(string)
		}
		if (sev == "BLOCKER" || sev == "MAJOR") && !closes(kind, decision) {
			section := "?"
			for _, key := range []string{"plan_section", "spec_section"} {
				if s, ok := f[key].(string); ok && s != "" {
					section = s
					break
				}
			}
			problem, _ := f["problem"].(string)
			points = append(points, Point{id, sev, section, problem})
		}
	}
	return last, points, nil
}

// acceptedRows plans AcceptOpen without writing: the last round, the new sidecar rows and the
// open points.
func acceptedRows(attemptDir string) (int, []any, []Point, error) {
	last, points, err := openPoints(attemptDir)
	if err != nil {
		return 0, nil, nil, err
	}
	rows := []any{}
	sidecar := filepath.Join(attemptDir, fmt.Sprintf("round-%d.decisions.json", last))
	if isFile(sidecar) {
		if rows, err = readJSONList(sidecar); err != nil {
			return 0, nil, nil, err
		}
	}
	byID := map[string]*orderedjson.Object{}
	for _, r := range rows {
		row, ok := r.(*orderedjson.Object) // openPoints validated the rows
		if !ok {
			return 0, nil, nil, errors.New("a decisions row is no object")
		}
		id, _ := row.Get("finding_id")
		key, _ := id.(string)
		byID[key] = row
	}
	for _, p := range points {
		row := byID[p.FindingID]
		if row == nil {
			row = orderedjson.NewObject()
			row.Set("finding_id", p.FindingID)
			row.Set("decision", "accepted_open")
			row.Set("reason", "round_limit; without an earlier decision")
			rows = append(rows, row)
			continue
		}
		if d, _ := row.Get("decision"); d != "accepted_open" {
			old := fmt.Sprintf("was %v", d)
			if reason, _ := row.Get("reason"); !falsy(reason) {
				old += fmt.Sprintf(": %v", reason)
			}
			row.Set("decision", "accepted_open")
			row.Set("reason", "round_limit; "+old)
		}
	}
	return last, rows, points, nil
}

func writeSidecar(attemptDir string, n int, rows []any) error {
	data, err := orderedjson.Marshal(rows)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(attemptDir, fmt.Sprintf("round-%d.decisions.json", n)), data, 0o644)
}

// AcceptOpen sets every still-open BLOCKER/MAJOR of the last round to accepted_open in its
// decisions sidecar and puts the block heading:open_after_review into doc (replacing an
// existing block). It returns the block. The sequence is: read and render everything first,
// then the sidecar, then the document; an error before that writes nothing. ErrNoOpenPoints
// when nothing is open.
func AcceptOpen(attemptDir, doc string) (string, error) {
	last, rows, points, err := acceptedRows(attemptDir)
	if err != nil {
		return "", err
	}
	if len(points) == 0 {
		return "", ErrNoOpenPoints
	}
	block, err := RenderOpen(last, points, "")
	if err != nil {
		return "", err
	}
	text, err := os.ReadFile(doc)
	if err != nil {
		return "", err
	}
	newText, err := withOpenBlock(universalNewlines(string(text)), block)
	if err != nil {
		return "", err
	}
	if err := writeSidecar(attemptDir, last, rows); err != nil {
		return "", err
	}
	if err := os.WriteFile(doc, []byte(newText), 0o644); err != nil {
		return "", err
	}
	return block, nil
}

// RenderOpen is the markdown block heading:open_after_review in language ("" is [project]
// language); closing words with #N are neutralised.
func RenderOpen(round int, points []Point, language string) (string, error) {
	if language == "" {
		path, err := config.Path()
		if err != nil {
			return "", err
		}
		c, err := config.Load(path)
		if err != nil {
			return "", err
		}
		language = c.Language
	}
	text, ok := openText[language]
	if !ok {
		return "", fmt.Errorf("no open-points text for language '%s'", language)
	}
	heading, err := config.Heading("open_after_review", language)
	if err != nil {
		return "", err
	}
	lines := []string{
		heading, "",
		strings.ReplaceAll(text[0], "{n}", fmt.Sprint(round)),
		text[1], "",
	}
	for _, p := range points {
		problem := closingKeyword.ReplaceAllString(strings.Join(strings.Fields(p.Problem), " "), "${1}${2}issue ${3}")
		lines = append(lines, fmt.Sprintf("- **%s** (%s, %s): %s", p.FindingID, p.Severity, p.Section, problem))
	}
	return strings.Join(lines, "\n") + "\n", nil
}

// splitLines splits text like Python's str.splitlines(keepends=True).
func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		switch r {
		case '\r':
			end := i + size
			if strings.HasPrefix(text[end:], "\n") {
				end++
			}
			lines = append(lines, text[start:end])
			start, i = end, end
		case '\n', '\v', '\f', '\x1c', '\x1d', '\x1e', '\x85', ' ', ' ':
			lines = append(lines, text[start:i+size])
			start, i = i+size, i+size
		default:
			i += size
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// OpenSection is the span of the block heading:open_after_review (every variant), if it is the
// last "## " section. Lines inside a code fence do not count as a heading. The span runs from
// the heading to the end of text; ok is false without such a block.
func OpenSection(text string) (start, end int, ok bool, err error) {
	variants, err := config.Variants("open_after_review")
	if err != nil {
		return 0, 0, false, err
	}
	var open string
	lastHeading, lastText := -1, ""
	offset := 0
	for _, line := range splitLines(text) {
		m := fence.FindStringSubmatch(line)
		switch {
		case open != "":
			if m != nil && m[1][0] == open[0] && len(m[1]) >= len(open) {
				open = ""
			}
		case m != nil:
			open = m[1]
		case strings.HasPrefix(line, "## "):
			lastHeading, lastText = offset, strings.TrimRightFunc(line, unicode.IsSpace)
		}
		offset += len(line)
	}
	if lastHeading < 0 {
		return 0, 0, false, nil
	}
	for _, v := range variants {
		if v == lastText {
			return lastHeading, len(text), true, nil
		}
	}
	return 0, 0, false, nil
}

func withOpenBlock(text, block string) (string, error) {
	start, _, ok, err := OpenSection(text)
	switch {
	case err != nil:
		return "", err
	case ok:
		return text[:start] + block, nil
	case strings.TrimSpace(text) != "":
		return strings.TrimRightFunc(text, unicode.IsSpace) + "\n\n" + block, nil
	default:
		return block, nil
	}
}

// universalNewlines reads text like Python's text mode: \r\n and \r become \n.
func universalNewlines(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// resolve makes path absolute and follows symlinks when it exists.
func resolve(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// rel is doc relative to repo with forward slashes, or doc itself when it lies outside repo.
func rel(doc, repo string) string {
	r, err := filepath.Rel(resolve(repo), resolve(doc))
	if err != nil || r == ".." || strings.HasPrefix(r, "../") {
		return doc
	}
	return filepath.ToSlash(r)
}

func under(path, dir string) bool {
	r, err := filepath.Rel(dir, path)
	return err == nil && r != ".." && !strings.HasPrefix(r, "../")
}

func joinPath(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// configString is the string value of a config key.
func configString(key string) (string, error) {
	path, err := config.Path()
	if err != nil {
		return "", err
	}
	c, err := config.Load(path)
	if err != nil {
		return "", err
	}
	v, err := config.Lookup(c, key)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(v), nil
}

// specDocs are the specs that the **Spec:** line of a plan names and that exist under repo.
func specDocs(plan, text, repo string) ([]string, error) {
	specsDir, err := configString("paths.specs")
	if err != nil {
		return nil, err
	}
	specPath := regexp.MustCompile(regexp.QuoteMeta(strings.Trim(specsDir, "/")) + "/[^\\s)\\]>`'\"#]+\\.md")
	specLine := ""
	for _, line := range splitLines(text) {
		if strings.HasPrefix(line, "**Spec:**") {
			specLine = strings.TrimRight(line, "\r\n")
			break
		}
	}
	var candidates []string
	for _, m := range specPath.FindAllString(specLine, -1) {
		candidates = append(candidates, joinPath(repo, m))
	}
	for _, m := range specLink.FindAllStringSubmatch(specLine, -1) {
		if strings.HasPrefix(m[1], "http:") || strings.HasPrefix(m[1], "https:") {
			continue
		}
		candidates = append(candidates, joinPath(filepath.Dir(plan), m[1]))
	}
	repoReal := resolve(repo)
	var found []string
	for _, c := range candidates {
		path := resolve(c)
		if _, err := os.Stat(path); err != nil || !under(path, repoReal) || containsString(found, path) {
			continue
		}
		found = append(found, path)
	}
	return found, nil
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// PROpenPoints is the blocks heading:open_after_review of docs (and of the specs of their
// plans) under "## Open review points", for the PR body; "" without a block. A missing
// document is skipped; any other read error, a file that is not UTF-8 text or a config error
// is returned.
func PROpenPoints(docs []string, repo string) (string, error) {
	plansDir, err := configString("paths.plans")
	if err != nil {
		return "", err
	}
	plansDir = strings.Trim(plansDir, "/") + "/"
	var sections []string
	seen := map[string]bool{}
	queue := append([]string(nil), docs...)
	for len(queue) > 0 {
		doc := queue[0]
		queue = queue[1:]
		key := resolve(doc)
		if seen[key] {
			continue
		}
		seen[key] = true
		data, err := os.ReadFile(doc)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !utf8.Valid(data) {
			return "", fmt.Errorf("%s: not a UTF-8 text file", doc)
		}
		text := universalNewlines(string(data))
		start, _, ok, err := OpenSection(text)
		if err != nil {
			return "", err
		}
		if ok {
			lines := []string{"From " + rel(doc, repo) + ":"}
			for _, l := range splitLines(text[start:]) {
				if l = strings.TrimRight(l, "\r\n"); strings.HasPrefix(l, "- ") {
					lines = append(lines, l)
				}
			}
			sections = append(sections, strings.Join(lines, "\n"))
		}
		if strings.HasPrefix(rel(doc, repo), plansDir) {
			specs, err := specDocs(doc, text, repo)
			if err != nil {
				return "", err
			}
			queue = append(specs, queue...)
		}
	}
	if len(sections) == 0 {
		return "", nil
	}
	return "## Open review points\n\n" + strings.Join(sections, "\n\n") + "\n", nil
}
