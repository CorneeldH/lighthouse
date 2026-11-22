// Package direction checks the shape of the direction documents of a repo: the strategy, the
// tenets and one area file per area label. It checks form only, never whether the content is wise.
package direction

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/labels"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
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

var (
	areaHeadings     = []string{"Crux", "Leidend beleid", "Bewust niet"}
	strategyHeadings = []string{"Diagnose", "Voor wie", "Leidend beleid", "Bewust niet", "Roadmap"}
)

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
func (d doc) section(name string) (from, to int, ok bool) {
	for i, l := range d.lines {
		if ok && strings.HasPrefix(l, "## ") {
			return from, i, true
		}
		if strings.TrimSpace(l) == "## "+name {
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

// Check checks the direction documents named in the lighthouse.toml of root and returns every
// error at once as "path:line: message", sorted; empty means the shape is right.
func Check(root string) []string {
	var fs []finding
	add := func(p string, line int, format string, a ...any) {
		fs = append(fs, finding{p, line, fmt.Sprintf(format, a...)})
	}
	cfg, err := repoconfig.Load(root)
	if err != nil {
		add("lighthouse.toml", 1, "kan niet laden: %v", err)
		return render(fs)
	}
	areaNames, labelsOK := areaLabels(root, cfg, add)

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
		checkHeadings(d, strategyHeadings, add)
		checkNotNow(d, strategyID, anyStratID, "S", nil, add)
	}
	if d, ok := read("paths.tenets", cfg.Paths.Tenets); ok {
		checkTenets(d, add)
	}
	if cfg.Paths.Areas == "" {
		add("lighthouse.toml", 1, "paths.areas is niet gezet")
	} else {
		entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(cfg.Paths.Areas)))
		if err != nil {
			add(cfg.Paths.Areas, 1, "paths.areas: kan niet lezen: %v", err)
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
				checkHeadings(d, areaHeadings, add)
				checkNotNow(d, notNowID, anyNotNowID, "N", known, add)
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
	return render(fs)
}

// areaLabels returns the area names of the label taxonomy, and false when it cannot be read.
func areaLabels(root string, cfg repoconfig.Repo, add func(string, int, string, ...any)) ([]string, bool) {
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

// checkHeadings wants exactly the ## headings of want, in that order.
func checkHeadings(d doc, want []string, add func(string, int, string, ...any)) {
	got := d.heads()
	for i, w := range want {
		if i >= len(got) {
			add(d.path, len(d.lines), "kop ## %s ontbreekt", w)
			return
		}
		if got[i].text != w {
			add(d.path, got[i].line, "kop ## %s waar ## %s verwacht wordt", got[i].text, w)
			return
		}
	}
	if len(got) > len(want) {
		add(d.path, got[len(want)].line, "extra kop ## %s", got[len(want)].text)
	}
}

// checkNotNow checks the bullets of "## Bewust niet": one unique ID each and, with known set,
// a "→ <area>" that exists.
func checkNotNow(d doc, idRe, anyRe *regexp.Regexp, letter string, known map[string]bool, add func(string, int, string, ...any)) {
	from, to, ok := d.section("Bewust niet")
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

// checkTenets checks the "## <letter>. X boven Y" headings and their evidence.
func checkTenets(d doc, add func(string, int, string, ...any)) {
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
		if n := strings.Count(title, " boven "); n != 1 {
			add(d.path, h.line, "kop heeft %d keer ' boven ', precies 1 verwacht", n)
		}
		if strings.Contains(title, "(vervallen)") {
			continue
		}
		if links := evidenceLinks(d, h.line); links < 2 {
			add(d.path, h.line, "geen *Bewijs*-regel met minstens 2 links (gevonden: %d)", links)
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

// evidenceLinks counts the links of the *Bewijs* line (and its continuation) of the section at line.
func evidenceLinks(d doc, line int) int {
	for i := line; i < len(d.lines) && !strings.HasPrefix(d.lines[i], "## "); i++ {
		if strings.HasPrefix(d.lines[i], "*Bewijs*") {
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
