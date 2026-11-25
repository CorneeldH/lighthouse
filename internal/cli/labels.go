package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/dispatch"
	"github.com/CorneeldH/lighthouse/internal/flock"
	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/labelmigrate"
	"github.com/CorneeldH/lighthouse/internal/park"
	"github.com/CorneeldH/lighthouse/internal/repoconfig"
	"github.com/CorneeldH/lighthouse/internal/store"
)

// labelsEnv is what `lighthouse labels migrate` takes from its surroundings, so a test can
// replace it.
type labelsEnv struct {
	Getenv   func(string) string
	TomlPath func() (string, error)
	// Open opens the forge of repo.
	Open func(ctx context.Context, repo repoconfig.Repo) (forge.Forge, error)
	// Runs returns the runs of repo and whether a runs database exists; without one there is
	// nothing to read and step 3 refuses.
	Runs func(ctx context.Context, repo string) (runs []dispatch.RunView, haveDB bool, err error)
}

func defaultLabelsEnv() labelsEnv {
	o := newDefaultForgeOpener()
	return labelsEnv{
		Getenv:   os.Getenv,
		TomlPath: config.Path,
		Open: func(ctx context.Context, repo repoconfig.Repo) (forge.Forge, error) {
			return forgeOpenWith(o, func() (repoconfig.Repo, error) { return repo, nil })(ctx)
		},
		Runs: dbRuns,
	}
}

// dbRuns reads the runs of repo from the default database without creating or migrating it.
func dbRuns(ctx context.Context, repo string) ([]dispatch.RunView, bool, error) {
	p, err := store.DefaultPath()
	if err != nil {
		return nil, false, err
	}
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}
	st, err := store.OpenShared(ctx, p)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = st.Close() }()
	runs, _, err := dispatch.LoadView(ctx, st.DB, []string{repo})
	return runs, true, err
}

func newLabelsCommand() *cobra.Command { return newLabelsCommandWith(defaultLabelsEnv()) }

func newLabelsCommandWith(env labelsEnv) *cobra.Command {
	cmd := &cobra.Command{Use: "labels", Short: "Maintain the labels of this repo"}
	var stage string
	var olds []string
	var yes bool
	migrate := &cobra.Command{
		Use:   "migrate",
		Short: "Move needs_shaping, needs_decision and needs_testing to needs_mind and needs_hands",
		Long: "One-off migration of a repo to needs_mind and needs_hands. Stage files rewrites the taxonomy file\n" +
			"([paths].labels) and [labels] of lighthouse.toml. Stage forge relabels the issues, sets blocked_issue,\n" +
			"proposes To do for stuck In progress issues and deletes the old labels; it needs the tick paused\n" +
			"(lighthouse pause), takes tick.lock, shows the plan, asks to confirm (--yes skips the question), reads\n" +
			"the forge again and then writes. Stage all does both. After [labels] is rewritten, stage forge needs\n" +
			"--old needs_shaping=<name> (repeatable) to name the old labels; without it the built-in names are used\n" +
			"and a run that finds nothing says so on stderr. Without needs_mind or needs_hands in [labels] the new\n" +
			"names follow the old ones: a name ending in needs-shaping gives the same prefix plus needs-mind\n" +
			"(3-needs-shaping gives 3-needs-mind), needs-testing likewise gives needs-hands; any other old name\n" +
			"gives the built-in default. Exit 2 on a config error.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runLabelsMigrate(c, env, stage, olds, yes)
		},
	}
	migrate.Flags().StringVar(&stage, "stage", "all", "all, files or forge")
	migrate.Flags().StringArrayVar(&olds, "old", nil, "old label name as key=name, key one of needs_shaping, needs_decision, needs_testing")
	migrate.Flags().BoolVar(&yes, "yes", false, "skip the confirmation question, not the preview")
	cmd.AddCommand(migrate)
	return cmd
}

func configErr(err error) error { return &exitError{code: forgeExitUsage, err: err} }

