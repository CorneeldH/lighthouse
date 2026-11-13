package codelang

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// Chain is the [code_language_llm] table of the session's lighthouse.toml.
//
// Placeholder until layer 2 lands (Task 6 of #276), which moves Chain and ChainFrom to llm.go
// and gives ChainFrom its checks.
type Chain struct {
	URL     string
	Models  []string
	KeyEnv  string
	EnvFile string
}

// ChainFrom reads the chain from c; nil without a table. Placeholder, see Chain.
func ChainFrom(c *config.Config) (*Chain, error) { return nil, nil }

// Inspection is what Check found in one file: findings, doubts for a later layer, and the
// chain of that layer (nil when the session has none).
type Inspection struct {
	Findings, Doubts []Finding
	Chain            *Chain
}

// languages maps a file suffix to the scanner that reads it as code.
var languages = map[string]string{".py": "python", ".sh": "shell", ".bash": "shell", ".go": "go"}

// markdownSuffixes are read as prose, inside code_language_paths only.
var markdownSuffixes = []string{".md", ".markdown"}

// Check returns the findings and doubts for the file at path (absolute) in the repo at root.
// Both are empty when the file is not checked: no lighthouse.toml, no code_language, an exempt
// glob, a file `lighthouse init` manages, or a file that is neither code nor inside
// code_language_paths. A code file is read as code, a Markdown file inside the scope as prose;
// any other file inside the scope counts by name only. The layer 2 chain comes only from
// sessionTOML (empty: none), never from the written file's repo. scorer is called only for a
// file that is checked. An error is a *CannotCheckError.
func Check(scorer func() (*Scorer, error), path, root, sessionTOML string) (Inspection, error) {
	settings, on, err := readSettings(root)
	if err != nil || !on {
		return Inspection{}, err
	}
	relative, err := relativeTo(path, root)
	if err != nil {
		return Inspection{}, err
	}
	suffix, stem := nameParts(filepath.Base(path))
	language, isCode := languages[suffix]
	dirs, inScope := ScopeDirs(relative, settings.paths)
	if !isCode && !inScope {
		return Inspection{}, nil
	}
	for _, glob := range settings.exempt {
		if matchGlob(glob, relative) {
			return Inspection{}, nil
		}
	}
	managed, err := managedPaths(root)
	if err != nil {
		return Inspection{}, err
	}
	if managed[relative] {
		return Inspection{}, nil
	}
	// Read for every checked file, doubts or not: a wrong table is COULD NOT CHECK at once,
	// and only then: a file that is not checked never trips over the session's table.
	chain, err := sessionChain(sessionTOML)
	if err != nil {
		return Inspection{}, err
	}
	if !isCode && slices.Contains(markdownSuffixes, suffix) {
		language, isCode = "markdown", true
	}
	var units []Unit
	if isCode {
		if units, err = readUnits(path, language); err != nil {
			return Inspection{}, err
		}
	}
	for _, dir := range dirs {
		units = append(units, Unit{Line: 1, Kind: "directory", Text: dir, Name: true})
	}
	units = append(units, Unit{Line: 1, Kind: "name", Text: stem, Name: true})
	s, err := scorer()
	if err != nil {
		return Inspection{}, cannotCheck(err)
	}
	result := Inspection{Chain: chain}
	for _, f := range judge(s, units) {
		if f.Doubt {
			result.Doubts = append(result.Doubts, f)
		} else {
			result.Findings = append(result.Findings, f)
		}
	}
	return result, nil
}

// cannotCheck makes err a *CannotCheckError with the same text.
func cannotCheck(err error) error {
	var cc *CannotCheckError
	if errors.As(err, &cc) {
		return cc
	}
	return &CannotCheckError{Reason: err.Error()}
}

// settings are the [project] keys of the checked repo that steer the check.
type settings struct {
	exempt, paths []string
}

// readSettings reads lighthouse.toml at root; on is false without the file or without
// project.code_language.
func readSettings(root string) (settings, bool, error) {
	file := filepath.Join(root, "lighthouse.toml")
	if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
		return settings{}, false, nil
	}
	cfg, err := config.Load(file)
	if err != nil {
		return settings{}, false, cannotCheck(err)
	}
	if _, err := config.Lookup(cfg, "project.code_language"); err != nil {
		if errors.Is(err, config.ErrNotSet) {
			return settings{}, false, nil
		}
		return settings{}, false, cannotCheck(err)
	}
	var s settings
	if s.exempt, err = stringList(cfg, "project.code_language_exempt"); err != nil {
		return settings{}, false, err
	}
	if s.paths, err = stringList(cfg, "project.code_language_paths"); err != nil {
		return settings{}, false, err
	}
	return s, true, nil
}

