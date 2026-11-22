// Package direction checks the shape of the direction documents of a repo: the strategy, the
// tenets and one area file per area label. It checks form only, never whether the content is wise.
package direction

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/labels"
)

var (
	notNowID    = regexp.MustCompile(`^\*\*N(\d+)\*\*`)
	strategyID  = regexp.MustCompile(`^\*\*S(\d+)\*\*`)
	anyNotNowID = regexp.MustCompile(`\*\*N\d+[a-z]*\*\*`)
	anyStratID  = regexp.MustCompile(`\*\*S\d+[a-z]*\*\*`)
	tenetHead   = regexp.MustCompile(`^## ([A-Z])\. (.+)$`)
	linkRe      = regexp.MustCompile(`\]\(([^)#]+)(#[^)]*)?\)`)
	targetRe    = regexp.MustCompile(`→ ([a-z][a-z0-9-]*)`)
)

// Heading keys of .lighthouse/headings.toml, in the order a document wants them.
var (
	areaKeys     = []string{"area_crux", "policy", "not_now"}
	strategyKeys = []string{"strategy_diagnosis", "strategy_audience", "policy", "not_now", "strategy_roadmap"}
)

// vocab is the headings table of the repo plus the language of [project] language; it turns a
// key into its variants. Reading accepts every variant.
type vocab struct {
	table    *config.HeadingsTable
	language string
	err      error // first lookup error, reported once
}

func (v *vocab) variants(key string) []string {
	out, err := v.table.Variants(key)
	if err != nil && v.err == nil {
		v.err = err
	}
	return out
}

// matches is whether text equals a variant of key.
func (v *vocab) matches(key, text string) bool {
	for _, w := range v.variants(key) {
		if text == w {
			return true
		}
	}
	return false
}

// names is the variants of key as "a / b", for a message.
func (v *vocab) names(key string) string { return strings.Join(v.variants(key), " / ") }

type finding struct {
	path string
	line int
	msg  string
}

type heading struct {
	text string
	line int
}

// doc is a markdown file split into lines; fenced code is blanked so no rule reads it.
type doc struct {
	path  string
	lines []string
}

func (d doc) heads() []heading {
	var hs []heading
	for i, l := range d.lines {
		if strings.HasPrefix(l, "## ") {
			hs = append(hs, heading{strings.TrimSpace(l[3:]), i + 1})
		}
	}
	return hs
}

// section returns the line numbers (1-based) of the lines after the "## name" heading up to the next one.
func (d doc) section(v *vocab, key string) (from, to int, ok bool) {
	for i, l := range d.lines {
		if ok && strings.HasPrefix(l, "## ") {
			return from, i, true
		}
		if strings.HasPrefix(l, "## ") && v.matches(key, strings.TrimSpace(l[3:])) {
			from, ok = i+2, true
		}
	}
	return from, len(d.lines), ok
}

// bullet is a top-level "- " line with its continuation lines.
type bullet struct {
	line int
	text string
}

func (d doc) bullets(from, to int) []bullet {
	var bs []bullet
	for n := from; n <= to; n++ {
		l := d.lines[n-1]
		switch {
		case strings.HasPrefix(l, "- "):
			bs = append(bs, bullet{n, l[2:]})
		case strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#"):
			bs = append(bs, bullet{}) // closes the open bullet
		case len(bs) > 0 && bs[len(bs)-1].line != 0:
			bs[len(bs)-1].text += " " + strings.TrimSpace(l)
		}
	}
	var out []bullet
	for _, b := range bs {
		if b.line != 0 {
			out = append(out, b)
		}
	}
	return out
}

func readDoc(root, rel string) (doc, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return doc{}, err
	}
	lines := strings.Split(string(b), "\n")
	fenced := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			fenced = !fenced
			lines[i] = ""
			continue
		}
		if fenced {
			lines[i] = ""
		}
	}
	return doc{rel, lines}, nil
}

// repoPaths is the part of lighthouse.toml this package reads: [paths], [labels] area_prefix and
// [project] language. Nothing of [forge] is needed.
type repoPaths struct {
	Language string
	Paths    struct{ Labels, Tenets, Areas, Strategy string }
	Labels   struct{ AreaPrefix string }
}

// readPaths decodes lighthouse.toml of root; a missing file is an empty config.
func readPaths(root string) (repoPaths, error) {
	var raw struct {
		Project struct {
			Language string `toml:"language"`
		} `toml:"project"`
		Paths struct {
			Labels   string `toml:"labels"`
			Tenets   string `toml:"tenets"`
			Areas    string `toml:"areas"`
			Strategy string `toml:"strategy"`
		} `toml:"paths"`
		Labels struct {
			AreaPrefix string `toml:"area_prefix"`
		} `toml:"labels"`
	}
	var out repoPaths
	if _, err := toml.DecodeFile(filepath.Join(root, "lighthouse.toml"), &raw); err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	out.Language = raw.Project.Language
	if out.Language == "" {
		out.Language = "en"
	}
	out.Paths.Labels, out.Paths.Tenets, out.Paths.Areas, out.Paths.Strategy = raw.Paths.Labels, raw.Paths.Tenets, raw.Paths.Areas, raw.Paths.Strategy
	out.Labels.AreaPrefix = raw.Labels.AreaPrefix
	return out, nil
}

