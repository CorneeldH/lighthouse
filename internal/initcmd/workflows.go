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
	"github.com/CorneeldH/lighthouse/internal/projectconfig"
)

// errNoPython means the config reader cannot run; workflows are then skipped, not fatal.
var errNoPython = errors.New("python3 is not available")

// fillWorkflows fills the @@<key>@@ placeholders of every workflow unit (plan M0 (b),
// design choice 13). A workflow that cannot be filled (a key not set, no python3, a
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

// pythonLookup answers config keys with the package's own config reader, so the
// defaults have one source. It reads lighthouse.toml as init will leave it.
type pythonLookup struct {
	packs  fs.FS
	root   string
	tables []projectconfig.Table // what lighthouse.toml gets; see configTables
	dir    string                // temporary: lighthouse_lib/ and the proposed lighthouse.toml
	err    error
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
	proposed, _, err := proposeConfig(old, p.tables)
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
	// The user's PYTHON* settings could hide the temporary lib (PYTHONSAFEPATH drops the
	// cwd from sys.path) or shadow it; the lib is found through PYTHONPATH alone.
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PYTHON") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env,
		"PYTHONPATH="+p.dir,
		"PYTHONDONTWRITEBYTECODE=1",
		"LIGHTHOUSE_REPO_ROOT="+p.root,
		"LIGHTHOUSE_TOML="+filepath.Join(p.dir, configPath),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return strings.TrimSuffix(stdout.String(), "\n"), true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(stderr.String(), key+" is not set"):
		return "", false, nil
	default:
		// The last line says what went wrong (a traceback ends with it); the temporary
		// copy of lighthouse.toml stands for the repository's own.
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		msg = strings.ReplaceAll(msg, filepath.Join(p.dir, configPath), configPath)
		return "", false, fmt.Errorf("%s: %s", key, msg)
	}
}

func (p *pythonLookup) close() {
	if p.dir != "" {
		_ = os.RemoveAll(p.dir)
	}
}
