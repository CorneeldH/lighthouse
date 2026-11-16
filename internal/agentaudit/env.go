package agentaudit

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/hook"
	"github.com/CorneeldH/lighthouse/internal/manifest"
	"github.com/CorneeldH/lighthouse/internal/packs"
	"github.com/CorneeldH/lighthouse/internal/version"
	"github.com/CorneeldH/lighthouse/pack"
)

// Env holds the filesystem roots the audit reads, so a test can point it at a temporary tree.
type Env struct {
	Root       string       // repository or worktree root
	MainRoot   string       // main checkout (the directory above --git-common-dir); == Root outside a worktree
	ConfigDir  string       // $CLAUDE_CONFIG_DIR, else ~/.claude
	ClaudeJSON string       // $CLAUDE_CONFIG_DIR/.claude.json when that variable is set, else ~/.claude.json
	RepoOnly   bool         // read only the repository: no user config, no plugins
	Packs      []packs.Pack // the packages the manifest says are installed
	HookNames  []string     // hook.Names()
}

// DefaultEnv is the Env of the repository at root on this machine. A missing or broken
// manifest leaves Packs empty; Collect reports it as the status of source lighthouse.
func DefaultEnv(root string) (Env, error) {
	env := Env{Root: root, MainRoot: mainRoot(root), HookNames: hook.Names()}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		env.ConfigDir = dir
		env.ClaudeJSON = filepath.Join(dir, ".claude.json")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return Env{}, err
		}
		env.ConfigDir = filepath.Join(home, ".claude")
		env.ClaudeJSON = filepath.Join(home, ".claude.json")
	}
	m, err := manifest.Read(root)
	if err != nil {
		return env, nil
	}
	for _, name := range m.Packages {
		sub, err := fs.Sub(pack.Files, name)
		if err != nil {
			return Env{}, err
		}
		if _, err := fs.Stat(sub, "."); err != nil {
			continue // a package this binary does not carry
		}
		p, err := packs.Load(sub, name, version.String())
		if err != nil {
			return Env{}, err
		}
		env.Packs = append(env.Packs, p)
	}
	return env, nil
}

// mainRoot is the main checkout of the repository at root: the directory above its git
// common dir. Outside git, or when git cannot say, it is root itself.
func mainRoot(root string) string {
	out, err := exec.CommandContext(context.Background(), "git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return root
	}
	common := strings.TrimSpace(string(out))
	if filepath.Base(common) != ".git" {
		return root // a bare repository or an unusual layout: no checkout above it
	}
	return filepath.Dir(common)
}
