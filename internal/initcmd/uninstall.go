package initcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/agentsmd"
	"github.com/CorneeldH/lighthouse/internal/created"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/reconcile"
	"github.com/CorneeldH/lighthouse/internal/settings"
)

// Uninstall removes what init installed and recorded; returns 0 done, 1 refused, declined or failed.
//
// Every unit of the manifest gets an empty desired state, so reconcile gives Remove for
// an untouched or missing unit and Conflict for a locally changed one, the rule of
// `init --upgrade`. A conflict refuses the whole run unless --force. Nothing is written
// before the diff is shown and the question answered (or --yes).
func Uninstall(o Options) int {
	if o.In == nil {
		o.In = strings.NewReader("")
	}
	if err := uninstall(o); err != nil {
		_, _ = fmt.Fprintf(o.Err, "lighthouse uninstall: %v\n", err)
		return 1
	}
	return 0
}

func uninstall(o Options) error {
	root, err := repoRoot(o.Dir)
	if err != nil {
		return err
	}
	m, err := manifest.Read(root)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("lighthouse is not installed here (%s not found)", manifest.Path)
	}
	if err != nil {
		return err
	}
	cur, err := current(root, nil, m)
	if err != nil {
		return err
	}
	changes := reconcile.Plan(nil, m, cur)
	if !o.Force {
		// Unlike --upgrade there is no keptRetired exception: uninstall would remove it.
		var conflicts []reconcile.Change
		for _, c := range changes {
			if c.Action == reconcile.Conflict {
				conflicts = append(conflicts, c)
			}
		}
		if len(conflicts) > 0 {
			for _, c := range conflicts {
				printConflict(o.Out, c)
			}
			return fmt.Errorf("refused: %d locally modified unit(s); rerun with --force to remove them", len(conflicts))
		}
	}

	plan, notes, err := uninstallPlan(root, changes)
	if err != nil {
		return err
	}
	paths := uninstallOrder(plan)
	showDiffs(o, plan, paths)
	in := bufio.NewReader(o.In)
	if !confirm(o, func() bool { return ask(o.Out, in, "Remove lighthouse? [y/N] ") }) {
		return errDeclined
	}
	if err := applyAll(root, plan, paths, uninstallApply); err != nil {
		return err
	}
	// A unit that was already gone left its directory behind; it is lighthouse's too.
	for rel, p := range plan {
		if p.delete && !p.existed {
			if err := removeEmptyDirs(root, rel); err != nil {
				return err
			}
		}
	}
	_, _ = fmt.Fprintf(o.Out, "Removed %d file(s).\n", len(paths))
	for _, n := range notes {
		_, _ = fmt.Fprintln(o.Err, n)
	}
	return nil
}

// uninstallPlan plans the removal of every unit in changes, the undoing of what
// created.toml records, and the manifest and created.toml themselves. The notes are
// for the report after a successful run.
func uninstallPlan(root string, changes []reconcile.Change) (map[string]*planned, []string, error) {
	plan := map[string]*planned{}
	load := func(rel string) (*planned, error) {
		if p, ok := plan[rel]; ok {
			return p, nil
		}
		b, ok, err := readFile(root, rel)
		if err != nil {
			return nil, err
		}
		p := &planned{old: b, new: b, existed: ok}
		plan[rel] = p
		return p, nil
	}

	var hooksTouched bool
	var allowDrop []string
	for _, c := range changes {
		switch c.Key.Kind {
		case packs.KindFile:
			p, err := load(c.Key.Path)
			if err != nil {
				return nil, nil, err
			}
			p.delete = true
		case packs.KindBlock:
			p, err := load(agentsMDPath)
			if err != nil {
				return nil, nil, err
			}
			if p.new, err = agentsmd.Remove(p.new, c.Key.ID); err != nil {
				return nil, nil, err
			}
		case packs.KindHook:
			hooksTouched = true
		case packs.KindPermission:
			// A rule outside the pattern is the project's, even when the manifest names it.
			if packs.IsLighthouseRule(c.Key.ID) {
				allowDrop = append(allowDrop, c.Key.ID)
			}
		}
	}

	if hooksTouched || len(allowDrop) > 0 {
		doc, _, err := readSettings(root)
		if err != nil {
			return nil, nil, err
		}
		// Without settings.json there is nothing to remove from; it is not created.
		if doc != nil {
			p, err := load(settingsPath)
			if err != nil {
				return nil, nil, err
			}
			if hooksTouched {
				if err := settings.Apply(doc, nil); err != nil {
					return nil, nil, err
				}
			}
			if err := settings.ApplyAllow(doc, nil, allowDrop); err != nil {
				return nil, nil, err
			}
			out, err := orderedjson.Marshal(doc)
			if err != nil {
				return nil, nil, err
			}
			if !jsonEqual(p.old, out) {
				p.new = out
			}
		}
	}

	c, found, err := created.Read(root)
	if err != nil {
		return nil, nil, err
	}
	var notes []string
	if found {
		notes, err = revertCreated(root, c, plan, load)
	} else {
		notes, err = leftBehind(load)
	}
	if err != nil {
		return nil, nil, err
	}

	for _, rel := range []string{manifest.Path, created.Path} {
		p, err := load(rel)
		if err != nil {
			return nil, nil, err
		}
		p.delete = true
	}
	return plan, notes, nil
}

