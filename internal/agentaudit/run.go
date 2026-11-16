package agentaudit

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// The parts --only selects; Bronnen is always shown, so a COULD NOT CHECK is never silent.
const (
	PartInventory = "inventory"
	PartConflicts = "conflicts"
	PartOverlap   = "overlap"
	PartSurface   = "surface"
)

// Parts are the values --only accepts, in the order of the output.
var Parts = []string{PartInventory, PartConflicts, PartOverlap, PartSurface}

// SummaryTop is how many of the largest posts Summary shows.
const SummaryTop = 5

// Hint is the last line of Summary.
const Hint = "Draai /lighthouse-audit-agent-config voor een oordeel over de kandidaten."

// Options steer one run: which parts, how strict, which finding keys to ignore, and the
// largest posts and the description maximum of the surface (see internal/cli/agentaudit.go).
type Options struct {
	Only         []string // Parts; empty is every part
	Strict       bool
	Ignore       []string
	Top, MaxDesc int
}

func (o Options) shows(part string) bool { return len(o.Only) == 0 || slices.Contains(o.Only, part) }

// result is everything one run measured, with the ignored findings left out.
type result struct {
	inv       *Inventory
	statuses  []Status // the sources, then a status per unreadable hook
	conflicts []Finding
	overlap   []Finding
	stale     []string // ignore keys that no longer occur, while every source they name was measured
	surface   Surface
}

// Run is `lighthouse agent-audit`: it measures env, writes the report to out and returns
// the exit code. 2 when the audit as a whole cannot measure (then a COULD NOT CHECK line on
// errOut), or with Strict when a source or hook could not be checked (managed settings
// excepted: they are never read); 1 with Strict when a shown conflict or overlap candidate
// is not ignored; else 0. It writes nothing to disk.
func Run(env Env, o Options, out, errOut io.Writer) int {
	r, err := measure(env, o)
	if err != nil {
		return CouldNotCheck(errOut, err)
	}
	r.render(o, out)
	return r.exitCode(o)
}

// Summary is the report `init` shows: the sources, conflicts, overlap candidates and the
// five largest posts of the surface, then the Hint line. Its exit code is Run's.
func Summary(env Env, o Options, out, errOut io.Writer) int {
	o.Only = []string{PartConflicts, PartOverlap, PartSurface}
	o.Top = SummaryTop
	r, err := measure(env, o)
	if err != nil {
		return CouldNotCheck(errOut, err)
	}
	r.render(o, out)
	_, _ = fmt.Fprintf(out, "\n%s\n", Hint)
	return r.exitCode(o)
}

// render writes the sections o shows, in the order of spec §7.
func (r *result) render(o Options, out io.Writer) {
	w := &report{out: out}
	w.sources(r.statuses)
	if o.shows(PartConflicts) {
		w.findings("Conflicten", r.conflicts)
	}
	if o.shows(PartOverlap) {
		w.findings("Overlap, kandidaten", r.overlap)
	}
	if o.shows(PartConflicts) || o.shows(PartOverlap) {
		w.stale(r.stale)
	}
	if o.shows(PartSurface) {
		w.surface(r.surface, o.MaxDesc)
	}
	if o.shows(PartInventory) {
		w.inventory(r.inv)
	}
}

// CouldNotCheck writes why the audit as a whole cannot measure and returns its exit code, 2.
func CouldNotCheck(errOut io.Writer, err error) int {
	_, _ = fmt.Fprintf(errOut, "agent-audit: COULD NOT CHECK — %v\n", err)
	return 2
}

// measure collects env and judges it: findings, ignore, stale ignore keys, surface.
func measure(env Env, o Options) (*result, error) {
	inv, err := Collect(env)
	if err != nil {
		return nil, err
	}
	var guards []packs.Guard
	for _, p := range env.Packs {
		guards = append(guards, p.Guards...)
	}
	conflicts, overlap := Conflicts(inv, env), Overlap(inv, guards)
	r := &result{
		inv:      inv,
		statuses: append(slices.Clone(inv.Statuses), HookStatuses(inv)...),
		surface:  Measure(inv, o.Top, o.MaxDesc),
	}
	r.conflicts, r.overlap = unignored(conflicts, o.Ignore), unignored(overlap, o.Ignore)
	r.stale = staleKeys(o.Ignore, append(conflicts, overlap...), inv.Statuses)
	return r, nil
}

func unignored(fs []Finding, ignore []string) []Finding {
	var out []Finding
	for _, f := range fs {
		if !slices.Contains(ignore, f.Key) {
			out = append(out, f)
		}
	}
	return out
}

// exitCode follows spec §1: without Strict always 0.
func (r *result) exitCode(o Options) int {
	if !o.Strict {
		return 0
	}
	for _, s := range r.statuses {
		if s.State == StateCouldNotCh && s.Source != SourceManaged {
			return 2
		}
	}
	if (o.shows(PartConflicts) && len(r.conflicts) > 0) || (o.shows(PartOverlap) && len(r.overlap) > 0) {
		return 1
	}
	return 0
}

// namedSource is a source a finding key names: after the start, a colon or a bar, and
// followed by the slash of its path.
var namedSource = regexp.MustCompile(`(?:^|[:|])(` + regexp.QuoteMeta(SourcePluginPrefix) + `[^/:|]+|` +
	strings.Join([]string{SourceLighthouse, SourceProject, SourceLocal, SourceUser}, "|") + `)/`)

// permissionSources are the sources a permission key stands for: its key names the rule,
// not the sources, so every source that can hold a permission rule must be measured.
var permissionSources = []string{SourceLighthouse, SourceProject, SourceLocal, SourceUser}

// keySources are the sources an ignore key names (spec §8).
func keySources(key string) []string {
	kind, _, _ := strings.Cut(key, ":")
	if kind == KindPermission {
		return permissionSources
	}
	var out []string
	if kind == KindOverlap {
		out = append(out, SourceLighthouse)
	}
	for _, m := range namedSource.FindAllStringSubmatch(key, -1) {
		out = appendNew(out, m[1])
	}
	return out
}

// staleKeys are the ignore keys that match no finding while every source they name was
// measured (ok). A key with a source that is absent, skipped or COULD NOT CHECK stays
// silent: elsewhere it may still occur.
func staleKeys(ignore []string, all []Finding, statuses []Status) []string {
	state := map[string]string{}
	for _, s := range statuses {
		state[s.Source] = s.State
	}
	var out []string
	for _, key := range ignore {
		if slices.ContainsFunc(all, func(f Finding) bool { return f.Key == key }) {
			continue
		}
		measured := true
		for _, s := range keySources(key) {
			if state[s] != StateOK {
				measured = false
				break
			}
		}
		if measured {
			out = appendNew(out, key)
		}
	}
	return out
}