func runLabelsMigrate(cmd *cobra.Command, env labelsEnv, stage string, olds []string, yes bool) error {
	if !slices.Contains([]string{"all", "files", "forge"}, stage) {
		return configErr(fmt.Errorf("--stage %q is not all, files or forge", stage))
	}
	tomlPath, err := env.TomlPath()
	if err != nil {
		return configErr(err)
	}
	old, err := labelmigrate.ReadOldNames(tomlPath)
	if err != nil {
		return configErr(err)
	}
	keys, err := labelmigrate.ReadLabelsKeys(tomlPath)
	if err != nil {
		return configErr(err)
	}
	if err := applyOld(&old, olds); err != nil {
		return configErr(err)
	}
	repo, src, err := loadRepoForMigration(tomlPath, old, keys)
	if err != nil {
		return configErr(err)
	}
	l := repo.Labels
	out := cmd.OutOrStdout()

	var taxPath string
	var taxSrc, taxNew []byte
	var tomlNew []byte
	if stage != "forge" {
		// Step 4 is read first: a taxonomy file that cannot be read stops everything.
		if repo.Paths.Labels != "" {
			taxPath = filepath.Join(repo.Dir, repo.Paths.Labels)
			if taxSrc, err = os.ReadFile(taxPath); err != nil {
				return fmt.Errorf("cannot read the taxonomy file: %w", err)
			}
			if taxNew, err = labelmigrate.RewriteTaxonomy(taxSrc, old, l.NeedsMind, l.NeedsHands); err != nil {
				return err
			}
		}
		if tomlNew, err = labelmigrate.RewriteLabelsTableHands(src, l.NeedsMind, derivedHands(keys, l)); err != nil {
			return err
		}
	}
	writeFiles := func() error {
		if taxPath != "" && string(taxNew) != string(taxSrc) {
			if err := writeKeepMode(taxPath, taxNew); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "wrote %s\n", taxPath)
		}
		if string(tomlNew) != string(src) {
			if err := writeKeepMode(tomlPath, tomlNew); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "wrote %s\n", tomlPath)
		}
		return nil
	}
	if stage == "files" {
		return writeFiles()
	}

	// Forge write phase, from the lock to the end.
	lockPath, err := tickLockPath(env.Getenv)
	if err != nil {
		return err
	}
	lock, err := flock.TryLock(lockPath)
	if errors.Is(err, flock.ErrHeld) {
		return errors.New("tick is running; pause and retry")
	}
	if err != nil {
		return fmt.Errorf("take tick.lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	pausePath, err := park.PauseFile(env.Getenv)
	if err != nil {
		return err
	}
	if _, paused := park.ReadPause(pausePath, time.Now()); !paused {
		return errors.New("the tick is not paused: run lighthouse pause first")
	}
	f, err := env.Open(cmd.Context(), repo)
	if err != nil {
		return err
	}
	runs, haveDB, err := env.Runs(cmd.Context(), repo.Slug)
	if err != nil {
		return fmt.Errorf("read runs: %w", err)
	}
	for _, r := range runs {
		if r.State == "open" {
			return fmt.Errorf("run of #%d is open: wait until it ends or parks", r.Issue)
		}
	}
	var lookup labelmigrate.RunLookup
	if haveDB {
		lookup = func(n int) (string, error) {
			for _, r := range runs {
				if r.Issue == n {
					return r.State, nil
				}
			}
			return "", nil
		}
	}
	plan, err := labelmigrate.BuildPlan(cmd.Context(), f, old, l, lookup, repo.Board != nil)
	if err != nil {
		return err
	}
	if len(plan) == 0 {
		_, _ = fmt.Fprintln(out, "forge: nothing to do")
		if !keys.Old && len(olds) == 0 {
			// Once the files are written the old keys are gone, so the old names are the built-in
			// defaults; a repo with other names would see nothing to do although the old labels remain.
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: [labels] has no old keys and no --old was given, so the old names are the built-in %s, %s and %s; none of them is on the forge. If the forge still has the old labels under other names, run again with --old needs_shaping=<name> (and needs_decision, needs_testing).\n",
				old.Shaping, old.Decision, old.Testing)
		}
	} else {
		_, _ = fmt.Fprintf(out, "forge plan, %d steps:\n", len(plan))
		for _, s := range plan {
			_, _ = fmt.Fprintf(out, "  %s\n", s)
		}
		if !yes && !confirm(cmd.InOrStdin(), out) {
			return errors.New("aborted: nothing was written")
		}
		// Read the forge again: it may have changed since the preview, and the lock holds from here.
		if plan, err = labelmigrate.BuildPlan(cmd.Context(), f, old, l, lookup, repo.Board != nil); err != nil {
			return err
		}
	}
	var deletes, rest []labelmigrate.Step
	for _, s := range plan {
		if s.Kind == labelmigrate.KindDeleteLabel {
			deletes = append(deletes, s)
		} else {
			rest = append(rest, s)
		}
	}
	// Spec order: steps 1-3, then the files (4, 5), then the old labels go (6).
	if err := labelmigrate.Execute(cmd.Context(), f, rest, out); err != nil {
		return err
	}
	if stage == "all" {
		if err := writeFiles(); err != nil {
			return err
		}
	}
	return labelmigrate.Execute(cmd.Context(), f, deletes, out)
}