// uninstallOrder is every changed path of plan, sorted, with manifest.Path and then
// created.Path last: a run that fails halfway leaves both, so the next uninstall still
// sees the remainder.
func uninstallOrder(plan map[string]*planned) []string {
	var paths []string
	for p, pl := range plan {
		if p != manifest.Path && p != created.Path && pl.changes() {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range []string{manifest.Path, created.Path} {
		if plan[p].changes() {
			paths = append(paths, p)
		}
	}
	return paths
}

// uninstallApply is apply with removeEmptyDirs instead of removeEmptyParents.
func uninstallApply(root, rel string, p *planned) error {
	if !p.delete {
		return apply(root, rel, p)
	}
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return removeEmptyDirs(root, rel)
}

// removeEmptyDirs removes the directories that removing rel left empty, from its parent
// up to root, which stays. It stops at the first directory with an entry in it. A
// directory whose only entry is a __pycache__ directory counts as empty, as in
// removeEmptyParents. Unlike that one it has no install roots: after uninstall they go too.
func removeEmptyDirs(root, rel string) error {
	for dir := path.Dir(filepath.ToSlash(rel)); dir != "." && dir != "/"; dir = path.Dir(dir) {
		abs := filepath.Join(root, filepath.FromSlash(dir))
		entries, err := os.ReadDir(abs)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if len(entries) == 1 && entries[0].IsDir() && entries[0].Name() == "__pycache__" {
			if err := os.RemoveAll(filepath.Join(abs, "__pycache__")); err != nil {
				return err
			}
		} else if len(entries) > 0 {
			return nil
		}
		if err := os.Remove(abs); err != nil {
			return err
		}
	}
	return nil
}

// revertCreated plans the undoing of what created.toml records, on top of the unit plan.
// It returns lines for the report (kept / left).
//
// Recorded things are not under the conflict gate: what changed since init stays and
// gets a note. The exception is lighthouse.toml, which goes even when edited: without
// lighthouse it has no reader.
func revertCreated(root string, c created.Created, plan map[string]*planned, load func(string) (*planned, error)) (notes []string, err error) {
	inFiles := func(rel string) bool { return slices.Contains(c.Files, rel) }
	keptOther := func(rel string) {
		notes = append(notes, fmt.Sprintf("kept: %s (init created it; it holds other content now)", rel))
	}

	if inFiles(configPath) {
		p, err := load(configPath)
		if err != nil {
			return nil, err
		}
		p.delete = p.existed
	}

	agents, err := load(agentsMDPath)
	if err != nil {
		return nil, err
	}
	if inFiles(agentsMDPath) && agents.existed {
		if len(bytes.TrimSpace(agents.new)) == 0 {
			agents.delete = true
		} else {
			keptOther(agentsMDPath)
		}
	}

	if inFiles(claudeMDPath) {
		p, err := load(claudeMDPath)
		if err != nil {
			return nil, err
		}
		switch {
		case !p.existed:
		case string(p.new) != "@AGENTS.md\n":
			notes = append(notes, fmt.Sprintf("kept: %s (changed since init)", claudeMDPath))
		case agents.existed && !agents.delete:
			notes = append(notes, fmt.Sprintf("kept: %s (%s stays)", claudeMDPath, agentsMDPath))
		default:
			p.delete = true
		}
	}

	if len(c.Gitattributes) > 0 || inFiles(gitattributesPath) {
		p, err := load(gitattributesPath)
		if err != nil {
			return nil, err
		}
		if p.existed {
			out := p.new
			for _, rule := range c.Gitattributes {
				out = removeLastLine(out, rule)
			}
			switch {
			case inFiles(gitattributesPath) && len(bytes.TrimSpace(out)) == 0:
				p.delete = true
			default:
				p.new = out
				if inFiles(gitattributesPath) {
					keptOther(gitattributesPath)
				}
			}
		}
	}

	settingsNotes, err := revertSettings(c, inFiles(settingsPath), load)
	if err != nil {
		return nil, err
	}
	return append(notes, settingsNotes...), nil
}

// revertSettings removes the "language" and the emptied containers that created.toml
// records from .claude/settings.json, and the file itself when init created it and
// nothing is left. A settings.json that does not exist is not touched.
func revertSettings(c created.Created, inFiles bool, load func(string) (*planned, error)) (notes []string, err error) {
	containers := map[string]bool{}
	for _, k := range c.SettingsKeys {
		containers[k] = true
	}
	if inFiles {
		containers["hooks"], containers["permissions"], containers["permissions.allow"] = true, true, true
	}
	if c.SettingsLanguage == "" && !containers["hooks"] && !containers["permissions"] && !containers["permissions.allow"] {
		return nil, nil
	}
	p, err := load(settingsPath)
	if err != nil || !p.existed {
		return nil, err
	}
	doc, err := parseSettings(p.new)
	if err != nil {
		return nil, err
	}

	if c.SettingsLanguage != "" {
		if v, ok := doc.Get("language"); ok {
			if s, isStr := v.(string); isStr && s == c.SettingsLanguage {
				doc.Delete("language")
			} else {
				got, _ := orderedjson.Canonical(v)
				notes = append(notes, fmt.Sprintf("kept: %s \"language\" is %s; init set %q", settingsPath, got, c.SettingsLanguage))
			}
		}
	}
	if pv, ok := doc.Get("permissions"); ok {
		if perms, isObj := pv.(*orderedjson.Object); isObj {
			if av, ok := perms.Get("allow"); ok && containers["permissions.allow"] {
				if allow, isArr := av.([]any); isArr && len(allow) == 0 {
					perms.Delete("allow")
				}
			}
			if containers["permissions"] && len(perms.Keys()) == 0 {
				doc.Delete("permissions")
			}
		}
	}
	if hv, ok := doc.Get("hooks"); ok && containers["hooks"] {
		if hooks, isObj := hv.(*orderedjson.Object); isObj && len(hooks.Keys()) == 0 {
			doc.Delete("hooks")
		}
	}

	if inFiles && len(doc.Keys()) == 0 {
		p.delete = true
		return notes, nil
	}
	if inFiles {
		notes = append(notes, fmt.Sprintf("kept: %s (init created it; it holds other content now)", settingsPath))
	}
	out, err := orderedjson.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if jsonEqual(p.old, out) {
		p.new = p.old
	} else {
		p.new = out
	}
	return notes, nil
}

// leftBehind names what init may have written outside the units, for a repo without
// created.toml: uninstall cannot tell init's from the project's, so it touches none.
func leftBehind(load func(string) (*planned, error)) ([]string, error) {
	const tail = " — init may have written it; remove it yourself if so"
	var notes []string
	get := func(rel string) ([]byte, bool, error) {
		p, err := load(rel)
		if err != nil {
			return nil, false, err
		}
		return p.new, p.existed, nil
	}

	if _, ok, err := get(configPath); err != nil {
		return nil, err
	} else if ok {
		notes = append(notes, "left: "+configPath+tail)
	}
	if b, ok, err := get(claudeMDPath); err != nil {
		return nil, err
	} else if ok && hasLine(b, "@AGENTS.md") {
		notes = append(notes, "left: "+claudeMDPath+tail)
	}
	if b, ok, err := get(gitattributesPath); err != nil {
		return nil, err
	} else if ok && (hasLine(b, "*.sh eol=lf") || hasLine(b, ".claude/hooks/lighthouse/** eol=lf")) {
		notes = append(notes, "left: "+gitattributesPath+tail)
	}
	if b, ok, err := get(settingsPath); err != nil {
		return nil, err
	} else if ok {
		doc, err := parseSettings(b)
		if err != nil {
			return nil, err
		}
		if _, has := doc.Get("language"); has {
			notes = append(notes, "left: "+settingsPath+` "language"`+tail)
		}
	}
	return notes, nil
}

// parseSettings parses settings.json content that must be a JSON object.
func parseSettings(b []byte) (*orderedjson.Object, error) {
	v, err := orderedjson.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", settingsPath, err)
	}
	obj, isObj := v.(*orderedjson.Object)
	if !isObj {
		return nil, fmt.Errorf("%s: not a JSON object", settingsPath)
	}
	return obj, nil
}

// removeLastLine removes the last line of b that is exactly rule, with its line ending
// (LF or CRLF). b is returned unchanged when no line matches.
func removeLastLine(b []byte, rule string) []byte {
	lines := strings.SplitAfter(string(b), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSuffix(strings.TrimSuffix(lines[i], "\n"), "\r") == rule {
			return []byte(strings.Join(append(lines[:i:i], lines[i+1:]...), ""))
		}
	}
	return b
}
