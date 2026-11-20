package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CorneeldH/lighthouse/internal/config"
	"github.com/CorneeldH/lighthouse/internal/gate"
	"github.com/CorneeldH/lighthouse/internal/importcheck"
)

// importsTune lets a test replace the process runner and the checkers of `imports check`; nil in
// production.
var importsTune func(env *importcheck.Env, checkers map[importcheck.Lang]importcheck.Checker)

// importsUnchecked is the end of a run that could not check: the reason on stderr, exit 3.
func importsUnchecked(cmd *cobra.Command, err error) error {
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "imports: COULD NOT CHECK — %v\n", err)
	return &exitError{code: gate.Unchecked, err: errSilent}
}

// newImportsCommand is `lighthouse imports check`, the gate on undeclared direct imports. Every
// failure to check, flag errors included, is exit 3, never cobra's 1 (a finding).
func newImportsCommand() *cobra.Command {
	imports := &cobra.Command{
		Use:   "imports check",
		Short: "Gate on undeclared direct imports",
		Args:  cobra.ArbitraryArgs,
		// Without RunE cobra prints the help and returns 0: a silently green gate.
		RunE: func(cmd *cobra.Command, args []string) error {
			name := "(none)"
			if len(args) > 0 {
				name = args[0]
			}
			return importsUnchecked(cmd, fmt.Errorf("unknown subcommand %s", name))
		},
	}
	var selected, files []string
	check := &cobra.Command{
		Use:   "check [--lang python|go|node]... [--files PATH]...",
		Short: "Check that every direct import is declared in a manifest",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return importsUnchecked(cmd, fmt.Errorf("unexpected argument %q", args[0]))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImportsCheck(cmd, selected, files)
		},
	}
	check.Flags().StringArrayVar(&selected, "lang", nil, "language to check: python, go or node (default: the packs of the manifest)")
	check.Flags().StringArrayVar(&files, "files", nil, "check only the projects these files belong to (repo-relative, or absolute under the root)")
	check.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return importsUnchecked(cmd, err) })
	imports.SetFlagErrorFunc(check.FlagErrorFunc())
	imports.AddCommand(check)
	return imports
}

func runImportsCheck(cmd *cobra.Command, selected, files []string) error {
	var languages []importcheck.Lang // lighthouse: allow-language
	for _, l := range selected {
		switch l {
		case "python", "go", "node":
			languages = append(languages, importcheck.Lang(l))
		default:
			return importsUnchecked(cmd, fmt.Errorf("unknown --lang %q: use python, go or node", l))
		}
	}
	enabled, err := importsEnabled()
	if err != nil {
		return importsUnchecked(cmd, err)
	}
	if !enabled {
		return nil
	}
	root, err := config.RepoRoot()
	if err != nil {
		return importsUnchecked(cmd, err)
	}
	relFiles, err := importsRelFiles(root, files)
	if err != nil {
		return importsUnchecked(cmd, err)
	}
	sourceDirs, err := importsSourceDirs()
	if err != nil {
		return importsUnchecked(cmd, err)
	}
	checkers := map[importcheck.Lang]importcheck.Checker{ // lighthouse: allow-language
		"go":     importcheck.GoChecker{},
		"python": importcheck.PythonChecker{SourceDirs: sourceDirs, MainCheckout: os.Getenv("CLAUDE_PROJECT_DIR")},
		"node":   importcheck.NodeChecker{},
	}
	env := importcheck.Env{Root: root, Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr()}
	if importsTune != nil {
		importsTune(&env, checkers)
	}
	if rc := importcheck.Run(importcheck.Options{Root: root, Langs: languages, Files: relFiles, Checkers: checkers}, env); rc != 0 {
		return &exitError{code: rc, err: errSilent}
	}
	return nil
}

// importsConfig reads one key through the config reader; set is false when the key is not set.
func importsConfig(key string) (lines []string, set bool, err error) {
	var out, errOut bytes.Buffer
	switch config.Run([]string{key}, &out, &errOut) {
	case 0:
		return strings.Split(strings.TrimRight(out.String(), "\n"), "\n"), true, nil
	case 1:
		return nil, false, nil
	}
	return nil, false, errors.New(strings.TrimSpace(strings.TrimPrefix(errOut.String(), "config: ")))
}

// importsEnabled is `[imports] check`; not set means on.
func importsEnabled() (bool, error) {
	lines, set, err := importsConfig("imports.check")
	if err != nil || !set {
		return true, err
	}
	return lines[0] != "false", nil
}

func importsSourceDirs() ([]string, error) {
	lines, set, err := importsConfig("python.source_dirs")
	if err != nil || !set {
		return nil, err
	}
	var dirs []string
	for _, l := range lines {
		if l != "" {
			dirs = append(dirs, l)
		}
	}
	return dirs, nil
}

// importsRelFiles turns --files into repo-relative, slash-separated paths. An absolute path
// under the root is made relative; a path outside the root is an error (exit 3), never skipped.
func importsRelFiles(root string, files []string) ([]string, error) {
	roots := []string{filepath.Clean(root)}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		roots = append(roots, real)
	}
	var out []string
	for _, f := range files {
		rel := filepath.ToSlash(f)
		if filepath.IsAbs(f) {
			rel = ""
			for _, r := range roots {
				if p, err := filepath.Rel(r, f); err == nil && !escapes(p) {
					rel = filepath.ToSlash(p)
					break
				}
			}
			if rel == "" {
				return nil, fmt.Errorf("--files %s is outside the repository root %s", f, root)
			}
		} else if escapes(filepath.FromSlash(rel)) {
			return nil, fmt.Errorf("--files %s is outside the repository root %s", f, root)
		}
		out = append(out, rel)
	}
	return out, nil
}

func escapes(rel string) bool {
	c := filepath.Clean(rel)
	return c == ".." || strings.HasPrefix(c, ".."+string(filepath.Separator))
}
