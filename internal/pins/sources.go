package pins

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SourcePin is one thing taken over from another repository: a skill directory (key: the
// directory relative to pack/) or a file listed in pack/SOURCES.toml (key: its path).
type SourcePin struct {
	Key    string
	Repo   string // owner/repo
	Commit string // 40 hex
	Path   string // path inside the upstream repo, without leading or trailing /
	// Files are the SKILL.md variants or the single file the pin is made of, relative to the
	// repo root, sorted.
	Files []string
	// Err is set when the pin does not load; the measurement reports it as `error`.
	Err error
}

var (
	shaRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoRe     = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)
	upstreamRe = regexp.MustCompile(`https://github\.com/([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)`)
)

// FindSourcePins returns the skill pins under pack/*/skills (never the installed copies in
// .claude/ or .agents/) and the files of pack/SOURCES.toml that carry source fields, sorted by
// key. The `claude/` and `agents/` variants of one skill directory form one pin; a `pin = "<key>"`
// entry of SOURCES.toml belongs to that skill pin and is not counted again. A pin that does not
// load comes back with Err set instead of failing the call.
func FindSourcePins(root string) ([]SourcePin, error) {
	skills, err := findSkillPins(root)
	if err != nil {
		return nil, err
	}
	files, err := findFilePins(root)
	if err != nil {
		return nil, err
	}
	out := append(skills, files...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

type skillFields struct{ origin, upstream, commit, path string }

func (f skillFields) sameUpstream(o skillFields) bool {
	return f.upstream == o.upstream && f.commit == o.commit && f.path == o.path
}

func findSkillPins(root string) ([]SourcePin, error) {
	packDir := filepath.Join(root, "pack")
	skillRoots, err := filepath.Glob(filepath.Join(packDir, "*", "skills"))
	if err != nil {
		return nil, err
	}
	// skill directory (relative to pack/) -> variant file (relative to root) -> fields
	variants := map[string]map[string]skillFields{}
	for _, skillsDir := range skillRoots {
		err := filepath.WalkDir(skillsDir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || d.Name() != "SKILL.md" {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			dir := filepath.Dir(p)
			if b := filepath.Base(dir); b == "claude" || b == "agents" {
				dir = filepath.Dir(dir)
			}
			// Both paths lie under packDir and root by construction (WalkDir of a glob under
			// them), so Rel cannot fail.
			key, _ := filepath.Rel(packDir, dir)
			key = filepath.ToSlash(key)
			rel, _ := filepath.Rel(root, p)
			meta := SkillMetadata(string(data))
			if variants[key] == nil {
				variants[key] = map[string]skillFields{}
			}
			variants[key][filepath.ToSlash(rel)] = skillFields{
				origin: meta["origin"], upstream: meta["upstream"], commit: meta["upstream-commit"], path: meta["upstream-path"],
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read skills in %s: %w", skillsDir, err)
		}
	}
	var out []SourcePin
	for key, vs := range variants {
		pin, ok := buildSkillPin(key, vs)
		if ok {
			out = append(out, pin)
		}
	}
	return out, nil
}

// buildSkillPin merges the variants of one skill directory; ok is false when no variant
// carries an `upstream`, so the skill is not a pin at all.
func buildSkillPin(key string, vs map[string]skillFields) (SourcePin, bool) {
	files := make([]string, 0, len(vs))
	anyUpstream := false
	for f, v := range vs {
		files = append(files, f)
		if v.upstream != "" {
			anyUpstream = true
		}
	}
	if !anyUpstream {
		return SourcePin{}, false
	}
	sort.Strings(files)
	pin := SourcePin{Key: key, Files: files}
	first := vs[files[0]]
	for _, f := range files[1:] {
		if !vs[f].sameUpstream(first) {
			pin.Err = fmt.Errorf("skill %s: variants %s and %s disagree on upstream, upstream-commit or upstream-path", key, files[0], f)
			return pin, true
		}
	}
	repo, err := RepoFromUpstream(first.upstream)
	if err != nil {
		pin.Err = fmt.Errorf("skill %s: %w", key, err)
		return pin, true
	}
	pin.Repo = repo
	pin.Commit = first.commit
	pin.Path = normalizePath(first.path)
	if !shaRe.MatchString(pin.Commit) {
		pin.Err = fmt.Errorf("skill %s: upstream-commit must be 40 hex, got %q", key, pin.Commit)
	} else if pin.Path == "" {
		pin.Err = fmt.Errorf("skill %s: upstream-path is missing", key)
	}
	return pin, true
}

type sourceEntry struct {
	Path       string `toml:"path"`
	Pin        string `toml:"pin"`
	SourceRepo string `toml:"source_repo"`
	SourceSHA  string `toml:"source_sha"`
	SourcePath string `toml:"source_path"`
}

func findFilePins(root string) ([]SourcePin, error) {
	var doc struct {
		File []sourceEntry `toml:"file"`
	}
	_, err := toml.DecodeFile(filepath.Join(root, "pack", "SOURCES.toml"), &doc)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pack/SOURCES.toml: %w", err)
	}
	var out []SourcePin
	for _, e := range doc.File {
		if e.SourceRepo == "" && e.SourceSHA == "" && e.SourcePath == "" {
			continue
		}
		pin := SourcePin{
			Key: e.Path, Repo: e.SourceRepo, Commit: e.SourceSHA,
			Path: normalizePath(e.SourcePath), Files: []string{e.Path},
		}
		switch {
		case !repoRe.MatchString(e.SourceRepo):
			pin.Err = fmt.Errorf("file %s: source_repo must be owner/repo, got %q", e.Path, e.SourceRepo)
		case !shaRe.MatchString(e.SourceSHA):
			pin.Err = fmt.Errorf("file %s: source_sha must be 40 hex, got %q", e.Path, e.SourceSHA)
		case pin.Path == "":
			pin.Err = fmt.Errorf("file %s: source_path is missing", e.Path)
		}
		out = append(out, pin)
	}
	return out, nil
}

// RepoFromUpstream returns owner/repo of the first https://github.com/<owner>/<repo> in an
// `upstream` value. A `.git` suffix and further path segments (`/tree/main/x`) are dropped.
func RepoFromUpstream(u string) (string, error) {
	m := upstreamRe.FindStringSubmatch(u)
	if m == nil {
		return "", fmt.Errorf("upstream %q holds no https://github.com/<owner>/<repo>", u)
	}
	repo := strings.TrimSuffix(strings.TrimRight(m[2], "."), ".git")
	if repo == "" {
		return "", fmt.Errorf("upstream %q holds no repository name", u)
	}
	return m[1] + "/" + repo, nil
}

// normalizePath cuts leading and trailing slashes.
func normalizePath(p string) string { return strings.Trim(p, "/") }
