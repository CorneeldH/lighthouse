package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/gate"
	"github.com/CorneeldH/lighthouse/internal/lintcheck"
	"github.com/CorneeldH/lighthouse/internal/manifest"
)

// lintUnchecked is the end of a run that could not check: the reason on stderr, exit 3.
func lintUnchecked(cmd *cobra.Command, err error) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "lint check: COULD NOT CHECK — %v\n", err)
	return &exitError{code: gate.Unchecked, err: errSilent}
}

// lintRun checks the wiring of the linters of langs (empty: the packs of the manifest) under root
// and returns the exit code. It prints through lintcheck.Run only and is free of cobra, so init
// and the self test call it too.
func lintRun(root string, langs []lintcheck.Lang, dirs []string, forgeKind string, out, errOut io.Writer) int { // lighthouse: allow-language
	checkers := map[lintcheck.Lang]lintcheck.Checker{ // lighthouse: allow-language
		"python": lintcheck.PythonChecker{},
		"go":     lintcheck.GoChecker{},
		"shell":  lintcheck.ShellChecker{},
		"node":   lintcheck.NodeChecker{},
	}
	env := lintcheck.Env{Root: root, Stdout: out, Stderr: errOut, ForgeKind: forgeKind}
	return lintcheck.Run(lintcheck.Options{Root: root, Langs: langs, Dirs: dirs, Checkers: checkers}, env)
}

// lintCheck is the lint check init runs after it wrote; a test replaces it.
var lintCheck = lintRun

// lintAfterInit runs the lint check on root for every installed language. It never changes the
// exit code of init and prints nothing for a fully wired repo, so init stays `No changes.` there:
// the output of the check is held back and goes to errOut, under a heading, only for exit 1 or 3.
// out is unused on purpose: nothing of the check may reach the stdout of init.
func lintAfterInit(root string, _, errOut io.Writer) {
	forgeKind := ""
	if lines, set, err := importsConfig("forge.kind"); err == nil && set {
		forgeKind = lines[0]
	}
	var held bytes.Buffer
	rc := lintCheck(root, nil, nil, forgeKind, &held, &held)
	if rc == 0 {
		return
	}
	_, _ = fmt.Fprintln(errOut, "lint check: some linters are not wired")
	_, _ = errOut.Write(held.Bytes())
}

// newLintCommand is `lighthouse lint check`, the gate on a linter that is not wired. Every
// failure to check, flag errors included, is exit 3, never cobra's 1 (a finding).
func newLintCommand() *cobra.Command {
	lint := &cobra.Command{
		Use:   "lint check",
		Short: "Gate on a linter that is not wired",
		Args:  cobra.ArbitraryArgs,
		// Without RunE cobra prints the help and returns 0: a silently green gate.
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "(none)"
			if len(args) > 0 {
				name = args[0]
			}
			return lintUnchecked(cmd, fmt.Errorf("unknown subcommand %s", name))
		},
	}
	var selected, dirs []string
	check := &cobra.Command{
		Use:   "check [--lang python|go|node|shell]... [--dir PATH]...",
		Short: "Check that the linter of each installed language is pinned, configured and run in CI",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return lintUnchecked(cmd, fmt.Errorf("unexpected argument %q", args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLintCheck(cmd, selected, dirs)
		},
	}
	check.Flags().StringArrayVar(&selected, "lang", nil, "language to check: python, go, node or shell (default: the packs of the manifest)")
	check.Flags().StringArrayVar(&dirs, "dir", nil, "Node root to check (repo-relative; default: every Node root of the repository)")
	check.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return lintUnchecked(cmd, err) })
	lint.SetFlagErrorFunc(check.FlagErrorFunc())
	lint.AddCommand(check)
	return lint
}

func runLintCheck(cmd *cobra.Command, selected, dirs []string) error {
	var langs []lintcheck.Lang // lighthouse: allow-language
	for _, l := range selected {
		switch l {
		case "python", "go", "node", "shell":
			langs = append(langs, lintcheck.Lang(l))
		default:
			return lintUnchecked(cmd, fmt.Errorf("unknown --lang %q: use python, go, node or shell", l))
		}
	}
	root, err := config.RepoRoot()
	if err != nil {
		return lintUnchecked(cmd, err)
	}
	if len(langs) > 0 {
		if err := lintInstalled(root, selected); err != nil {
			return lintUnchecked(cmd, err)
		}
	}
	forgeKind := ""
	lines, set, err := importsConfig("forge.kind")
	if err != nil {
		return lintUnchecked(cmd, err)
	}
	if set {
		forgeKind = lines[0]
	}
	if rc := lintRun(root, langs, dirs, forgeKind, cmd.OutOrStdout(), cmd.ErrOrStderr()); rc != 0 {
		return &exitError{code: rc, err: errSilent}
	}
	return nil
}

// lintInstalled is an error when a selected language has no pack in the manifest: a language that
// is asked for but not installed is not checked, never a pass.
func lintInstalled(root string, selected []string) error {
	m, err := manifest.Read(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("--lang %s: %s is missing, no language is installed", selected[0], manifest.Path)
		}
		return err
	}
	for _, l := range selected {
		packs := []string{l}
		if l == "node" {
			packs = append(packs, "expo")
		}
		if !slices.ContainsFunc(packs, func(p string) bool { return slices.Contains(m.Packages, p) }) {
			return fmt.Errorf("--lang %s is not installed in %s", l, manifest.Path)
		}
	}
	return nil
}