// CheckConfig reports a lighthouse.toml of root that cannot be parsed; a missing one is fine.
func CheckConfig(root string) error {
	_, err := readPaths(root)
	return err
}

// Check checks the direction documents named in the lighthouse.toml of root and returns every
// error at once as "path:line: message", sorted; empty means the shape is right.
func Check(root string) []string {
	var fs []finding
	add := func(p string, line int, format string, a ...any) {
		fs = append(fs, finding{p, line, fmt.Sprintf(format, a...)})
	}
	cfg, err := readPaths(root)
	if err != nil {
		add("lighthouse.toml", 1, "kan niet laden: %v", err)
		return render(fs)
	}
	if cfg.Paths.Strategy == "" && cfg.Paths.Tenets == "" && cfg.Paths.Areas == "" {
		return nil // opt-out: the repo wants no direction documents
	}
	table, err := config.LoadHeadingsFile(filepath.Join(root, ".lighthouse", "headings.toml"))
	if err != nil {
		add(".lighthouse/headings.toml", 1, "kan niet laden: %v", err)
		return render(fs)
	}
	v := &vocab{table: table, language: cfg.Language}

	var docs []doc
	read := func(key, rel string) (doc, bool) {
		if rel == "" {
			add("lighthouse.toml", 1, "%s is niet gezet", key)
			return doc{}, false
		}
		d, err := readDoc(root, rel)
		if err != nil {
			add(rel, 1, "%s: kan niet lezen: %v", key, err)
			return doc{}, false
		}
		docs = append(docs, d)
		return d, true
	}

	if d, ok := read("paths.strategy", cfg.Paths.Strategy); ok {
		checkHeadings(d, v, strategyKeys, add)
		checkNotNow(d, v, strategyID, anyStratID, "S", nil, add)
	}
	if d, ok := read("paths.tenets", cfg.Paths.Tenets); ok {
		checkTenets(d, v, add)
	}
	if cfg.Paths.Areas == "" {
		add("lighthouse.toml", 1, "paths.areas is niet gezet")
	} else {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(cfg.Paths.Areas)))
		if err != nil {
			add(cfg.Paths.Areas, 1, "paths.areas: kan niet lezen: %v", err)
		}
		hasMD := false
		for _, e := range entries {
			hasMD = hasMD || (!e.IsDir() && strings.HasSuffix(e.Name(), ".md"))
		}
		// No taxonomy is no error while there are no area files to compare with it.
		var areaNames []string
		labelsOK := false
		if hasMD || (cfg.Paths.Labels != "" && cfg.Labels.AreaPrefix != "") {
			areaNames, labelsOK = areaLabels(root, cfg, add)
		}
		known := map[string]bool{}
		for _, n := range areaNames {
			known[n] = true
		}
		onDisk := map[string]bool{}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			onDisk[name] = true
			rel := path.Join(cfg.Paths.Areas, e.Name())
			if labelsOK && !known[name] {
				add(rel, 1, "geen arealabel %s%s voor dit bestand", cfg.Labels.AreaPrefix, name)
			}
			if d, ok := read("paths.areas", rel); ok {
				checkHeadings(d, v, areaKeys, add)
				checkNotNow(d, v, notNowID, anyNotNowID, "N", known, add)
			}
		}
		if err == nil {
			for _, n := range areaNames {
				if !onDisk[n] {
					add(path.Join(cfg.Paths.Areas, n+".md"), 1, "geen bestand voor arealabel %s%s", cfg.Labels.AreaPrefix, n)
				}
			}
		}
	}
	for _, d := range docs {
		checkLinks(root, d, add)
	}
	if v.err != nil {
		add(".lighthouse/headings.toml", 1, "kop-sleutel ontbreekt: %v", v.err)
	}
	return render(fs)
}

// areaLabels returns the area names of the label taxonomy, and false when it cannot be read.
func areaLabels(root string, cfg repoPaths, add func(string, int, string, ...any)) ([]string, bool) {
	if cfg.Paths.Labels == "" {
		add("lighthouse.toml", 1, "paths.labels is niet gezet")
		return nil, false
	}
	if cfg.Labels.AreaPrefix == "" {
		add("lighthouse.toml", 1, "labels.area_prefix is niet gezet")
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(cfg.Paths.Labels)))
	if err != nil {
		add(cfg.Paths.Labels, 1, "paths.labels: kan niet lezen: %v", err)
		return nil, false
	}
	var names []string
	for _, l := range labels.ParseTaxonomy(string(b)) {
		if n, ok := strings.CutPrefix(l.Name, cfg.Labels.AreaPrefix); ok {
			names = append(names, n)
		}
	}
	return names, true
}

