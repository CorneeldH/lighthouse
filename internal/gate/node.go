package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const nodeUsage = "usage: lighthouse gate node [--dir DIR] [--also PATH]... [--drift SCRIPT=PATH]... -- SCRIPT..."

// NodeTouches reports whether a changed path lies under dir (a directory boundary: web/x is
// under web, website/x is not; dir "." takes every path) or equals an also path. Empty names
// are ignored.
func NodeTouches(files []string, dir string, also []string) bool {
	for _, name := range files {
		if name == "" {
			continue
		}
		if slices.Contains(also, name) || dir == "." || strings.HasPrefix(name, dir+"/") {
			return true
		}
	}
	return false
}

// snapshot maps the relative slash path of every file under p to its content. A single file
// gives {".": content}; a missing path gives an empty map.
func snapshot(p string) (map[string]string, error) {
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(p)
		return map[string]string{".": string(data)}, err
	}
	out := map[string]string{}
	err = filepath.WalkDir(p, func(file string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			return err
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(p, file)
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return out, err
}

// cleanPath normalises a root-relative path; an empty, absolute or escaping one is a usage error.
func cleanPath(p string) (string, error) {
	if p == "" || strings.HasPrefix(p, "/") {
		return "", uncheckedError(nodeUsage)
	}
	c := path.Clean(p)
	if c == ".." || strings.HasPrefix(c, "../") {
		return "", uncheckedError(nodeUsage)
	}
	return c, nil
}

type drift struct{ script, path string }

func parseNode(args []string) (dir string, also []string, drifts []drift, scripts []string, err error) {
	split := slices.Index(args, "--")
	if split < 0 {
		return "", nil, nil, nil, uncheckedError(nodeUsage)
	}
	options, scripts := args[:split], args[split+1:]
	for len(options) > 0 {
		if len(options) < 2 {
			return "", nil, nil, nil, uncheckedError(nodeUsage)
		}
		flag, value := options[0], options[1]
		switch {
		case flag == "--dir" && dir == "":
			dir, err = cleanPath(value)
		case flag == "--also":
			var p string
			p, err = cleanPath(value)
			also = append(also, p)
		case flag == "--drift" && strings.Contains(value, "="):
			script, p, _ := strings.Cut(value, "=")
			if script == "" || p == "" {
				return "", nil, nil, nil, uncheckedError(nodeUsage)
			}
			p, err = cleanPath(p)
			drifts = append(drifts, drift{script, p})
		default:
			err = uncheckedError(nodeUsage)
		}
		if err != nil {
			return "", nil, nil, nil, err
		}
		options = options[2:]
	}
	if len(scripts) == 0 {
		return "", nil, nil, nil, uncheckedError(nodeUsage)
	}
	if dir == "" {
		dir = "."
	}
	return dir, also, drifts, scripts, nil
}

// checkScripts requires every name to be a script in dir/package.json.
func checkScripts(root, dir string, names []string) error {
	data, err := os.ReadFile(filepath.Join(root, dir, "package.json"))
	var manifest map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(data, &manifest)
	}
	if err != nil {
		return unchecked("cannot read %s/package.json: %s", dir, reason(err))
	}
	var scripts map[string]json.RawMessage
	if raw, ok := manifest["scripts"]; ok {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &scripts) != nil {
			return unchecked("%s/package.json has no scripts object", dir)
		}
	}
	for _, name := range names {
		if _, ok := scripts[name]; !ok {
			return unchecked("no script %s in %s/package.json", name, dir)
		}
	}
	return nil
}

func npm(dir, script string, env Env) (int, error) {
	_, _ = fmt.Fprintf(env.Stdout, "gate-node: %s — npm run %s\n", dir, script)
	inDir := env
	inDir.Dir = filepath.Join(env.Dir, dir)
	return run([]string{"npm", "run", "--silent", script}, inDir)
}

func runDrift(dir string, d drift, env Env) (int, error) {
	target := filepath.Join(env.Dir, dir, filepath.FromSlash(d.path))
	before, err := snapshot(target)
	if err != nil {
		return 0, unchecked("cannot read %s: %s", d.path, reason(err))
	}
	if rc, err := npm(dir, d.script, env); rc != 0 || err != nil {
		return rc, err
	}
	after, err := snapshot(target)
	if err != nil {
		return 0, unchecked("cannot read %s: %s", d.path, reason(err))
	}
	if sameFiles(before, after) {
		return 0, nil
	}
	shown := d.path
	if dir != "." {
		shown = dir + "/" + d.path
	}
	_, _ = fmt.Fprintf(env.Stdout, "gate-node: %s was stale; npm run %s regenerated it — commit the result\n", shown, d.script)
	return 1, nil
}

func sameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

// Node runs the npm scripts of a package directory when the commit touches it. args is
// everything after `gate node`: options, `--`, then script names. The exit code is that of the
// first failing step, or Unchecked with a line on stderr when nothing was tested. npm passes
// the exit code of a script through, so a check that itself exits 3 is reported as not tested.
func Node(args []string, env Env) int {
	rc, err := selectAndRunNode(args, env)
	return finish("gate-node", env, rc, err)
}

func selectAndRunNode(args []string, env Env) (int, error) {
	dir, also, drifts, scripts, err := parseNode(args)
	if err != nil {
		return 0, err
	}
	if env.FilesPath != "" {
		// Bytes split on newline, no UTF-8 check: a path need not be valid UTF-8.
		data, err := os.ReadFile(env.FilesPath)
		if err != nil {
			return 0, unchecked("cannot read the changed files: %s", reason(err))
		}
		if !NodeTouches(strings.Split(string(data), "\n"), dir, also) {
			where := ""
			if dir != "." {
				where = " under " + dir
			}
			_, _ = fmt.Fprintf(env.Stdout, "gate-node: skipped — the commit touches nothing%s\n", where)
			return 0, nil
		}
	}
	for _, tool := range []string{"node", "npm"} {
		if _, err := exec.LookPath(tool); err != nil {
			return 0, unchecked("%s not on PATH", tool)
		}
	}
	if info, err := os.Stat(filepath.Join(env.Dir, dir, "node_modules")); err != nil || !info.IsDir() {
		return 0, unchecked("%s/node_modules missing; fix: cd %s && npm ci", dir, dir)
	}
	names := make([]string, 0, len(drifts)+len(scripts))
	for _, d := range drifts {
		names = append(names, d.script)
	}
	names = append(names, scripts...)
	if err := checkScripts(env.Dir, dir, names); err != nil {
		return 0, err
	}
	for _, d := range drifts {
		if rc, err := runDrift(dir, d, env); rc != 0 || err != nil {
			return rc, err
		}
	}
	for _, script := range scripts {
		if rc, err := npm(dir, script, env); rc != 0 || err != nil {
			return rc, err
		}
	}
	_, _ = fmt.Fprintf(env.Stdout, "gate-node: passed — %s\n", strings.Join(names, ", "))
	return 0, nil
}
