package history

import (
	"regexp"
	"strings"
)

var (
	gitPushRe   = regexp.MustCompile(`\bgit\s+(?:-C\s+(?:"[^"]*"|'[^']*'|\S+)\s+)?push\b`)
	gitCommitRe = regexp.MustCompile(`\bgit\s+(?:-C\s+(?:"[^"]*"|'[^']*'|\S+)\s+)?commit\b`)
)

// isTrunkName reports whether a branch is a trunk: main, master, develop, trunk,
// release/* or the default branch (a bare name, without refs/remotes/origin/).
func isTrunkName(name, defaultBranch string) bool {
	if name == "" {
		return false
	}
	switch name {
	case "main", "master", "develop", "trunk":
		return true
	}
	if strings.HasPrefix(name, "release/") {
		return true
	}
	return defaultBranch != "" && name == defaultBranch
}

type trunk struct {
	env       Env
	cwds      map[string]map[string]string // session -> cwd -> gitBranch
	commits   map[string]int
	pushes    map[string]int
	names     []string
	seen      map[string]bool
	unknown   int
	total     int
	trunkPush int
	knownPush int
	hits      Hits
}

func newTrunk(env Env) *trunk {
	return &trunk{env: env, cwds: map[string]map[string]string{}, commits: map[string]int{},
		pushes: map[string]int{}, seen: map[string]bool{}}
}

func (d *trunk) isTrunk(name string) bool {
	if isTrunkName(name, "") {
		return true
	}
	for _, t := range d.env.TrunkNames {
		if isTrunkName(name, t) {
			return true
		}
	}
	return false
}

func (d *trunk) Add(r Record) {
	sess := r.SessionID
	if sess == "" {
		sess = r.File
	}
	if r.Cwd != "" && r.GitBranch != "" && r.GitBranch != "HEAD" {
		if d.cwds[sess] == nil {
			d.cwds[sess] = map[string]string{}
		}
		d.cwds[sess][r.Cwd] = r.GitBranch
	}
	for _, c := range r.Commands {
		push, commit := gitPushRe.MatchString(c), gitCommitRe.MatchString(c)
		if !push && !commit {
			continue
		}
		branch, known := ResolveBranch(c, r.GitBranch, d.cwds[sess])
		if push {
			d.total++
		}
		if commit {
			d.total++
		}
		if !known {
			if push {
				d.unknown++
			}
			if commit {
				d.unknown++
			}
			continue
		}
		if commit {
			d.commits[branch]++
		}
		if push {
			d.pushes[branch]++
			d.knownPush++
			if d.isTrunk(branch) {
				d.trunkPush++
				if !d.seen[branch] {
					d.seen[branch] = true
					d.names = append(d.names, branch)
				}
				d.hits.Add(r.File, r.Line, r.Timestamp)
			}
		}
	}
}

// Counts gives the pushes on a trunk and the pushes with a known branch; the level rule
// uses them.
func (d *trunk) Counts() (trunkPushes, knownPushes int) { return d.trunkPush, d.knownPush }

func (d *trunk) Finish(Env) Finding {
	ratio := 0.0
	if d.knownPush > 0 {
		ratio = float64(d.trunkPush) / float64(d.knownPush)
	}
	names := d.names
	if names == nil {
		names = []string{}
	}
	f := Finding{
		Key:        "trunk",
		Status:     "none",
		Confidence: "high",
		Measured: map[string]any{
			"commits_by_branch": d.commits,
			"pushes_by_branch":  d.pushes,
			"trunk_names":       names,
			"trunk_push_ratio":  ratio,
			"unknown_branch":    d.unknown,
		},
		Meaning: "Pushes go straight to the trunk; main-commit-gate blocks that from day one. " +
			"A shifting trunk (release/rN) clashes with one fixed default branch.",
		Proposal: Proposal{Kind: "advice", Text: "Pick one fixed trunk (main) and work on a branch per issue."},
	}
	if d.trunkPush > 0 {
		f.Status = "found"
		f.Sources = d.hits.Top()
	}
	if d.unknown*2 > d.total {
		f.Confidence = "low"
	}
	return f
}
