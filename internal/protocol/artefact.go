package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/pytext"
)

// An artefact is a spec or plan that stands as a version chain of issue comments. Line 1 of a
// comment is an HTML comment with JSON metadata (`<!-- lighthouse: {...} -->`), then a visible
// `<sub>` line, a blank line and the content. Readers accept the legacy marker and the legacy
// keys; writers use the new ones.
//
// This file is the port of the former pack/core/lib/artefacts.py. Where Python and Go word a text
// rule differently, the Python wording wins (package pytext): the comment is compared byte for byte.

// GithubLimit is the maximum length of a GitHub comment, in characters.
const GithubLimit = 65_536

// DefaultSplitLimit is the length of a part, in characters, that SplitArtefact aims at.
const DefaultSplitLimit = 60_000

// ArtefactKinds are the kinds of artefact, in the order of the config's directories.
var ArtefactKinds = []string{"spec", "plan"}

// artefactMarker opens every artefact comment.
const artefactMarker = "<!-- lighthouse:"

var (
	artefactMetaLine = regexp.MustCompile(`^(?:<!-- lighthouse:|<!-- paia:) (\{.*\}) -->$`)
	artefactSHA      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	artefactHash     = regexp.MustCompile(`^[0-9a-f]{12}$`)
	integerText      = regexp.MustCompile(`^-?[0-9]+$`)
)

var artefactFields = []string{"kind", "version", "part", "parts", "file", "commit", "hash", "replaces"}

// legacyArtefactFields maps the keys of comments from before the move to the current ones.
var legacyArtefactFields = map[string]string{
	"soort":    "kind",
	"versie":   "version",
	"deel":     "part",
	"delen":    "parts",
	"bestand":  "file",
	"vervangt": "replaces",
}

// ArtefactMeta is the metadata of one artefact comment.
type ArtefactMeta struct {
	Kind                 string
	Version, Part, Parts int
	File, Commit, Hash   string
	// Replaces is the url of the comment that this version replaces; nil for none.
	Replaces *string
	// Extra holds the unknown fields in their order; ParseArtefactMeta fills them and
	// RenderArtefact writes them back after the eight fixed ones. Nil for none.
	Extra *orderedjson.Object
}

// MetadataError is the error of metadata that cannot be written into an HTML comment.
type MetadataError struct{ Msg string }

func (e *MetadataError) Error() string { return e.Msg }

// positive reads v as a JSON integer of at least 1. A bool, a fraction and an exponent are not.
func positive(v any) (int, bool) {
	n, ok := v.(json.Number)
	if !ok || !integerText.MatchString(n.String()) {
		return 0, false
	}
	i, err := strconv.Atoi(n.String())
	return i, err == nil && i >= 1
}

