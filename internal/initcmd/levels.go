package initcmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// defaultLevel is the level of an initialised repo whose lighthouse.toml has no `level`;
// newLevel is what a first init picks when neither the flag nor lighthouse.toml says.
const (
	defaultLevel = "strict"
	newLevel     = "light"
)

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

// levelInputs reads the level (the flag, else lighthouse.toml, else strict, or light on a
// first init), the level lighthouse.toml itself names, and the [components] overrides.
func levelInputs(o Options, root string, initialized bool) (level, fileLevel string, overrides map[string]levels.Stand, err error) {
	overrides = map[string]levels.Stand{}
	cfgPath := filepath.Join(root, configPath)
	if _, statErr := os.Stat(cfgPath); statErr == nil {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			return "", "", nil, err
		}
		level, fileLevel = cfg.Level, cfg.Level
		for name, s := range cfg.Components {
			stand := levels.Stand(s)
			if err := stand.UnmarshalText([]byte(s)); err != nil {
				return "", "", nil, fmt.Errorf("[components] %s: %w", name, err)
			}
			overrides[name] = stand
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return "", "", nil, statErr
	}
	if o.Level != "" {
		level = o.Level
	}
	if level == "" {
		level = defaultLevel
		if !initialized {
			level = newLevel
		}
	}
	return level, fileLevel, overrides, nil
}

// levelSetsLabels reports whether the level has the labels flag and this run is a first
// init or a switch. It decides before the plan is built, so it validates nothing: a bad
// level or override is reported by resolveLevels. Any doubt is false, never an error.
func levelSetsLabels(o Options, root string, initialized bool, files map[string]levels.File) bool {
	if len(files) == 0 {
		return false
	}
	level, _, overrides, err := levelInputs(o, root, initialized)
	if err != nil {
		return false
	}
	r, err := levels.Resolve(fileList(files), level, overrides)
	if err != nil || !r.Flags["labels"] {
		return false
	}
	prev, found, err := levels.ReadInstalled(root)
	// A repo initialised before levels has a manifest but no components.toml: that is
	// no switch, so only a recorded level that differs counts.
	return err == nil && (!initialized || (found && prev.Level != level))
}

// resolveLevels resolves the level and the [components] overrides against the packs.
// With no levels.toml in any chosen package there is nothing to select: ok is false.
// write is the level that lighthouse.toml must get: set when the flag differs from the
// file, or on a first init that found no level; "" when the file stays as it is (an
// upgrade never adds the key).
func resolveLevels(o Options, root string, initialized bool, files map[string]levels.File, desired []packs.Unit) (r levels.Resolved, ok bool, write string, err error) {
	if len(files) == 0 {
		return levels.Resolved{}, false, "", nil
	}
	level, fileLevel, overrides, err := levelInputs(o, root, initialized)
	if err != nil {
		return levels.Resolved{}, false, "", err
	}
	if level != fileLevel && (o.Level != "" || !initialized) {
		write = level
	}
	warnings, err := validate(fileList(files), level, overrides, loggableFunc(desired, files))
	if err != nil {
		return levels.Resolved{}, false, "", err
	}
	for _, w := range warnings {
		_, _ = fmt.Fprintln(o.Err, w)
	}
	r, err = levels.Resolve(fileList(files), level, overrides)
	return r, err == nil, write, err
}

func fileList(files map[string]levels.File) []levels.File {
	list := make([]levels.File, 0, len(files))
	for _, f := range files {
		list = append(list, f)
	}
	return list
}

// loggableFunc reports whether a component is a Go hook: it has a hook registration whose
// command goes through run.sh. A skill or a shell hook of a language pack has none.
func loggableFunc(desired []packs.Unit, files map[string]levels.File) func(string) bool {
	loggable := map[string]bool{}
	for _, u := range desired {
		f, has := files[u.Package]
		if !has || u.Kind != packs.KindHook || !strings.Contains(u.ID, "/run.sh ") {
			continue
		}
		if name, base, err := packs.Component(u, f); err == nil && !base {
			loggable[name] = true
		}
	}
	return func(c string) bool { return loggable[c] }
}

