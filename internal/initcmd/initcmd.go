// Package initcmd runs `lighthouse init` and `lighthouse init --upgrade`: load the
// packages, decide per unit against the manifest, show a diff per file, ask, write.
//
// Nothing is written without approval or --yes. A locally changed unit refuses the
// whole run unless --force is given (spec, besluit 4).
package initcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/pmezard/go-difflib/difflib"

	"github.com/CorneeldH/lighthouse/internal/agentsmd"
	"github.com/CorneeldH/lighthouse/internal/deps"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/orderedjson"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
	"github.com/CorneeldH/lighthouse/internal/reconcile"
	"github.com/CorneeldH/lighthouse/internal/settings"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/pack"
)

const (
	settingsPath = ".claude/settings.json"
	agentsMDPath = "AGENTS.md"
	claudeMDPath = "CLAUDE.md"
	configPath   = "lighthouse.toml"
)

// Options configures one run.
type Options struct {
	Dir                 string // working directory; the root comes from git rev-parse --show-toplevel
	Upgrade, Force, Yes bool
	Version             string                 // "" means version.String()
	Packs               fs.FS                  // holds <name>/...; nil means pack.Files
	Deps                deps.Checker           // nil fields mean deps.Default()
	Lookup              packs.Lookup           // config values for workflow placeholders; nil runs the package's config reader
	HostTOML            string                 // "" means $LIGHTHOUSE_HOST_TOML, else ~/.config/lighthouse/host.toml
	Hostname            func() (string, error) // nil means os.Hostname
	TailscaleIP         func() (string, error) // nil runs `tailscale ip -4`
	In                  io.Reader
	Out, Err            io.Writer
}

// planned is the new state of one path: nil content means delete.
type planned struct {
	old, new []byte
	existed  bool
	delete   bool
	mode     fs.FileMode // 0 keeps the mode of an existing file, or 0o644 for a new one
}

// Run executes init or --upgrade and returns the exit code: 0 done or nothing to do,
// 1 refused, declined or failed.
func Run(o Options) int {
	if o.Version == "" {
		o.Version = version.String()
	}
	if o.Packs == nil {
		o.Packs = pack.Files
	}
	if o.Deps.LookPath == nil || o.Deps.Run == nil {
		o.Deps = deps.Default()
	}
	if o.In == nil {
		o.In = strings.NewReader("")
	}
	if o.HostTOML == "" {
		o.HostTOML = defaultHostTOML()
	}
	if o.Hostname == nil {
		o.Hostname = os.Hostname
	}
	if o.TailscaleIP == nil {
		o.TailscaleIP = tailscaleIP
	}
	if err := execute(o); err != nil {
		_, _ = fmt.Fprintf(o.Err, "lighthouse init: %v\n", err)
		return 1
	}
	return 0
}

var errDeclined = errors.New("nothing written")

func execute(o Options) error {
	root, err := repoRoot(o.Dir)
	if err != nil {
		return err
	}
	m, err := manifest.Read(root)
	initialized := true
	if errors.Is(err, fs.ErrNotExist) {
		initialized = false
	} else if err != nil {
		return err
	}
	switch {
	case o.Upgrade && !initialized:
		return errors.New("this repository is not initialized; run `lighthouse init` first")
	case !o.Upgrade && initialized:
		return errors.New("this repository is already initialized; use `lighthouse init --upgrade`")
	}

	names := []string{"core"}
	if o.Upgrade && len(m.Packages) > 0 {
		names = m.Packages
	}
	var desired []packs.Unit
	var reqs []packs.Requirement
	for _, name := range names {
		sub, err := fs.Sub(o.Packs, name)
		if err == nil {
			_, err = fs.Stat(sub, ".")
		}
		if err != nil {
			return fmt.Errorf("package %s is not in this binary", name)
		}
		p, err := packs.Load(sub, name, o.Version)
		if err != nil {
			return err
		}
		desired = append(desired, p.Units...)
		reqs = append(reqs, p.Requires...)
	}
	lookup := o.Lookup
	if lookup == nil {
		pl := &pythonLookup{packs: o.Packs, root: root}
		defer pl.close()
		lookup = pl.lookup
	}
	if desired, err = fillWorkflows(o.Err, desired, lookup); err != nil {
		return err
	}
	for _, miss := range o.Deps.Check(reqs) {
		_, _ = fmt.Fprintf(o.Err, "missing dependency: %s (needed by: %s)\n", miss.Tool, strings.Join(miss.NeededBy, ", "))
	}

	cur, err := current(root, desired, m)
	if err != nil {
		return err
	}
	changes := reconcile.Plan(desired, m, cur)
	if !o.Force {
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
			return fmt.Errorf("refused: %d locally modified unit(s); rerun with --force to overwrite", len(conflicts))
		}
	}

	plan, err := buildPlan(o, root, names, desired, changes)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(plan))
	for p, pl := range plan {
		if pl.changes() {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	host := proposeHost(o)
	if len(paths) == 0 && host == nil {
		_, _ = fmt.Fprintln(o.Out, "No changes.")
		warnMissingScripts(o.Err, root)
		return nil
	}
	for _, p := range paths {
		printDiff(o.Out, p, plan[p])
	}
	if host != nil {
		_, _ = fmt.Fprint(o.Out, unified(nil, host, "/dev/null", hostLabel(o.HostTOML)))
	}
	if !o.Yes {
		_, _ = fmt.Fprint(o.Out, "Apply these changes? [y/N] ")
		line, _ := bufio.NewReader(o.In).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			_, _ = fmt.Fprintln(o.Out)
			return errDeclined
		}
	}
	for _, p := range paths {
		if err := apply(root, p, plan[p]); err != nil {
			return err
		}
	}
	written := len(paths)
	if host != nil {
		if err := writeHost(o.HostTOML, host); err != nil {
			return err
		}
		written++
	}
	_, _ = fmt.Fprintf(o.Out, "Wrote %d file(s).\n", written)
	warnMissingScripts(o.Err, root)
	return nil
}

