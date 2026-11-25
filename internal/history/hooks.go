package history

import (
	"sort"
	"strings"

	"github.com/CorneeldH/lighthouse/internal/agentaudit"
	"github.com/CorneeldH/lighthouse/internal/packs"
)

// OverlapFromAudit runs the agent audit over env and returns the project, local and user
// hooks that overlap a lighthouse guard. Names come from the finding key
// (`overlap:<guard>|<source>/<path>`), never from its text.
func OverlapFromAudit(env agentaudit.Env, guards []packs.Guard) ([]HookOverlap, error) {
	inv, err := agentaudit.Collect(env)
	if err != nil {
		return nil, err
	}
	var out []HookOverlap
	for _, f := range agentaudit.Overlap(inv, guards) {
		rest, ok := strings.CutPrefix(f.Key, agentaudit.KindOverlap+":")
		if !ok {
			continue
		}
		guard, hook, ok := strings.Cut(rest, "|")
		if !ok {
			continue
		}
		source, path, _ := strings.Cut(hook, "/")
		switch source {
		case agentaudit.SourceProject, agentaudit.SourceLocal, agentaudit.SourceUser:
			out = append(out, HookOverlap{Hook: source + "/" + safeHookPath(path), Guard: guard})
		}
	}
	return out, nil
}

// safeHookPath keeps a repo-relative or `~/` script path and reduces anything else (an
// absolute path, or an inline command with arguments) to its basename, so no user name or
// argument reaches the report.
func safeHookPath(path string) string {
	absolute := strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`) ||
		(len(path) >= 2 && path[1] == ':' && isLetter(path[0]))
	// `~user/...` names a user; only `~` alone or `~/` is the caller's own home.
	otherHome := strings.HasPrefix(path, "~") && !strings.HasPrefix(path, "~/")
	if absolute || otherHome || strings.ContainsAny(path, " \t\n\"'$`;&|<>=\\") {
		return hookBasename(path)
	}
	return path
}

// hookBasename is the basename of the first word of a hook command, without quotes. Both
// `/` and `\` separate, whatever the platform.
func hookBasename(command string) string {
	var word strings.Builder
	var quote rune
	for _, c := range strings.TrimSpace(command) {
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				word.WriteRune(c)
			}
			continue
		case c == '"' || c == '\'':
			quote = c
			continue
		case c == ' ' || c == '\t' || c == '\n':
		default:
			word.WriteRune(c)
			continue
		}
		break
	}
	p := strings.ReplaceAll(word.String(), `\`, "/")
	return p[strings.LastIndex(p, "/")+1:]
}

// hooks counts the blocks per hook script and reports the project hooks that overlap a
// lighthouse guard. It keeps no error text.
type hooks struct {
	env      Env
	blocking map[string]int
	hits     Hits
}

func newHooks(env Env) Detector {
	return &hooks{env: env, blocking: map[string]int{}}
}

func (d *hooks) Add(r Record) {
	if r.HookCommand == "" {
		return
	}
	if name := hookBasename(r.HookCommand); name != "" {
		d.blocking[name]++
		d.hits.Add(r.File, r.Line, r.Timestamp)
	}
}

func (d *hooks) Finish(env Env) Finding {
	f := Finding{
		Key:        "hooks",
		Status:     "none",
		Confidence: "low",
		Meaning:    "Shows which hooks block tool calls and which project hooks may duplicate a lighthouse hook.",
		Proposal: Proposal{Kind: "advice",
			Text: "No overlap found; run /lighthouse-audit-agent-config for a full audit of the hooks."},
	}
	if env.AuditErr != nil {
		f.Status = "no_data"
		return f
	}
	overlapping := make([]map[string]string, 0, len(env.AuditHooks))
	names := make([]string, 0, len(env.AuditHooks))
	for _, o := range env.AuditHooks {
		overlapping = append(overlapping, map[string]string{"hook": o.Hook, "guard": o.Guard})
		names = append(names, o.Hook)
	}
	sort.Strings(names)
	sort.Slice(overlapping, func(i, j int) bool {
		a, b := overlapping[i], overlapping[j]
		if a["hook"] != b["hook"] {
			return a["hook"] < b["hook"]
		}
		return a["guard"] < b["guard"]
	})
	f.Measured = map[string]any{"overlapping": overlapping, "blocking_by_hook": d.blocking}
	if len(overlapping) == 0 && len(d.blocking) == 0 {
		return f
	}
	f.Status = "found"
	f.Sources = d.hits.Top()
	if len(names) > 0 {
		f.Proposal.Text = "These hooks may duplicate a lighthouse hook: " + strings.Join(names, ", ") +
			". Run /lighthouse-audit-agent-config to judge each one."
	} else {
		f.Confidence = "high"
		f.Proposal.Text = "Hooks blocked tool calls; run /lighthouse-audit-agent-config to check them."
	}
	return f
}