// stringList is the list of strings at key; empty when it is not set. config.Load has
// checked the type.
func stringList(cfg *config.Config, key string) ([]string, error) {
	v, err := config.Lookup(cfg, key)
	if errors.Is(err, config.ErrNotSet) {
		return nil, nil
	}
	if err != nil {
		return nil, cannotCheck(err)
	}
	var out []string
	for _, item := range v.([]any) {
		out = append(out, item.(string))
	}
	return out, nil
}

// relativeTo is path relative to root with "/" separators, like PurePath.relative_to.
func relativeTo(path, root string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &CannotCheckError{Reason: fmt.Sprintf("'%s' is not in the subpath of '%s'", path, root)}
	}
	return filepath.ToSlash(rel), nil
}

// nameParts is the suffix and the stem of a file name, as pathlib has them: the last dot
// splits, unless it is the first or the last character.
func nameParts(name string) (suffix, stem string) {
	i := strings.LastIndex(name, ".")
	if i > 0 && i < len(name)-1 {
		return name[i:], name[:i]
	}
	return "", name
}

// managedPaths is the set of paths under [[unit]] in .lighthouse/manifest.toml; empty without
// a manifest. Only `path` is read: the strict manifest reader would refuse what Python skips.
func managedPaths(root string) (map[string]bool, error) {
	manifest := filepath.Join(root, ".lighthouse", "manifest.toml")
	if info, err := os.Stat(manifest); err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	fail := func(err error) (map[string]bool, error) {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("cannot read %s: %v", manifest, err)}
	}
	raw, err := os.ReadFile(manifest)
	if err != nil {
		return fail(err)
	}
	if !utf8.Valid(raw) {
		return fail(errors.New("invalid UTF-8"))
	}
	var data map[string]any
	if _, err := toml.Decode(string(raw), &data); err != nil {
		return fail(err)
	}
	var list []any
	switch units := data["unit"].(type) {
	case nil:
	case []map[string]any:
		for _, u := range units {
			list = append(list, u)
		}
	case []any:
		list = units
	default:
		return fail(fmt.Errorf("unit is a %T, not a list of tables", units))
	}
	managed := map[string]bool{}
	for _, item := range list {
		unit, ok := item.(map[string]any)
		if !ok {
			return fail(fmt.Errorf("a unit is a %T, not a table", item))
		}
		path, ok := unit["path"].(string)
		if !ok {
			return fail(errors.New("a unit has no path"))
		}
		managed[path] = true
	}
	return managed, nil
}

// sessionChain is [code_language_llm] from the session's lighthouse.toml; nil without one.
func sessionChain(sessionTOML string) (*Chain, error) {
	if sessionTOML == "" {
		return nil, nil
	}
	if info, err := os.Stat(sessionTOML); err != nil || !info.Mode().IsRegular() {
		return nil, nil
	}
	cfg, err := config.Load(sessionTOML)
	if err != nil {
		return nil, cannotCheck(err)
	}
	chain, err := ChainFrom(cfg)
	if err != nil {
		return nil, cannotCheck(err)
	}
	return chain, nil
}

// bom is the UTF-8 byte order mark, which is not content.
var bom = []byte{0xef, 0xbb, 0xbf}

// readUnits reads the file at path as UTF-8, without a leading BOM and with universal
// newlines ("\r\n" and a bare "\r" become "\n", as Path.read_text does), and scans it.
func readUnits(path, language string) ([]Unit, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, cannotCheck(err)
	}
	raw = bytes.TrimPrefix(raw, bom)
	if reason, bad := invalidUTF8(raw); bad {
		return nil, &CannotCheckError{Reason: fmt.Sprintf("%s is not valid UTF-8: %s", path, reason)}
	}
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	return scanUnits(language, strings.ReplaceAll(src, "\r", "\n"))
}

// invalidUTF8 describes the first invalid sequence of raw the way Python's decoder does.
func invalidUTF8(raw []byte) (string, bool) {
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r != utf8.RuneError || size > 1 {
			i += size
			continue
		}
		b := raw[i]
		reason := "invalid continuation byte"
		switch {
		case b < 0xc2 || b > 0xf4:
			reason = "invalid start byte"
		case !utf8.FullRune(raw[i:]):
			reason = "unexpected end of data"
		}
		return fmt.Sprintf("'utf-8' codec can't decode byte 0x%02x in position %d: %s", b, i, reason), true
	}
	return "", false
}

// ScopeDirs returns the directory names of relative that a glob in globs lets through, and
// false when no glob covers the path. The names are those between the fixed start of each
// covering glob and the file, as a union in path order. The fixed start is the project's own
// layout and is not judged.
func ScopeDirs(relative string, globs []string) ([]string, bool) {
	parts := strings.Split(relative, "/")
	parts = parts[:len(parts)-1]
	positions := map[int]bool{}
	covered := false
	for _, glob := range globs {
		if !matchGlob(glob, relative) {
			continue
		}
		covered = true
		for p := fixedStart(glob); p < len(parts); p++ {
			positions[p] = true
		}
	}
	if !covered {
		return nil, false
	}
	names := []string{}
	for p := range parts {
		if positions[p] && !slices.Contains(names, parts[p]) {
			names = append(names, parts[p])
		}
	}
	return names, true
}

