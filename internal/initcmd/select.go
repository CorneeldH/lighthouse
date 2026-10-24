package initcmd

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"

	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// maxContainsSize is the largest file a [[contains]] rule reads; a bigger one does not
// count (plan M0 (c), ontwerpkeuze 3).
const maxContainsSize = 1 << 20

// choice is a chosen package and why: "found <file>", "--pack", or "" for core and for
// a package the manifest already has.
type choice struct {
	Name, Reason string
}

func (c choice) String() string {
	if c.Reason == "" {
		return c.Name
	}
	return c.Name + " (" + c.Reason + ")"
}

// loadAll loads every package directory in fsys. A broken package fails the run, also
// when it is not chosen.
func loadAll(fsys fs.FS, version string) (map[string]packs.Pack, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	all := map[string]packs.Pack{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub, err := fs.Sub(fsys, e.Name())
		if err != nil {
			return nil, err
		}
		p, err := packs.Load(sub, e.Name(), version)
		if err != nil {
			return nil, err
		}
		all[e.Name()] = p
	}
	if _, ok := all["core"]; !ok {
		return nil, errors.New("package core is not in this binary")
	}
	return all, nil
}

// lsFiles lists the files git tracks or would track (not ignored) that one of specs
// names, read with glob semantics. Files lighthouse installed do not count: the shell
// hooks of core would otherwise make every repo a shell repo.
func lsFiles(root string, specs []string) ([]string, error) {
	args := []string{"-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--"}
	for _, s := range specs {
		args = append(args, ":(glob)"+s)
	}
	for _, g := range packs.InstalledGlobs() {
		args = append(args, ":(exclude,glob)"+g)
	}
	var stderr bytes.Buffer
	cmd := exec.Command("git", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	var files []string
	seen := map[string]bool{}
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" && !seen[f] {
			seen[f] = true
			files = append(files, f)
		}
	}
	return files, nil
}

// detect reports the first file through which the package with rule d fits the
// repository at root: files first, then each contains rule.
func detect(root string, d *packs.Detect) (string, bool, error) {
	if d == nil {
		return "", false, nil
	}
	if len(d.Files) > 0 {
		files, err := lsFiles(root, d.Files)
		if err != nil || len(files) > 0 {
			return first(files), len(files) > 0, err
		}
	}
	for _, c := range d.Contains {
		files, err := lsFiles(root, c.Files)
		if err != nil {
			return "", false, err
		}
		for _, f := range files {
			path := filepath.Join(root, filepath.FromSlash(f))
			info, err := os.Stat(path)
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
				continue // tracked, but gone from the working tree
			}
			if err != nil {
				return "", false, err
			}
			if !info.Mode().IsRegular() || info.Size() > maxContainsSize {
				continue
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return "", false, err
			}
			if bytes.Contains(b, []byte(c.Text)) {
				return f, true, nil
			}
		}
	}
	return "", false, nil
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

// choosePacks decides which packages this run installs (plan M0 (c), ontwerpkeuzes 5
// and 6): on init core plus every package that fits, on --upgrade the manifest's
// packages; then --pack adds and --no-pack drops. The result has core first, then the
// rest in alphabetical order. notes are for stderr.
func choosePacks(o Options, root string, all map[string]packs.Pack, m manifest.Manifest, initialized bool) ([]choice, []string, error) {
	for _, n := range append(append([]string{}, o.AddPacks...), o.DropPacks...) {
		if _, ok := all[n]; !ok {
			return nil, nil, fmt.Errorf("package %s is not in this binary", n)
		}
	}
	if slices.Contains(o.DropPacks, "core") {
		return nil, nil, errors.New("core cannot be left out")
	}
	reason := map[string]string{"core": ""}
	if initialized {
		for _, n := range m.Packages {
			if _, ok := all[n]; !ok {
				return nil, nil, fmt.Errorf("package %s is not in this binary", n)
			}
			reason[n] = ""
		}
	}
	for _, n := range o.AddPacks {
		if _, ok := reason[n]; !ok {
			reason[n] = "--pack"
		}
	}
	var notes []string
	for _, n := range sortedNames(all) {
		if _, chosen := reason[n]; chosen || slices.Contains(o.DropPacks, n) {
			continue
		}
		match, ok, err := detect(root, all[n].Detect)
		switch {
		case err != nil && initialized:
			// On --upgrade detection only suggests a package; it never stops the run.
			notes = append(notes, fmt.Sprintf("package %s: cannot check whether it fits: %v", n, err))
		case err != nil:
			return nil, nil, fmt.Errorf("package %s: cannot check whether it fits: %w", n, err)
		case !ok:
		case initialized:
			notes = append(notes, fmt.Sprintf("package %s fits this repository (found %s); add it with: lighthouse init --upgrade --pack %s", n, match, n))
		default:
			reason[n] = "found " + match
		}
	}
	for _, n := range o.DropPacks {
		delete(reason, n)
	}
	var chosen []choice
	for _, n := range sortedNames(reason) {
		chosen = append(chosen, choice{Name: n, Reason: reason[n]})
	}
	return chosen, notes, nil
}

// sortedNames returns the keys of m with core first, then in alphabetical order.
func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		if (names[i] == "core") != (names[j] == "core") {
			return names[i] == "core"
		}
		return names[i] < names[j]
	})
	return names
}

// mergeRequires joins the requirements of several packages per tool, so a tool that
// two packages need gives one report with the units of both.
func mergeRequires(reqs []packs.Requirement) []packs.Requirement {
	at := map[string]int{} // tool -> index in out
	var out []packs.Requirement
	for _, r := range reqs {
		i, ok := at[r.Tool]
		if !ok {
			at[r.Tool] = len(out)
			out = append(out, packs.Requirement{Tool: r.Tool})
			i = len(out) - 1
		}
		for _, u := range r.NeededBy {
			if !slices.Contains(out[i].NeededBy, u) {
				out[i].NeededBy = append(out[i].NeededBy, u)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tool < out[j].Tool })
	return out
}
