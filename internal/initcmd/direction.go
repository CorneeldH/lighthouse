package initcmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/CorneeldH/lighthouse/internal/config"
	labelsync "github.com/CorneeldH/lighthouse/internal/labels"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// directionKeys are the [paths] keys of the direction documents; one of them set is an opt-in.
var directionKeys = []string{"strategy", "tenets", "areas"}

var headingMarker = regexp.MustCompile(`\{\{heading:([a-z0-9_]+)\}\}`)

// wantsDirection is whether this run seeds the direction documents: --direction, or a
// lighthouse.toml that sets at least one of paths.strategy, paths.tenets and paths.areas.
// A missing or unparseable lighthouse.toml sets nothing; planConfig reports a broken one.
func wantsDirection(root string, o Options) bool {
	if o.Direction {
		return true
	}
	b, ok, err := readFile(root, configPath)
	if err != nil || !ok {
		return false
	}
	var v map[string]any
	md, err := toml.Decode(string(b), &v)
	if err != nil {
		return false
	}
	for _, k := range directionKeys {
		if md.IsDefined("paths", k) {
			return true
		}
	}
	return false
}

// safeRel rejects a path that is absolute, not clean or leaves the repository, so nothing is
// read or written outside it. key names the setting in the error.
func safeRel(key, target string) error {
	if filepath.IsAbs(target) || path.IsAbs(target) || path.Clean(target) != target || slices.Contains(strings.Split(target, "/"), "..") {
		return fmt.Errorf("lighthouse.toml: %s %q must be a clean relative path inside the repository", key, target)
	}
	return nil
}

// directionConfig is the part of the lighthouse.toml this run writes that seeding reads.
type directionConfig struct {
	Project struct {
		Language string `toml:"language"`
	} `toml:"project"`
	Paths struct {
		Labels   string `toml:"labels"`
		Strategy string `toml:"strategy"`
		Tenets   string `toml:"tenets"`
		Areas    string `toml:"areas"`
	} `toml:"paths"`
	Labels struct {
		AreaPrefix string `toml:"area_prefix"`
	} `toml:"labels"`
}

// directionPlan seeds the direction documents into the plan: the strategy, the tenets and one
// area file per area label of the taxonomy, each from its template in tmpl, only where the file
// is missing. An existing file is left as it is, with one line on stderr; a seeded file is the
// project's, so it is not in the manifest. It reads the lighthouse.toml this run writes (planConfig
// has run), and the headings of headings, the table this run installs: the one on disk may be
// older. Every path is checked before anything is planned.
func directionPlan(root string, o Options, tmpl map[string][]byte, headings []byte, load func(string) (*planned, error)) error {
	cp, err := load(configPath)
	if err != nil {
		return err
	}
	var cfg directionConfig
	md, err := toml.Decode(string(cp.new), &cfg)
	if err != nil {
		return fmt.Errorf("lighthouse.toml: %w", err)
	}
	for key, value := range map[string]string{"labels": cfg.Paths.Labels, "strategy": cfg.Paths.Strategy, "tenets": cfg.Paths.Tenets, "areas": cfg.Paths.Areas} {
		if md.IsDefined("paths", key) {
			if err := safeRel("paths."+key, value); err != nil {
				return err
			}
		}
	}
	if headings == nil {
		return errors.New("--direction: the core package has no headings.toml")
	}
	table, err := config.ParseHeadings(headings)
	if err != nil {
		return fmt.Errorf("headings.toml of the core package: %w", err)
	}
	language := cfg.Project.Language
	if language == "" {
		language = "en"
	}
	render := func(name string) ([]byte, error) {
		body, ok := tmpl[name]
		if !ok {
			return nil, fmt.Errorf("--direction: the core package has no templates/%s", name)
		}
		var firstErr error
		out := headingMarker.ReplaceAllStringFunc(string(body), func(m string) string {
			h, err := table.Heading(headingMarker.FindStringSubmatch(m)[1], language)
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("templates/%s: %w", name, err)
			}
			return h
		})
		return []byte(out), firstErr
	}

	type seed struct {
		rel, template string
	}
	var seeds []seed
	if cfg.Paths.Strategy != "" {
		seeds = append(seeds, seed{cfg.Paths.Strategy, "strategy.md"})
	}
	if cfg.Paths.Tenets != "" {
		seeds = append(seeds, seed{cfg.Paths.Tenets, "tenets.md"})
	}
	keepAreas := false
	if cfg.Paths.Areas != "" {
		names, why, err := areaNames(cfg, load)
		if err != nil {
			return err
		}
		for _, n := range names {
			if strings.Contains(n, "/") {
				return fmt.Errorf("%s: area label %s%s cannot name a file", cfg.Paths.Labels, cfg.Labels.AreaPrefix, n)
			}
			rel := path.Join(cfg.Paths.Areas, n+".md")
			if safeRel("", rel) != nil || path.Dir(rel) != cfg.Paths.Areas {
				return fmt.Errorf("%s: area label %s%s gives no file inside %s", cfg.Paths.Labels, cfg.Labels.AreaPrefix, n, cfg.Paths.Areas)
			}
			seeds = append(seeds, seed{rel, "area.md"})
		}
		if len(names) == 0 {
			// The check reads the areas directory; an empty one keeps it green.
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(cfg.Paths.Areas))); errors.Is(err, os.ErrNotExist) {
				keepAreas = true
			}
			if o.Direction {
				_, _ = fmt.Fprintf(o.Err, "direction: %s; no area files seeded\n", why)
			}
		}
	}

	for _, s := range seeds {
		body, err := render(s.template)
		if err != nil {
			return err
		}
		p, err := load(s.rel)
		if err != nil {
			return err
		}
		if p.existed {
			_, _ = fmt.Fprintf(o.Err, "direction: %s exists; left as it is\n", s.rel)
			continue
		}
		p.new, p.mode = body, 0o644
	}
	if keepAreas {
		p, err := load(path.Join(cfg.Paths.Areas, ".gitkeep"))
		if err != nil {
			return err
		}
		if !p.existed {
			p.new, p.mode = []byte{}, 0o644
		}
	}
	return nil
}

// areaNames are the area labels of the taxonomy without their prefix: the taxonomy is
// paths.labels and the prefix labels.area_prefix, both of the lighthouse.toml this run writes,
// as `lighthouse direction check` reads them, and the taxonomy as this run leaves it (a seeded
// labels.md counts). Without them, why says what is missing.
func areaNames(cfg directionConfig, load func(string) (*planned, error)) (names []string, why string, err error) {
	if cfg.Paths.Labels == "" || cfg.Labels.AreaPrefix == "" {
		return nil, "no label taxonomy ([paths] labels and [labels] area_prefix in lighthouse.toml)", nil
	}
	p, err := load(cfg.Paths.Labels)
	if err != nil {
		return nil, "", err
	}
	if p.new == nil {
		return nil, fmt.Sprintf("no label taxonomy at %s", cfg.Paths.Labels), nil
	}
	for _, l := range labelsync.ParseTaxonomy(string(p.new)) {
		if n, ok := strings.CutPrefix(l.Name, cfg.Labels.AreaPrefix); ok {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Sprintf("no area labels (prefix %q) in %s", cfg.Labels.AreaPrefix, cfg.Paths.Labels), nil
	}
	return names, "", nil
}

// headingsOf is the content of the headings table among the units this run installs, or nil.
func headingsOf(desired []packs.Unit) []byte {
	for _, u := range desired {
		if u.Kind == packs.KindFile && u.Path == packs.HeadingsPath {
			return u.Content
		}
	}
	return nil
}