// readObject reads one JSON object keeping the order of its keys. A repeated key keeps its first
// place and its last value, as json.loads does.
func readObject(text string) (*orderedjson.Object, bool) {
	dec := json.NewDecoder(strings.NewReader(text))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	obj := orderedjson.NewObject()
	for dec.More() {
		keyTok, err := dec.Token()
		key, isString := keyTok.(string)
		if err != nil || !isString {
			return nil, false
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, false
		}
		value, err := orderedjson.Parse(raw)
		if err != nil {
			return nil, false
		}
		obj.Set(key, value)
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return obj, true
}

// ParseArtefactMeta reads the metadata of the first line of body; false when absent or invalid.
func ParseArtefactMeta(body string) (ArtefactMeta, bool) {
	lines := pytext.SplitLines(pytext.Strip(body), false)
	if len(lines) == 0 {
		return ArtefactMeta{}, false
	}
	match := artefactMetaLine.FindStringSubmatch(lines[0])
	if match == nil {
		return ArtefactMeta{}, false
	}
	raw, ok := readObject(match[1])
	if !ok {
		return ArtefactMeta{}, false
	}
	data := orderedjson.NewObject()
	for _, key := range raw.Keys() {
		value, _ := raw.Get(key)
		name := key
		if current, legacy := legacyArtefactFields[key]; legacy {
			name = current
		}
		data.Set(name, value)
	}
	for _, name := range artefactFields {
		if _, present := data.Get(name); !present {
			return ArtefactMeta{}, false
		}
	}
	str := func(name string) (string, bool) {
		v, _ := data.Get(name)
		s, ok := v.(string)
		return s, ok
	}
	var m ArtefactMeta
	var kindOK, fileOK, commitOK, hashOK bool
	m.Kind, kindOK = str("kind")
	m.File, fileOK = str("file")
	m.Commit, commitOK = str("commit")
	m.Hash, hashOK = str("hash")
	version, _ := data.Get("version")
	part, _ := data.Get("part")
	parts, _ := data.Get("parts")
	var versionOK, partOK, partsOK bool
	m.Version, versionOK = positive(version)
	m.Part, partOK = positive(part)
	m.Parts, partsOK = positive(parts)
	replaces, _ := data.Get("replaces")
	replacesOK := replaces == nil
	if s, isString := replaces.(string); isString {
		m.Replaces, replacesOK = &s, true
	}
	valid := kindOK && contains(ArtefactKinds, m.Kind) && versionOK && partOK && partsOK &&
		m.Part <= m.Parts && fileOK && commitOK && artefactSHA.MatchString(m.Commit) &&
		hashOK && artefactHash.MatchString(m.Hash) && replacesOK
	if !valid {
		return ArtefactMeta{}, false
	}
	for _, key := range data.Keys() {
		if !contains(artefactFields, key) {
			if m.Extra == nil {
				m.Extra = orderedjson.NewObject()
			}
			value, _ := data.Get(key)
			m.Extra.Set(key, value)
		}
	}
	return m, true
}

// metaJSON is `json.dumps(data, ensure_ascii=False, separators=(",", ":"))` of the metadata.
func metaJSON(m ArtefactMeta) string {
	pairs := []string{
		`"kind":` + pytext.JSONString(m.Kind, false),
		`"version":` + strconv.Itoa(m.Version),
		`"part":` + strconv.Itoa(m.Part),
		`"parts":` + strconv.Itoa(m.Parts),
		`"file":` + pytext.JSONString(m.File, false),
		`"commit":` + pytext.JSONString(m.Commit, false),
		`"hash":` + pytext.JSONString(m.Hash, false),
		`"replaces":null`,
	}
	if m.Replaces != nil {
		pairs[7] = `"replaces":` + pytext.JSONString(*m.Replaces, false)
	}
	if m.Extra != nil {
		for _, key := range m.Extra.Keys() {
			value, _ := m.Extra.Get(key)
			pairs = append(pairs, pytext.JSONString(key, false)+":"+compactJSON(value))
		}
	}
	return "{" + strings.Join(pairs, ",") + "}"
}

// compactJSON writes a parsed JSON value as `json.dumps` does with compact separators.
func compactJSON(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case string:
		return pytext.JSONString(t, false)
	case json.Number:
		return pythonNumber(t)
	case []any:
		items := make([]string, len(t))
		for i, item := range t {
			items[i] = compactJSON(item)
		}
		return "[" + strings.Join(items, ",") + "]"
	case *orderedjson.Object:
		items := make([]string, 0, len(t.Keys()))
		for _, key := range t.Keys() {
			item, _ := t.Get(key)
			items = append(items, pytext.JSONString(key, false)+":"+compactJSON(item))
		}
		return "{" + strings.Join(items, ",") + "}"
	}
	return fmt.Sprint(v)
}

// pythonNumber is how json.dumps writes a number it has read: an integer as it is, a float as
// repr() writes it (`1.50` is `1.5`, `1e2` is `100.0`).
func pythonNumber(n json.Number) string {
	if integerText.MatchString(n.String()) {
		return n.String()
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil || math.IsInf(f, 0) {
		return n.String()
	}
	abs := math.Abs(f)
	if abs != 0 && (abs < 1e-4 || abs >= 1e16) {
		return strconv.FormatFloat(f, 'e', -1, 64)
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// runeHead is the first n characters of s.
func runeHead(s string, n int) string {
	r := []rune(s)
	return string(r[:min(n, len(r))])
}

// Source is the repository an artefact links to: the forge kind ("" is github), the GitLab host
// ("" is gitlab.com) and the repo slug.
type Source struct{ Kind, Host, Repo string }

// BlobURL is the web address of path at commit sha: `<host>/<repo>/blob/<sha>/<path>` on
// GitHub, `<host>/<repo>/-/blob/<sha>/<path>` on GitLab. path is not escaped.
func (s Source) BlobURL(sha, path string) string {
	if s.Kind == "gitlab" {
		host := s.Host
		if host == "" {
			host = "gitlab.com"
		}
		return fmt.Sprintf("https://%s/%s/-/blob/%s/%s", host, s.Repo, sha, path)
	}
	return fmt.Sprintf("https://github.com/%s/blob/%s/%s", s.Repo, sha, path)
}

// RenderArtefact is RenderArtefactAt for a GitHub repo.
func RenderArtefact(m ArtefactMeta, content, language, repo string) (string, error) {
	return RenderArtefactAt(m, content, language, Source{Repo: repo})
}

// RenderArtefactAt is the comment body: metadata line, visible header line, blank line, content.
// The header line links to the file at its commit on src, in language. An empty repo is an
// error: an artefact links to its source.
func RenderArtefactAt(m ArtefactMeta, content, language string, src Source) (string, error) {
	jsonText := metaJSON(m)
	// `--` can close the HTML comment early.
	if strings.Contains(jsonText, "--") {
		return "", &MetadataError{Msg: "metadata holds '--'; that closes the HTML comment"}
	}
	if src.Repo == "" {
		return "", errors.New("forge.repo is not set; an artefact links to its source")
	}
	path := pytext.HTMLEscape(m.File)
	header := fmt.Sprintf("<sub>%s · %s %d · %s %d/%d · %s <a href=\"%s\">%s</a> @ %s</sub>",
		m.Kind, Heading("artefact_version", language), m.Version,
		Heading("artefact_part", language), m.Part, m.Parts,
		Heading("artefact_source", language), src.BlobURL(m.Commit, path), path, runeHead(m.Commit, 7))
	return artefactMarker + " " + jsonText + " -->\n" + header + "\n\n" + content, nil
}

// ArtefactHash is the first 12 hex characters of the sha256 of data.
func ArtefactHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:12]
}

var artefactFence = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})(.*)")

