package initcmd

import (
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
)

// fillWorkflows fills the @@<key>@@ placeholders of every workflow unit (plan M0 (b),
// design choice 13). A workflow that cannot be filled (a key not set, a
// config that cannot be read) is reported, never fatal: a new one is left out, and one
// that installed returns stays as it is, so a passing problem never deletes it.
// installed returns the content on disk of a workflow the manifest lists.
func fillWorkflows(w io.Writer, desired []packs.Unit, lookup packs.Lookup, installed func(rel string) ([]byte, bool)) []packs.Unit {
	out := desired[:0:0]
	for _, u := range desired {
		if u.Kind != packs.KindFile || !strings.HasPrefix(u.Path, packs.WorkflowPrefix) {
			out = append(out, u)
			continue
		}
		name := path.Base(u.Path)
		content, missing, err := packs.Fill(u.Content, lookup)
		reason := ""
		switch {
		case err != nil:
			reason = err.Error()
		case len(missing) > 0:
			reason = strings.Join(missing, ", ") + " is not set"
		}
		if reason == "" {
			u.Content = content
			out = append(out, u)
			continue
		}
		if old, ok := installed(u.Path); ok {
			_, _ = fmt.Fprintf(w, "workflow %s not updated: %s; the installed version stays\n", name, reason)
			u.Content = old
			out = append(out, u)
			continue
		}
		_, _ = fmt.Fprintf(w, "workflow %s not installed: %s\n", name, reason)
	}
	return out
}

// goLookup answers config keys with internal/config, so the defaults have one source. It
// reads lighthouse.toml as init will leave it, parsed once on the first key.
type goLookup struct {
	root   string
	tables []projectconfig.Table // what lighthouse.toml gets; see configTables
	cfg    *config.Config
	err    error
	done   bool
}

func (g *goLookup) setup() (*config.Config, error) {
	old, _, err := readFile(g.root, configPath)
	if err != nil {
		return nil, err
	}
	proposed, _, err := proposeConfig(old, g.tables)
	if err != nil {
		return nil, err
	}
	return config.Parse(proposed, configPath)
}

func (g *goLookup) lookup(key string) (string, bool, error) {
	if !g.done {
		g.cfg, g.err = g.setup()
		g.done = true
	}
	if g.err != nil {
		return "", false, g.err
	}
	v, err := config.Lookup(g.cfg, key)
	if errors.Is(err, config.ErrNotSet) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	lines, err := config.Lines(key, v)
	if err != nil {
		return "", false, err
	}
	return strings.Join(lines, "\n"), true, nil
}
