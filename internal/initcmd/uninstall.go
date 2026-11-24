package initcmd

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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

	plan, err := uninstallPlan(root, changes)
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
	return nil
}

// uninstallPlan plans the removal of every unit in changes, plus the manifest and
// created.toml themselves.
func uninstallPlan(root string, changes []reconcile.Change) (map[string]*planned, error) {
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
				return nil, err
			}
			p.delete = true
		case packs.KindBlock:
			p, err := load(agentsMDPath)
			if err != nil {
				return nil, err
			}
			if p.new, err = agentsmd.Remove(p.new, c.Key.ID); err != nil {
				return nil, err
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
			return nil, err
		}
		// Without settings.json there is nothing to remove from; it is not created.
		if doc != nil {
			p, err := load(settingsPath)
			if err != nil {
				return nil, err
			}
			if hooksTouched {
				if err := settings.Apply(doc, nil); err != nil {
					return nil, err
				}
			}
			if err := settings.ApplyAllow(doc, nil, allowDrop); err != nil {
				return nil, err
			}
			out, err := orderedjson.Marshal(doc)
			if err != nil {
				return nil, err
			}
			if !jsonEqual(p.old, out) {
				p.new = out
			}
		}
	}

	for _, rel := range []string{manifest.Path, created.Path} {
		p, err := load(rel)
		if err != nil {
			return nil, err
		}
		p.delete = true
	}
	return plan, nil
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