func (p planned) changes() bool {
	switch {
	case p.delete:
		return p.existed
	case !p.existed:
		return p.new != nil
	default:
		return !bytes.Equal(p.old, p.new) || p.mode != 0
	}
}

func repoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%s is not a git repository (or git failed)", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

func writes(a reconcile.Action, force bool) bool {
	return a == reconcile.Add || a == reconcile.Replace || (force && a == reconcile.Conflict)
}

func buildPlan(o Options, root string, names []string, desired []packs.Unit, changes []reconcile.Change) (map[string]*planned, error) {
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
	var allowWant, allowDrop []string
	for _, c := range changes {
		switch c.Key.Kind {
		case packs.KindFile:
			p, err := load(c.Key.Path)
			if err != nil {
				return nil, err
			}
			switch {
			case c.Desired != nil && writes(c.Action, o.Force):
				p.new, p.mode = c.Desired.Content, c.Desired.Mode
			case c.Desired == nil && (c.Action == reconcile.Remove || o.Force):
				p.delete = true
			case c.Desired != nil && c.Desired.Mode == 0o755 && p.existed:
				if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(c.Key.Path))); err == nil && info.Mode().Perm()&0o111 == 0 {
					p.mode = 0o755
				}
			}
		case packs.KindBlock:
			p, err := load(agentsMDPath)
			if err != nil {
				return nil, err
			}
			switch {
			case c.Desired != nil && writes(c.Action, o.Force):
				if p.new, err = agentsmd.Set(p.new, c.Key.ID, c.Desired.Content); err != nil {
					return nil, err
				}
			case c.Desired == nil && (c.Action == reconcile.Remove || o.Force):
				if p.new, err = agentsmd.Remove(p.new, c.Key.ID); err != nil {
					return nil, err
				}
			}
		case packs.KindHook:
			hooksTouched = true
		case packs.KindPermission:
			switch {
			case c.Desired != nil && writes(c.Action, o.Force):
				allowWant = append(allowWant, c.Key.ID)
			case c.Desired == nil && (c.Action == reconcile.Remove || o.Force):
				allowDrop = append(allowDrop, c.Key.ID)
			}
		}
	}

	if hooksTouched || len(allowWant) > 0 || len(allowDrop) > 0 {
		p, err := load(settingsPath)
		if err != nil {
			return nil, err
		}
		doc, _, err := readSettings(root)
		if err != nil {
			return nil, err
		}
		if doc == nil {
			doc = orderedjson.NewObject()
		}
		if hooksTouched {
			var want []packs.Registration
			for _, u := range desired {
				if u.Kind == packs.KindHook {
					want = append(want, *u.Hook)
				}
			}
			if err := settings.Apply(doc, want); err != nil {
				return nil, err
			}
		}
		if err := settings.ApplyAllow(doc, allowWant, allowDrop); err != nil {
			return nil, err
		}
		out, err := orderedjson.Marshal(doc)
		if err != nil {
			return nil, err
		}
		if !p.existed || !jsonEqual(p.old, out) {
			p.new = out
		}
	}

	// CLAUDE.md only when init creates AGENTS.md as well.
	agents, err := load(agentsMDPath)
	if err != nil {
		return nil, err
	}
	claude, err := load(claudeMDPath)
	if err != nil {
		return nil, err
	}
	switch {
	case !agents.existed && !claude.existed:
		claude.new = []byte("@AGENTS.md\n")
	case claude.existed && !hasLine(claude.old, "@AGENTS.md"):
		_, _ = fmt.Fprintln(o.Err, "warning: CLAUDE.md does not import AGENTS.md; add a line `@AGENTS.md` so Claude Code reads the lighthouse rules")
	}

	if err := planConfig(o, root, load); err != nil {
		return nil, err
	}

	mp, err := load(manifest.Path)
	if err != nil {
		return nil, err
	}
	nm := manifest.Manifest{Version: o.Version, Packages: names}
	for _, u := range desired {
		nm.Units = append(nm.Units, manifest.Entry{
			Kind: u.Kind, Path: u.Path, ID: u.ID, Package: u.Package, Version: o.Version,
			SHA256: manifest.Sum(u.Content),
		})
	}
	mp.new = manifest.Encode(nm)
	return plan, nil
}