// fixedStart is the number of segments of glob before the first wildcard; with none, all but
// the file name.
func fixedStart(glob string) int {
	segments := strings.Split(glob, "/")
	for i, segment := range segments {
		if strings.ContainsAny(segment, "*?[") {
			return i
		}
	}
	return len(segments) - 1
}

// globs caches the translated pattern per glob.
var globs sync.Map

// matchGlob reports whether name matches glob with the semantics of Python's
// fnmatch.fnmatchcase: "*" also matches "/", "[!...]" negates, and the rest is literal.
func matchGlob(glob, name string) bool {
	if re, ok := globs.Load(glob); ok {
		return re.(*regexp.Regexp).MatchString(name)
	}
	re := regexp.MustCompile(translateGlob(glob))
	globs.Store(glob, re)
	return re.MatchString(name)
}

// translateGlob is fnmatch.translate for RE2.
func translateGlob(glob string) string {
	pat := []rune(glob)
	var out strings.Builder
	out.WriteString(`^(?s:`)
	for i := 0; i < len(pat); {
		c := pat[i]
		i++
		switch c {
		case '*':
			for i < len(pat) && pat[i] == '*' {
				i++
			}
			out.WriteString(`.*`)
		case '?':
			out.WriteString(`.`)
		case '[':
			class, next, ok := globClass(pat, i)
			if !ok {
				out.WriteString(`\[`)
				continue
			}
			out.WriteString(class)
			i = next
		default:
			out.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	out.WriteString(`)\z`)
	return out.String()
}

// globClass translates the bracket expression whose body starts at pat[i], as
// fnmatch.translate does in Python 3.12; ok is false when the bracket does not close, and next
// is the index after the closing "]".
func globClass(pat []rune, i int) (class string, next int, ok bool) {
	j := i
	if j < len(pat) && pat[j] == '!' {
		j++
	}
	if j < len(pat) && pat[j] == ']' {
		j++
	}
	for j < len(pat) && pat[j] != ']' {
		j++
	}
	if j >= len(pat) {
		return "", 0, false
	}
	chunks := classChunks(pat, i, j)
	negate := len(chunks[0]) > 0 && chunks[0][0] == '!'
	if negate {
		chunks[0] = chunks[0][1:]
	}
	var body strings.Builder
	for k, chunk := range chunks {
		if k > 0 {
			body.WriteString("-")
		}
		for _, r := range chunk {
			body.WriteString(classRune(r))
		}
	}
	switch {
	case body.Len() == 0 && !negate:
		return `[^\x00-\x{10FFFF}]`, j + 1, true // an empty set matches nothing
	case body.Len() == 0:
		return `.`, j + 1, true
	case negate:
		return "[^" + body.String() + "]", j + 1, true
	}
	return "[" + body.String() + "]", j + 1, true
}

// classChunks splits the bracket body pat[i:j] at the hyphens that make a range, and drops
// the empty ranges, as fnmatch.translate does. Consecutive chunks join into a range: the last
// rune of one and the first rune of the next.
func classChunks(pat []rune, i, j int) [][]rune {
	if !slices.Contains(pat[i:j], '-') {
		return [][]rune{slices.Clone(pat[i:j])}
	}
	var chunks [][]rune
	k := i + 1
	if pat[i] == '!' {
		k = i + 2
	}
	for {
		at := -1
		if k < j {
			if d := slices.Index(pat[k:j], '-'); d >= 0 {
				at = k + d
			}
		}
		if at < 0 {
			break
		}
		chunks = append(chunks, slices.Clone(pat[i:at]))
		i = at + 1
		k = at + 3
	}
	if chunk := pat[i:j]; len(chunk) > 0 {
		chunks = append(chunks, slices.Clone(chunk))
	} else {
		chunks[len(chunks)-1] = append(chunks[len(chunks)-1], '-')
	}
	for k := len(chunks) - 1; k > 0; k-- {
		if chunks[k-1][len(chunks[k-1])-1] > chunks[k][0] {
			chunks[k-1] = append(chunks[k-1][:len(chunks[k-1])-1], chunks[k][1:]...)
			chunks = slices.Delete(chunks, k, k+1)
		}
	}
	return chunks
}

// classRune is r as a literal inside an RE2 character class.
func classRune(r rune) string {
	if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
		return string(r)
	}
	return fmt.Sprintf(`\x{%x}`, r)
}
