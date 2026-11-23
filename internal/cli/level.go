package cli

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

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/initcmd"
	"github.com/CorneeldH/lighthouse/internal/levellog"
	"github.com/CorneeldH/lighthouse/internal/levels"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/pack"
)

// newLevelCommand is `lighthouse level`: what this repo installed, how often the components on
// `log` would have reported, and which components of the next level are not measured yet. It
// only reads, and it never judges whether the next level is within reach.
func newLevelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "level",
		Short: "Show the setup level, the stand per component and what the log stands counted",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return showLevel(cmd.OutOrStdout())
		},
	}
}

func showLevel(out io.Writer) error {
	root, err := config.RepoRoot()
	if err != nil {
		return err
	}
	level, over, err := levelConfig(root)
	if err != nil {
		return err
	}
	installed, found, err := levels.ReadInstalled(root)
	if err != nil {
		return err
	}
	tomlLevel := level
	if level == "" {
		level = installed.Level
	}
	if level == "" {
		level = "strict"
	}
	files, err := initcmd.CheckLevel(pack.Files, version.String(), level, over, installedPackages(root))
	if err != nil {
		return err
	}
	if !found {
		if tomlLevel != "" {
			_, _ = fmt.Fprintf(out, "level: %s (not installed yet; run lighthouse init --upgrade)\n", tomlLevel)
		} else {
			_, _ = fmt.Fprintln(out, "level: strict (no level set)")
		}
		return nil
	}
	return writeLevel(out, root, installed, files)
}

// installedPackages are the packages of the manifest; nil (every package of the binary) when
// the repo has no readable manifest yet.
func installedPackages(root string) []string {
	m, err := manifest.Read(root)
	if err != nil {
		return nil
	}
	return m.Packages
}

// levelConfig reads `level` and the [components] overrides of lighthouse.toml; both are empty
// when the file is absent.
func levelConfig(root string) (string, map[string]levels.Stand, error) {
	over := map[string]levels.Stand{}
	path := filepath.Join(root, "lighthouse.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return "", over, nil
	} else if err != nil {
		return "", nil, err
	}
	cfg, err := config.Load(path)
	if err != nil {
		return "", nil, err
	}
	for name, s := range cfg.Components {
		stand := levels.Stand(s)
		if err := stand.UnmarshalText([]byte(s)); err != nil {
			return "", nil, fmt.Errorf("[components] %s: %w", name, err)
		}
		over[name] = stand
	}
	return cfg.Level, over, nil
}

func writeLevel(out io.Writer, root string, in levels.Installed, files []levels.File) error {
	since, err := time.Parse(time.RFC3339, in.Since)
	if err != nil {
		return fmt.Errorf("%s: since %q: %w", levels.InstalledPath, in.Since, err)
	}
	day := since.UTC().Format("2006-01-02")
	_, _ = fmt.Fprintf(out, "level: %s (since %s)\n", in.Level, day)

	var rows, logged []string
	for _, name := range slices.Sorted(maps.Keys(in.Components)) {
		switch in.Components[name] {
		case levels.StandOff:
			rows = append(rows, name+"  off")
		case levels.StandLog:
			rows = append(rows, name+"  log")
			logged = append(logged, name)
		}
	}
	for _, r := range rows {
		_, _ = fmt.Fprintln(out, r)
	}
	_, _ = fmt.Fprintf(out, "%d components on\n", countOn(in, files))

	if len(logged) > 0 {
		counts, err := levellog.Count(root, since)
		if err != nil {
			return err
		}
		for _, name := range logged {
			_, _ = fmt.Fprintf(out, "%s  would have reported %d× since %s\n", name, counts[name], day)
		}
	}

	if next := unmeasured(in, files); len(next) > 0 {
		_, _ = fmt.Fprintln(out, "not measured at this level")
		for _, name := range next {
			_, _ = fmt.Fprintf(out, "  %s\n", name)
		}
	}
	for _, g := range levels.CoreGates {
		if st := in.Stand(g); st != levels.StandOn {
			_, _ = fmt.Fprintln(out, levels.CoreGateWarning(g, st))
		}
	}
	return nil
}

// countOn is the number of switchable components that are on; a base component is no choice.
func countOn(in levels.Installed, files []levels.File) int {
	r, err := levels.Resolve(files, in.Level, nil)
	if err != nil {
		return 0
	}
	n := 0
	for _, name := range r.Names() {
		if !isBase(files, name) && in.Stand(name) == levels.StandOn {
			n++
		}
	}
	return n
}

func isBase(files []levels.File, name string) bool {
	for _, f := range files {
		if slices.Contains(f.Base, name) {
			return true
		}
	}
	return false
}

// unmeasured lists the components that are off here and not off at the next level of the order.
// A skill measures nothing, so only hooks count.
func unmeasured(in levels.Installed, files []levels.File) []string {
	var order []string
	for _, f := range files {
		if len(f.Order) > 0 {
			order = f.Order
		}
	}
	i := slices.Index(order, in.Level)
	if i < 0 || i+1 >= len(order) {
		return nil
	}
	next, err := levels.Resolve(files, order[i+1], nil)
	if err != nil {
		return nil
	}
	var names []string
	for name, st := range in.Components {
		if st == levels.StandOff && !strings.HasPrefix(name, "skill:") && next.Stand(name) != levels.StandOff {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}