// checkHeadings wants exactly the ## headings of keys, in that order, each in any variant.
func checkHeadings(d doc, v *vocab, keys []string, add func(string, int, string, ...any)) {
	got := d.heads()
	for i, k := range keys {
		if i >= len(got) {
			add(d.path, len(d.lines), "kop ## %s ontbreekt", v.names(k))
			return
		}
		if !v.matches(k, got[i].text) {
			add(d.path, got[i].line, "kop ## %s waar ## %s verwacht wordt", got[i].text, v.names(k))
			return
		}
	}
	if len(got) > len(keys) {
		add(d.path, got[len(keys)].line, "extra kop ## %s", got[len(keys)].text)
	}
}

// checkNotNow checks the bullets of the not-now section: one unique ID each and, with known set,
// a "→ <area>" that exists.
func checkNotNow(d doc, v *vocab, idRe, anyRe *regexp.Regexp, letter string, known map[string]bool, add func(string, int, string, ...any)) {
	from, to, ok := d.section(v, "not_now")
	if !ok {
		return
	}
	seen := map[string]bool{}
	for _, b := range d.bullets(from, to) {
		m := idRe.FindStringSubmatch(b.text)
		switch {
		case m == nil:
			add(d.path, b.line, "bullet zonder ID van de vorm **%s<n>**", letter)
		case len(anyRe.FindAllString(b.text, -1)) > 1:
			add(d.path, b.line, "bullet met twee ID's")
		case seen[m[1]]:
			add(d.path, b.line, "dubbel ID %s%s", letter, m[1])
		default:
			seen[m[1]] = true
		}
		if known != nil {
			for _, t := range targetRe.FindAllStringSubmatch(b.text, -1) {
				if !known[t[1]] {
					add(d.path, b.line, "→ %s is geen area", t[1])
				}
			}
		}
	}
}

// checkTenets checks the "## <letter>. X over Y" headings and their evidence.
func checkTenets(d doc, v *vocab, add func(string, int, string, ...any)) {
	letters := map[byte]int{}
	for _, h := range d.heads() {
		m := tenetHead.FindStringSubmatch("## " + h.text)
		if m == nil {
			add(d.path, h.line, "kop ## %s heeft niet de vorm ## <letter>. X boven Y", h.text)
			continue
		}
		letter, title := m[1][0], m[2]
		if first, dup := letters[letter]; dup {
			add(d.path, h.line, "dubbele letter %c (eerst op regel %d)", letter, first)
		} else {
			letters[letter] = h.line
		}
		if n := separators(v, title); n != 1 {
			add(d.path, h.line, "kop heeft %d keer ' %s ', precies 1 verwacht", n, v.names("tenet_over"))
		}
		if strings.Contains(title, "(vervallen)") {
			continue
		}
		if links := evidenceLinks(d, v, h.line); links < 2 {
			add(d.path, h.line, "geen *%s*-regel met minstens 2 links (gevonden: %d)", v.names("tenet_evidence"), links)
		}
	}
	var max byte
	for l := range letters {
		if l > max {
			max = l
		}
	}
	for l := byte('A'); l <= max; l++ {
		if _, ok := letters[l]; !ok {
			add(d.path, 1, "letter %c ontbreekt tussen A en %c (een vervallen uitgangspunt houdt zijn letter)", l, max)
		}
	}
}

// separators counts the " <over> " separators of a tenet title: the occurrences of all variants
// together, or, when that is more than one, those of the variant of [project] language.
func separators(v *vocab, title string) int {
	all := 0
	for _, w := range v.variants("tenet_over") {
		all += strings.Count(title, " "+w+" ")
	}
	if all <= 1 {
		return all
	}
	if w, err := v.table.Heading("tenet_over", v.language); err == nil {
		return strings.Count(title, " "+w+" ")
	}
	return all
}

// evidenceLinks counts the links of the evidence line (and its continuation) of the section at line.
func evidenceLinks(d doc, v *vocab, line int) int {
	for i := line; i < len(d.lines) && !strings.HasPrefix(d.lines[i], "## "); i++ {
		for _, w := range v.variants("tenet_evidence") {
			if !strings.HasPrefix(d.lines[i], "*"+w+"*") {
				continue
			}
			n := 0
			for ; i < len(d.lines) && strings.TrimSpace(d.lines[i]) != ""; i++ {
				n += strings.Count(d.lines[i], "](")
			}
			return n
		}
	}
	return 0
}

// checkLinks wants every relative link of d to exist.
func checkLinks(root string, d doc, add func(string, int, string, ...any)) {
	dir := path.Dir(d.path)
	for i, l := range d.lines {
		for _, m := range linkRe.FindAllStringSubmatch(l, -1) {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") || path.IsAbs(target) {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path.Join(dir, target)))); err != nil {
				add(d.path, i+1, "link %s wijst nergens heen", target)
			}
		}
	}
}

func render(fs []finding) []string {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].path != fs[j].path {
			return fs[i].path < fs[j].path
		}
		return fs[i].line < fs[j].line
	})
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.path+":"+strconv.Itoa(f.line)+": "+f.msg)
	}
	return out
}