// validate checks the level and the overrides before anything is planned. Errors: an
// unknown level or component name, an override on a base component, log on a component
// that is not a Go hook, off on a skill that an on skill hands over to. Warnings: one per
// core gate whose stand is not on.
func validate(files []levels.File, level string, over map[string]levels.Stand, loggable func(component string) bool) (warnings []string, err error) {
	r, err := levels.Resolve(files, level, over)
	if err != nil {
		return nil, err
	}
	known := r.Names()
	for _, name := range slices.Sorted(maps.Keys(over)) {
		for _, f := range files {
			if slices.Contains(f.Base, name) {
				return nil, fmt.Errorf("[components] %s: a base component accepts no override", name)
			}
		}
		if !slices.Contains(known, name) {
			return nil, fmt.Errorf("[components] unknown component %q (known: %s)", name, strings.Join(known, ", "))
		}
		if over[name] == levels.StandLog && !loggable(name) {
			return nil, fmt.Errorf("[components] %s: log is only for a Go hook of core; a skill or shell hook knows off and on", name)
		}
	}
	for _, f := range files {
		for _, caller := range slices.Sorted(maps.Keys(f.Calls)) {
			if r.Stand(caller) != levels.StandOn {
				continue
			}
			for _, callee := range f.Calls[caller] {
				if r.Stand(callee) == levels.StandOff {
					return nil, fmt.Errorf("%s is on and hands over to %s, which is off", caller, callee)
				}
			}
		}
	}
	for _, g := range levels.CoreGates {
		if st := r.Stand(g); st != levels.StandOn {
			warnings = append(warnings, fmt.Sprintf("lighthouse: %s is %s — this repo works outside the chain", g, st))
		}
	}
	return warnings, nil
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

// switched reports whether this run changes the level: the first init (nothing recorded),
// or a recorded level that differs from the effective one.
func switched(installed levels.Installed, found bool, level string) bool {
	return !found || installed.Level != level
}

// installedSince is the since of the components.toml on disk when it has the same level,
// else now.
func installedSince(installed levels.Installed, found bool, level string, now func() time.Time) string {
	if !switched(installed, found, level) && installed.Since != "" {
		return installed.Since
	}
	return now().UTC().Format(time.RFC3339)
}

// levelSummary is the stdout line after a write: the level, and the next one to move to.
// Beyond the last level of the order it names the one below; without an order, no hint.
func levelSummary(files map[string]levels.File, level string) string {
	var order []string
	for _, f := range files {
		if len(f.Order) > 0 {
			order = f.Order
		}
	}
	i := slices.Index(order, level)
	switch {
	case i < 0:
		return fmt.Sprintf("Level: %s.", level)
	case i+1 < len(order):
		return fmt.Sprintf("Level: %s. Scale up with: lighthouse init --upgrade --level %s", level, order[i+1])
	case i > 0:
		return fmt.Sprintf("Level: %s. Scale down with: lighthouse init --upgrade --level %s", level, order[i-1])
	}
	return fmt.Sprintf("Level: %s.", level)
}

// filterBlocking removes from the hooks/blocking list every hook whose component is not
// on, so run.sh no longer treats a log or off hook as blocking. It only removes: it never
// adds a line, and a base hook (not a component) is not subject to a stand. The content
// was marked by packs.Load; a line that is not a hook name (a marker comment) stays.
func filterBlocking(u packs.Unit, r levels.Resolved) packs.Unit {
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(u.Content), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && !strings.HasPrefix(name, "#") && r.Stand(name) != levels.StandOn {
			continue
		}
		out.WriteString(line)
	}
	u.Content = []byte(out.String())
	return u
}

// filterBlockingUnits applies filterBlocking to the blocking list among units.
func filterBlockingUnits(units []packs.Unit, r levels.Resolved) []packs.Unit {
	out := make([]packs.Unit, len(units))
	for i, u := range units {
		if u.Kind == packs.KindFile && u.Path == packs.HookDir+"blocking" {
			u = filterBlocking(u, r)
		}
		out[i] = u
	}
	return out
}
