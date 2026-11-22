package initcmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// defaultLevel is the level of a repo whose lighthouse.toml has no `level`.
const defaultLevel = "strict"

// levelFiles collects the levels.toml of the chosen packs, by package name. A package
// without one (a test fixture) is left out.
func levelFiles(chosen []packs.Pack) map[string]levels.File {
	out := map[string]levels.File{}
	for _, p := range chosen {
		if p.Levels.Levels != nil {
			out[p.Name] = p.Levels
		}
	}
	return out
}

// resolveLevels reads the level (the flag, else lighthouse.toml, else strict) and the
// [components] overrides, and resolves them against the packs. With no levels.toml in
// any chosen package there is nothing to select: ok is false.
func resolveLevels(o Options, root string, files map[string]levels.File) (r levels.Resolved, ok bool, err error) {
	if len(files) == 0 {
		return levels.Resolved{}, false, nil
	}
	var level string
	overrides := map[string]levels.Stand{}
	cfgPath := filepath.Join(root, configPath)
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return levels.Resolved{}, false, err
		}
		level = cfg.Level
		for name, s := range cfg.Components {
			stand := levels.Stand(s)
			if err := stand.UnmarshalText([]byte(s)); err != nil {
				return levels.Resolved{}, false, fmt.Errorf("[components] %s: %w", name, err)
			}
			overrides[name] = stand
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return levels.Resolved{}, false, statErr
	}
	if o.Level != "" {
		level = o.Level
	}
	if level == "" {
		level = defaultLevel
	}
	list := make([]levels.File, 0, len(files))
	for _, f := range files {
		list = append(list, f)
	}
	r, err = levels.Resolve(list, level, overrides)
	return r, err == nil, err
}

// selectUnits drops the units whose component is off. A base unit always stays, and so
// does a component that is on or log. A component that no levels.toml names stays on and
// is reported once on warn; this is stderr, never stdout.
func selectUnits(desired []packs.Unit, files map[string]levels.File, r levels.Resolved, warn io.Writer) ([]packs.Unit, error) {
	named := r.Names()
	var kept []packs.Unit
	reported := map[string]bool{}
	for _, u := range desired {
		f, has := files[u.Package]
		if !has {
			kept = append(kept, u)
			continue
		}
		name, base, err := packs.Component(u, f)
		if err != nil {
			return nil, err
		}
		if base {
			kept = append(kept, u)
			continue
		}
		if !slices.Contains(named, name) && !reported[name] {
			reported[name] = true
			_, _ = fmt.Fprintf(warn, "component %s is in no levels.toml; it stays on\n", name)
		}
		if r.Stand(name) == levels.StandOff {
			continue
		}
		kept = append(kept, u)
	}
	return kept, nil
}

// componentsUnit is the unit for .lighthouse/components.toml: no marker, so the file
// stays plain TOML.
func componentsUnit(r levels.Resolved, since string) packs.Unit {
	i := levels.Installed{Level: r.Level, Since: since, Components: r.Components}
	return packs.Unit{Kind: packs.KindFile, Package: "core", Path: levels.InstalledPath, Content: i.Marshal(), Mode: 0o644}
}

// installedSince is the since of the components.toml on disk when it has the same level,
// else now.
func installedSince(root, level string, now func() time.Time) (string, error) {
	prev, ok, err := levels.ReadInstalled(root)
	if err != nil {
		return "", err
	}
	if ok && prev.Level == level && prev.Since != "" {
		return prev.Since, nil
	}
	return now().UTC().Format(time.RFC3339), nil
}
