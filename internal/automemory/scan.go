package automemory

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Proposal is the suggested destination of one auto-memory file.
type Proposal struct {
	Path        string `json:"path"`
	Dir         string `json:"dir"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Destination string `json:"destination"` // instruction|issue|docs|host|drop|undecided
	Reason      string `json:"reason"`
}

// ScanResult lists the memory directories of a project and a proposal per file.
type ScanResult struct {
	Dirs  []string   `json:"dirs"`
	Files []Proposal `json:"files"`
}

var absPathRe = regexp.MustCompile(`(^|\s)(/|[A-Za-z]:[\\/])\S+`)

// MainCheckout returns the main checkout of the repository that contains dir:
// the parent of the common git directory.
func MainCheckout(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return "", fmt.Errorf("git rev-parse --git-common-dir in %s: empty output", dir)
	}
	return filepath.Dir(common), nil
}

// Scan proposes a destination for every markdown file in the memory
// directories of the project at repoMain under configDir.
func Scan(configDir, repoMain string) (ScanResult, error) {
	res := ScanResult{Dirs: []string{}, Files: []Proposal{}}
	projects := filepath.Join(configDir, "projects")
	entries, err := os.ReadDir(projects)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, fmt.Errorf("read %s: %w", projects, err)
	}
	enc := EncodeProject(repoMain)
	for _, e := range entries {
		if !e.IsDir() || !strings.EqualFold(e.Name(), enc) {
			continue
		}
		mem := filepath.Join(projects, e.Name(), "memory")
		if fi, err := os.Stat(mem); err == nil && fi.IsDir() {
			res.Dirs = append(res.Dirs, mem)
		}
	}
	sort.Strings(res.Dirs)
	for _, dir := range res.Dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			return res, fmt.Errorf("read %s: %w", dir, err)
		}
		var names []string
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".md") {
				names = append(names, f.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			path := filepath.Join(dir, n)
			data, err := os.ReadFile(path)
			if err != nil {
				return res, fmt.Errorf("read %s: %w", path, err)
			}
			res.Files = append(res.Files, propose(path, dir, n, string(data), repoMain))
		}
	}
	return res, nil
}

type frontmatter struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Metadata struct {
		Type string `yaml:"type"`
	} `yaml:"metadata"`
}

func splitFrontmatter(s string) (front, body string) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	rest, ok := strings.CutPrefix(s, "---\n")
	if !ok {
		return "", s
	}
	if f, b, ok := strings.Cut(rest, "\n---\n"); ok {
		return f, b
	}
	if f, ok := strings.CutSuffix(rest, "\n---"); ok {
		return f, ""
	}
	return "", s
}

func propose(path, dir, file, content, repoMain string) Proposal {
	p := Proposal{Path: path, Dir: dir}
	front, body := splitFrontmatter(content)
	var fm frontmatter
	if front != "" {
		_ = yaml.Unmarshal([]byte(front), &fm) // bad frontmatter counts as none
	}
	p.Name = fm.Name
	p.Type = fm.Type
	if p.Type == "" {
		p.Type = fm.Metadata.Type
	}
	switch {
	case file == "MEMORY.md":
		p.Destination, p.Reason = "drop", "index"
		return p
	case p.Type == "":
		p.Destination, p.Reason = "undecided", "no type"
		return p
	}
	p.Reason = "type " + p.Type
	switch p.Type {
	case "feedback":
		p.Destination = "instruction"
	case "project":
		p.Destination = "issue"
	case "reference":
		p.Destination = "docs"
		if hasOutsidePath(body, repoMain) {
			p.Destination = "host"
		}
	case "user":
		p.Destination = "drop"
	default:
		p.Destination = "undecided"
	}
	return p
}

func hasOutsidePath(body, repoMain string) bool {
	root := strings.TrimRight(strings.ReplaceAll(repoMain, `\`, "/"), "/")
	for _, m := range absPathRe.FindAllString(body, -1) {
		m = strings.ReplaceAll(strings.TrimSpace(m), `\`, "/")
		if m != root && !strings.HasPrefix(m, root+"/") {
			return true
		}
	}
	return false
}