// jsonEqual reports whether a and b hold the same JSON with the same key order,
// so a file that only differs in whitespace is not rewritten.
func jsonEqual(a, b []byte) bool {
	va, err := orderedjson.Parse(a)
	if err != nil {
		return false
	}
	ma, err := orderedjson.Marshal(va)
	return err == nil && bytes.Equal(ma, b)
}

func hasLine(b []byte, line string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == line {
			return true
		}
	}
	return false
}

// proposeConfig returns lighthouse.toml as init will leave it: old plus the missing
// core tables.
func proposeConfig(root string, old []byte) (out []byte, haveRepo bool, manual []string, err error) {
	repo := ""
	if url, err := exec.Command("git", "-C", root, "remote", "get-url", "origin").Output(); err == nil {
		repo, haveRepo = projectconfig.ForgeRepo(strings.TrimSpace(string(url)))
	}
	out, _, manual, err = projectconfig.AppendMissing(old, projectconfig.CoreTables(repo, haveRepo))
	return out, haveRepo, manual, err
}

func planConfig(o Options, root string, load func(string) (*planned, error)) error {
	p, err := load(configPath)
	if err != nil {
		return err
	}
	out, haveRepo, manual, err := proposeConfig(root, p.old)
	if err != nil {
		return err
	}
	p.new = out
	if !haveRepo {
		var v map[string]any
		md, err := toml.Decode(string(out), &v)
		if err == nil && !md.IsDefined("forge", "repo") {
			_, _ = fmt.Fprintln(o.Err, "warning: no GitHub remote 'origin': [forge] not proposed in lighthouse.toml; skills that need the forge will fail until you add it")
		}
	}
	for _, k := range manual {
		_, _ = fmt.Fprintf(o.Err, "warning: lighthouse.toml: add %s by hand; its table is not a plain [section]\n", k)
	}
	return nil
}

func printConflict(w io.Writer, c reconcile.Change) {
	what := c.Key.Path
	if c.Key.ID != "" {
		what += " [" + c.Key.ID + "]"
	}
	_, _ = fmt.Fprintf(w, "conflict: %s %s is changed locally\n", c.Key.Kind, what)
	var want []byte
	if c.Desired != nil {
		want = c.Desired.Content
	}
	from := "a/" + c.Key.Path + " (local)"
	if !c.Current.Present {
		from = "/dev/null (deleted locally)"
	}
	_, _ = fmt.Fprint(w, unified(c.Current.Content, want, from, "b/"+c.Key.Path+" (package)"))
}

func printDiff(w io.Writer, path string, p *planned) {
	from, to := "a/"+path, "b/"+path
	if !p.existed {
		from = "/dev/null"
	}
	newContent := p.new
	if p.delete {
		to, newContent = "/dev/null", nil
	}
	if p.mode != 0 && p.existed && bytes.Equal(p.old, newContent) {
		_, _ = fmt.Fprintf(w, "mode %s: %o\n", path, p.mode)
		return
	}
	_, _ = fmt.Fprint(w, unified(p.old, newContent, from, to))
}

// lines splits text for difflib; empty text is no lines, not one empty line.
func lines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	return difflib.SplitLines(string(b))
}

func unified(a, b []byte, from, to string) string {
	text, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        lines(a),
		B:        lines(b),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	})
	if err != nil {
		return fmt.Sprintf("--- %s\n+++ %s\n(diff failed: %v)\n", from, to, err)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return text
}

func apply(root, rel string, p *planned) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	if p.delete {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := p.mode
	if mode == 0 {
		mode = 0o644
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	}
	if err := os.WriteFile(path, p.new, mode); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// warnMissingScripts reports each hook command that runs a script under the project
// directory that does not exist.
func warnMissingScripts(w io.Writer, root string) {
	doc, _, err := readSettings(root)
	if err != nil || doc == nil {
		return
	}
	cmds, err := settings.Commands(doc)
	if err != nil {
		return
	}
	for _, c := range cmds {
		var rest string
		switch {
		case strings.HasPrefix(c, `"$CLAUDE_PROJECT_DIR"/`):
			rest = strings.TrimPrefix(c, `"$CLAUDE_PROJECT_DIR"/`)
		case strings.HasPrefix(c, "$CLAUDE_PROJECT_DIR/"):
			rest = strings.TrimPrefix(c, "$CLAUDE_PROJECT_DIR/")
		default:
			continue
		}
		script, _, _ := strings.Cut(rest, " ")
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(script))); err != nil {
			_, _ = fmt.Fprintf(w, "warning: %s: a hook runs %s, which does not exist\n", settingsPath, script)
		}
	}
}
