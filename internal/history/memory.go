package history

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// memoryReadLimit is the number of MEMORY.md lines Claude Code reads.
const memoryReadLimit = 200

// memory measures the auto-memory directories of the repo's project dirs. It reads only
// file names, counts and line counts, never content.
type memory struct {
	env  Env
	cwds map[string]map[string]bool // drive-letter-folded cwd -> raw spellings
}

func newMemory(env Env) Detector {
	return &memory{env: env, cwds: map[string]map[string]bool{}}
}

// foldDrive lowercases the drive letter of a Windows path; ok is false for other paths.
func foldDrive(p string) (string, bool) {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return "", false
	}
	c := p[0]
	if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
		return "", false
	}
	return strings.ToLower(p[:1]) + p[1:], true
}

func (d *memory) Add(r Record) {
	key, ok := foldDrive(r.Cwd)
	if !ok {
		return
	}
	if d.cwds[key] == nil {
		d.cwds[key] = map[string]bool{}
	}
	d.cwds[key][r.Cwd] = true
}

// countLines counts the lines of a file without keeping any of it.
func countLines(path string) (n int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 32<<10)
	var last byte = '\n'
	for {
		m, rerr := f.Read(buf)
		if m > 0 {
			n += bytes.Count(buf[:m], []byte{'\n'})
			last = buf[m-1]
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, rerr
		}
	}
	if last != '\n' {
		n++
	}
	return n, nil
}

func (d *memory) Finish(env Env) Finding {
	dirs := env.ProjectDirs
	if len(dirs) == 0 {
		dirs = d.env.ProjectDirs
	}
	files, indexLines := 0, 0
	byLower := map[string]int{}
	for _, p := range dirs {
		byLower[strings.ToLower(filepath.Base(p))]++
		mem := filepath.Join(p, "memory")
		_ = filepath.WalkDir(mem, func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			files++
			if path == filepath.Join(mem, "MEMORY.md") {
				if n, cerr := countLines(path); cerr == nil && n > indexLines {
					indexLines = n
				}
			}
			return nil
		})
	}
	variantDirs := 0
	for _, n := range byLower {
		if n > 1 {
			variantDirs++
		}
	}
	variantCwds := 0
	for _, spellings := range d.cwds {
		if len(spellings) > 1 {
			variantCwds++
		}
	}
	f := Finding{
		Key:        "memory",
		Status:     "none",
		Confidence: "high",
		Measured: map[string]any{
			"files":             files,
			"index_lines":       indexLines,
			"index_at_limit":    indexLines >= memoryReadLimit,
			"case_variant_dirs": variantDirs,
			"case_variant_cwds": variantCwds,
		},
		Meaning: "An index at the read limit is partly ignored, and lighthouse writes nothing to harness memory.",
		Proposal: Proposal{Kind: "advice",
			Text: "Clean up auto-memory; a rule that must last belongs in the repo."},
	}
	if files > 0 || variantDirs > 0 || variantCwds > 0 {
		f.Status = "found"
	}
	return f
}
