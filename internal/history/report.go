package history

import "sort"

// Report is the JSON the history command prints.
type Report struct {
	Schema       int       `json:"schema"`
	Sessions     int       `json:"sessions"`
	Span         Span      `json:"span"`
	SkippedLines int       `json:"skipped_lines"`
	Level        Level     `json:"level"`
	Findings     []Finding `json:"findings"`
	Hint         string    `json:"hint,omitempty"`
}

type Span struct {
	First string `json:"first"`
	Last  string `json:"last"`
}

type Level struct {
	Recommended string `json:"recommended"`
	Reason      string `json:"reason"`
}

type Finding struct {
	Key        string         `json:"key"`
	Status     string         `json:"status"`
	Confidence string         `json:"confidence"`
	Measured   map[string]any `json:"measured"`
	Meaning    string         `json:"meaning"`
	Proposal   Proposal       `json:"proposal"`
	Sources    []Source       `json:"sources,omitempty"`
}

type Proposal struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Flag string `json:"flag,omitempty"`
}

type Source struct {
	File  string `json:"file"`
	Lines []int  `json:"lines"`
}

// HookOverlap pairs a project hook with the lighthouse guard it overlaps.
type HookOverlap struct{ Hook, Guard string }

// Env is what detectors need besides the records.
type Env struct {
	Repo        string
	TrunkNames  []string
	ProjectsDir string
	ProjectDirs []string
	RepoRoot    string
	AuditHooks  []HookOverlap
	AuditErr    error
}

// Detector receives every record in session order and ends with one finding.
type Detector interface {
	Add(Record)
	Finish(Env) Finding
}

const (
	maxSourceFiles = 5
	maxSourceLines = 10
)

type hitFile struct {
	file  string
	ts    string
	seq   int
	lines []int
}

// Hits collects where a signal was seen: the 5 most recent files (by record timestamp,
// call order when there is none) with their last 10 lines each, most recent first.
type Hits struct {
	files map[string]*hitFile
	seq   int
}

func (h *Hits) Add(file string, line int, ts string) {
	if h.files == nil {
		h.files = map[string]*hitFile{}
	}
	h.seq++
	f := h.files[file]
	if f == nil {
		f = &hitFile{file: file}
		h.files[file] = f
	}
	f.seq = h.seq
	if ts != "" && ts > f.ts {
		f.ts = ts
	}
	f.lines = append(f.lines, line)
}

func (h *Hits) Top() []Source {
	fs := make([]*hitFile, 0, len(h.files))
	for _, f := range h.files {
		fs = append(fs, f)
	}
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].ts != fs[j].ts {
			return fs[i].ts > fs[j].ts
		}
		return fs[i].seq > fs[j].seq
	})
	if len(fs) > maxSourceFiles {
		fs = fs[:maxSourceFiles]
	}
	out := make([]Source, 0, len(fs))
	for _, f := range fs {
		ls := f.lines
		if len(ls) > maxSourceLines {
			ls = ls[len(ls)-maxSourceLines:]
		}
		rev := make([]int, len(ls))
		for i, l := range ls {
			rev[len(ls)-1-i] = l
		}
		out = append(out, Source{File: f.file, Lines: rev})
	}
	return out
}
