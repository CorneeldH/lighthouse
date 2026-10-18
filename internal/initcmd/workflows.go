package initcmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/packs"
)

// errNoPython means the config reader cannot run; workflows are then skipped, not fatal.
var errNoPython = errors.New("python3 is not available")

// fillWorkflows fills the @@<key>@@ placeholders of every workflow unit. A workflow
// with a key that is not set is left out and reported (plan M0 (b), ontwerpkeuze 13);
// a config that cannot be read stops the run.
func fillWorkflows(w io.Writer, desired []packs.Unit, lookup packs.Lookup) ([]packs.Unit, error) {
	out := desired[:0:0]
	for _, u := range desired {
		if u.Kind != packs.KindFile || !strings.HasPrefix(u.Path, packs.WorkflowPrefix) {
			out = append(out, u)
			continue
		}
		name := path.Base(u.Path)
		content, missing, err := packs.Fill(u.Content, lookup)
		switch {
		case errors.Is(err, errNoPython):
			_, _ = fmt.Fprintf(w, "workflow %s not installed: %v\n", name, err)
			continue
		case err != nil:
			return nil, fmt.Errorf("workflow %s: %w", name, err)
		case len(missing) > 0:
			_, _ = fmt.Fprintf(w, "workflow %s not installed: %s is not set\n", name, strings.Join(missing, ", "))
			continue
		}
		u.Content = content
		out = append(out, u)
	}
	return out, nil
}

// pythonLookup answers config keys with the package's own config reader, so the
// defaults have one source. It reads lighthouse.toml as init will leave it.
type pythonLookup struct {
	packs fs.FS
	root  string
	dir   string // temporary: lighthouse_lib/ and the proposed lighthouse.toml
	err   error
}

func (p *pythonLookup) setup() error {
	if _, err := exec.LookPath("python3"); err != nil {
		return errNoPython
	}
	dir, err := os.MkdirTemp("", "lighthouse-init-")
	if err != nil {
		return err
	}
	p.dir = dir
	lib := filepath.Join(dir, "lighthouse_lib")
	if err := os.Mkdir(lib, 0o755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(p.packs, "core/lib")
	if err != nil {
		return fmt.Errorf("config reader not in this binary: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".py" {
			continue
		}
		b, err := fs.ReadFile(p.packs, "core/lib/"+e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(lib, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	old, _, err := readFile(p.root, configPath)
	if err != nil {
		return err
	}
	proposed, _, _, err := proposeConfig(p.root, old)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, configPath), proposed, 0o644)
}

func (p *pythonLookup) lookup(key string) (string, bool, error) {
	if p.dir == "" && p.err == nil {
		p.err = p.setup()
	}
	if p.err != nil {
		return "", false, p.err
	}
	// -m puts the cwd first on sys.path: the temporary directory, never the user's repo.
	cmd := exec.Command("python3", "-m", "lighthouse_lib.config", key)
	cmd.Dir = p.dir
	cmd.Env = append(os.Environ(),
		"LIGHTHOUSE_REPO_ROOT="+p.root,
		"LIGHTHOUSE_TOML="+filepath.Join(p.dir, configPath),
		"PYTHONDONTWRITEBYTECODE=1",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return strings.TrimSuffix(stdout.String(), "\n"), true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("%s: %s", key, strings.TrimSpace(stderr.String()))
	}
}

func (p *pythonLookup) close() {
	if p.dir != "" {
		_ = os.RemoveAll(p.dir)
	}
}