type cutPoint struct{ offset, class int }

// cutPoints are all the cut points of text, in characters. Every line start p > 0 is a cut point
// of class 4. Class 1 (`## `), 2 (`### `) and 3 (after a blank line) hold only outside a code
// block: the line before p and the line at p both start outside a block.
func cutPoints(text string) []cutPoint {
	var points []cutPoint
	var fenceChar byte
	fenceLen := 0
	previousOutside, previousBlank := false, false
	offset := 0
	for _, line := range pytext.SplitLines(text, true) {
		outside := fenceLen == 0
		if offset > 0 {
			class := 4
			if previousOutside && outside {
				switch {
				case strings.HasPrefix(line, "## "):
					class = 1
				case strings.HasPrefix(line, "### "):
					class = 2
				case previousBlank:
					class = 3
				}
			}
			points = append(points, cutPoint{offset, class})
		}
		if match := artefactFence.FindStringSubmatch(line); match != nil {
			run, rest := match[1], match[2]
			switch {
			case fenceLen == 0:
				fenceChar, fenceLen = run[0], len(run)
			case run[0] == fenceChar && len(run) >= fenceLen && pytext.Strip(rest) == "":
				fenceLen = 0
			}
		}
		previousOutside = outside
		previousBlank = pytext.Strip(line) == ""
		offset += len([]rune(line))
	}
	return points
}

// SplitArtefact cuts text into parts of at most limit characters (code points, as Python counts
// them), on markdown boundaries. Per part: the strongest cut-point class in the window
// (start, start+limit], within it the furthest point; without a cut point the cut falls mid-line.
// No part is empty (except for empty input) and the parts together are exactly text.
func SplitArtefact(text string, limit int) []string {
	runes := []rune(text)
	if limit < 1 {
		return []string{text}
	}
	points := cutPoints(text)
	var parts []string
	start := 0
	for len(runes)-start > limit {
		strongest, cut := 5, start+limit
		for _, p := range points {
			if p.offset > start && p.offset <= start+limit {
				if p.class < strongest {
					strongest, cut = p.class, p.offset
				} else if p.class == strongest {
					cut = p.offset
				}
			}
		}
		parts = append(parts, string(runes[start:cut]))
		start = cut
	}
	return append(parts, string(runes[start:]))
}

type chainPart struct {
	meta    ArtefactMeta
	comment Comment
}

// chainVersions are the parts of one chain (kind and file), grouped per version.
func chainVersions(cs []Comment, kind, file string) map[int][]chainPart {
	versions := map[int][]chainPart{}
	for _, c := range cs {
		if m, ok := ParseArtefactMeta(c.Body); ok && m.Kind == kind && m.File == file {
			versions[m.Version] = append(versions[m.Version], chainPart{m, c})
		}
	}
	return versions
}

// complete: all parts have the same Parts, and Part is exactly 1..Parts.
func complete(parts []chainPart) bool {
	total := parts[0].meta.Parts
	numbers := make([]int, len(parts))
	for i, p := range parts {
		if p.meta.Parts != total {
			return false
		}
		numbers[i] = p.meta.Part
	}
	sort.Ints(numbers)
	if len(numbers) != total {
		return false
	}
	for i, n := range numbers {
		if n != i+1 {
			return false
		}
	}
	return true
}

// CurrentArtefact is the parts of the highest complete version of the chain, sorted on part.
// Incomplete versions (a post that failed halfway) do not count; nil when there is no complete one.
func CurrentArtefact(cs []Comment, kind, file string) []Comment {
	versions := chainVersions(cs, kind, file)
	numbers := make([]int, 0, len(versions))
	for v := range versions {
		numbers = append(numbers, v)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(numbers)))
	for _, v := range numbers {
		if !complete(versions[v]) {
			continue
		}
		parts := versions[v]
		sort.SliceStable(parts, func(i, j int) bool { return parts[i].meta.Part < parts[j].meta.Part })
		out := make([]Comment, len(parts))
		for i, p := range parts {
			out[i] = p.comment
		}
		return out
	}
	return nil
}

// NextArtefactVersion is max(versions of the chain, incomplete ones too) + 1; 1 for an empty chain.
func NextArtefactVersion(cs []Comment, kind, file string) int {
	highest := 0
	for v := range chainVersions(cs, kind, file) {
		highest = max(highest, v)
	}
	return highest + 1
}
