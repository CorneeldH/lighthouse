// Package transcripts finds the Claude Code transcripts that belong to a repo.
package transcripts

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/worktreeroot"
)

// Session is one main transcript with the subagent transcripts that belong to it.
type Session struct {
	ID, Path string
	Subs     []string
}

// Options tune the matching. The zero value is the behaviour of telemetry collect.
type Options struct {
	FoldCase bool
}

// Find walks the project folders under projects, keeps those whose cwd belongs to the
// repo and lists their main transcripts in a fixed order. A missing projects folder is no
// transcripts.
func Find(projects string, roots, slugSegs []string, o Options) ([]Session, error) {
	ents, err := os.ReadDir(projects)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(projects, e.Name())
		cwd := firstCwd(dir)
		if cwd == "" || !BelongsToRepo(cwd, roots, slugSegs, o) {
			continue
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if f.IsDir() || filepath.Ext(f.Name()) != ".jsonl" {
				continue
			}
			id := strings.TrimSuffix(f.Name(), ".jsonl")
			s := Session{ID: id, Path: filepath.Join(dir, f.Name())}
			subs, _ := filepath.Glob(filepath.Join(dir, id, "subagents", "*.jsonl"))
			sort.Strings(subs)
			s.Subs = subs
			out = append(out, s)
		}
	}
	return out, nil
}

// cwdScanLines bounds how far into a transcript the first cwd is looked for: a session sets its
// cwd on its first lines, so 200 is generous and keeps a huge file without one cheap to pass.
const cwdScanLines = 200

// firstCwd is the cwd of the first line object that has one, over the main transcripts of dir in
// name order; "" when there is none.
func firstCwd(dir string) string {
	files, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".jsonl" {
			continue
		}
		if cwd := cwdOf(filepath.Join(dir, f.Name())); cwd != "" {
			return cwd
		}
	}
	return ""
}

func cwdOf(path string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = fh.Close() }()
	br := bufio.NewReader(fh)
	for range cwdScanLines {
		raw, err := br.ReadString('\n')
		var o struct {
			Cwd string `json:"cwd"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(raw)), &o) == nil && o.Cwd != "" {
			return o.Cwd
		}
		if err != nil {
			return ""
		}
	}
	return ""
}

// BelongsToRepo is true when cwd lies under one of the roots, or its path holds the segments of
// the repo slug (<owner>/<repo>) next to each other, in any letter case: the layout of
// kenn-forge worktrees, which also finds a worktree that is already removed.
func BelongsToRepo(cwd string, roots, slugSegs []string, o Options) bool {
	_ = o
	cwd = filepath.Clean(cwd)
	for _, r := range roots {
		if cwd == r || worktreeroot.Contains(r, cwd) {
			return true
		}
	}
	segs := strings.FieldsFunc(filepath.ToSlash(cwd), func(r rune) bool { return r == '/' })
	for i := 0; i+len(slugSegs) <= len(segs); i++ {
		match := true
		for j, s := range slugSegs {
			if !strings.EqualFold(segs[i+j], s) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