func confirm(in io.Reader, out io.Writer) bool {
	_, _ = fmt.Fprint(out, "Apply this plan? [y/N] ")
	line, _ := bufio.NewReader(in).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}

func applyOld(old *labelmigrate.OldNames, olds []string) error {
	for _, kv := range olds {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			return fmt.Errorf("--old %q is not key=name", kv)
		}
		switch k {
		case "needs_shaping":
			old.Shaping = v
		case "needs_decision":
			old.Decision = v
		case "needs_testing":
			old.Testing = v
		default:
			return fmt.Errorf("--old key %q is not needs_shaping, needs_decision or needs_testing", k)
		}
	}
	return nil
}

// deriveName replaces the suffix oldSuffix of oldName by newSuffix, so a prefix such as `3-` is
// kept; any other oldName gives def.
func deriveName(oldName, oldSuffix, newSuffix, def string) string {
	if p, ok := strings.CutSuffix(oldName, oldSuffix); ok {
		return p + newSuffix
	}
	return def
}

// derivedHands is the needs_hands name RewriteLabelsTableHands must write: the derived one when
// [labels] has no needs_hands and it differs from the built-in default, "" otherwise.
func derivedHands(keys labelmigrate.LabelsKeys, l repoconfig.Labels) string {
	if keys.Hands || l.NeedsHands == repoconfig.DefaultLabels().NeedsHands {
		return ""
	}
	return l.NeedsHands
}

// loadRepoForMigration loads the lighthouse.toml at path although it still holds the removed
// [labels] keys: the old lines are dropped in memory first. Without needs_mind or needs_hands in
// the file their names follow the old ones (`3-needs-shaping` gives `3-needs-mind`). It returns
// the config, with Dir the directory of path, and the file as it is.
func loadRepoForMigration(path string, old labelmigrate.OldNames, keys labelmigrate.LabelsKeys) (repoconfig.Repo, []byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return repoconfig.Repo{}, nil, err
	}
	def := repoconfig.DefaultLabels()
	mind := deriveName(old.Shaping, "needs-shaping", "needs-mind", def.NeedsMind)
	clean, err := labelmigrate.RewriteLabelsTable(src, mind)
	if err != nil {
		return repoconfig.Repo{}, nil, err
	}
	tmp, err := os.MkdirTemp("", "lighthouse-labels-")
	if err != nil {
		return repoconfig.Repo{}, nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	p := filepath.Join(tmp, "lighthouse.toml")
	if err := os.WriteFile(p, clean, 0o600); err != nil {
		return repoconfig.Repo{}, nil, err
	}
	repo, err := repoconfig.LoadFile(p)
	if err != nil {
		return repoconfig.Repo{}, nil, err
	}
	repo.Dir = filepath.Dir(path)
	if !keys.Mind {
		repo.Labels.NeedsMind = mind
	}
	if !keys.Hands {
		repo.Labels.NeedsHands = deriveName(old.Testing, "needs-testing", "needs-hands", def.NeedsHands)
	}
	return repo, src, nil
}

func writeKeepMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}
