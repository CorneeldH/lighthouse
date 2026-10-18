package initcmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/CorneeldH/lighthouse/internal/agentsmd"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/reconcile"
	"github.com/CorneeldH/lighthouse/internal/settings"
)

// readFile returns the content of rel under root, and whether it exists.
func readFile(root, rel string) ([]byte, bool, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

// readSettings parses .claude/settings.json; nil when it does not exist.
func readSettings(root string) (*orderedjson.Object, []byte, error) {
	b, ok, err := readFile(root, settingsPath)
	if err != nil || !ok {
		return nil, nil, err
	}
	v, err := orderedjson.Parse(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", settingsPath, err)
	}
	obj, isObj := v.(*orderedjson.Object)
	if !isObj {
		return nil, nil, fmt.Errorf("%s: not a JSON object", settingsPath)
	}
	if _, err := settings.Registrations(obj); err != nil {
		return nil, nil, err
	}
	return obj, b, nil
}

// current reads the content of every unit in desired or in the manifest.
func current(root string, desired []packs.Unit, m manifest.Manifest) (map[packs.Key]reconcile.Current, error) {
	keys := map[packs.Key]bool{}
	for _, u := range desired {
		keys[u.Key()] = true
	}
	for _, e := range m.Units {
		keys[e.Key()] = true
	}
	agents, _, err := readFile(root, agentsMDPath)
	if err != nil {
		return nil, err
	}
	doc, _, err := readSettings(root)
	if err != nil {
		return nil, err
	}
	hooks := map[string][]byte{}
	allowed := map[string]bool{}
	if doc != nil {
		rules, err := settings.Allow(doc)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			allowed[r] = true
		}
		regs, err := settings.Registrations(doc)
		if err != nil {
			return nil, err
		}
		for _, r := range regs {
			cv, _ := r.Entry.Get("command")
			cmd, _ := cv.(string)
			if !packs.IsLighthouseCommand(cmd) {
				continue
			}
			id := packs.HookID(r.Event, r.Matcher, cmd)
			if _, seen := hooks[id]; seen {
				continue
			}
			c, err := orderedjson.Canonical(r.Entry)
			if err != nil {
				return nil, err
			}
			hooks[id] = c
		}
	}
	out := map[packs.Key]reconcile.Current{}
	for k := range keys {
		switch k.Kind {
		case packs.KindFile:
			b, ok, err := readFile(root, k.Path)
			if err != nil {
				return nil, err
			}
			out[k] = reconcile.Current{Content: b, Present: ok}
		case packs.KindBlock:
			body, found, err := agentsmd.Find(agents, k.ID)
			if err != nil {
				return nil, err
			}
			out[k] = reconcile.Current{Content: body, Present: found}
		case packs.KindHook:
			c, ok := hooks[k.ID]
			out[k] = reconcile.Current{Content: c, Present: ok}
		case packs.KindPermission:
			if allowed[k.ID] {
				out[k] = reconcile.Current{Content: []byte(k.ID), Present: true}
			} else {
				out[k] = reconcile.Current{}
			}
		}
	}
	return out, nil
}
