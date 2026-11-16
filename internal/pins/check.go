package pins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// defaultToolTimeout bounds one `<tool> version` call.
const defaultToolTimeout = 10 * time.Second

// Env is what a check needs from the outside world; every field but Root has a default.
type Env struct {
	Root             string // repository root: lighthouse.toml and pack/ are read from here
	Worktree         string // scope of the local plugin lookup; "" means Root
	Git              GitSource
	Releases         forge.ReleaseReader
	Run              Runner // nil means ExecRunner
	InstalledPlugins string // "" means DefaultInstalledPlugins()
	ToolTimeout      time.Duration
}

// DefaultInstalledPlugins is Claude Code's installed_plugins.json of the current user.
func DefaultInstalledPlugins() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".claude", "plugins", "installed_plugins.json")
	}
	return filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
}

// CheckOptions picks the measurements; both false means both.
type CheckOptions struct{ Upstream, Local bool }

// PinResult is the outcome per measurement of one pin; a measurement that did not run, or does not
// apply to the kind of pin, is nil. The two are never merged into one status.
type PinResult struct {
	Key, Kind       string
	Upstream, Local *Result
}

// HasError reports whether any measurement of the pin is an error.
func (r PinResult) HasError() bool {
	return r.Upstream != nil && r.Upstream.Status == StatusError ||
		r.Local != nil && r.Local.Status == StatusError
}

// Check measures every pin of the repository at env.Root. Skill and file pins have an upstream
// measurement only; plugin and tool pins have both. A lighthouse.toml without a `[pins]` section,
// or none at all, has no plugin or tool pins. Results are sorted by key.
func Check(ctx context.Context, env Env, opts CheckOptions) []PinResult {
	if !opts.Upstream && !opts.Local {
		opts = CheckOptions{Upstream: true, Local: true}
	}
	if env.Run == nil {
		env.Run = ExecRunner
	}
	if env.InstalledPlugins == "" {
		env.InstalledPlugins = DefaultInstalledPlugins()
	}
	if env.ToolTimeout == 0 {
		env.ToolTimeout = defaultToolTimeout
	}
	if env.Worktree == "" {
		env.Worktree = env.Root
	}
	var out []PinResult

	tomlPath := filepath.Join(env.Root, "lighthouse.toml")
	toolPins, err := LoadPins(tomlPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		out = append(out, configError(opts, err))
	}
	for _, p := range toolPins {
		r := PinResult{Key: p.Name, Kind: string(p.Kind)}
		if opts.Upstream {
			res := measureUpstream(ctx, env, p)
			r.Upstream = &res
		}
		if opts.Local {
			res := measureLocal(ctx, env, p)
			r.Local = &res
		}
		out = append(out, r)
	}

	if opts.Upstream {
		srcs, err := FindSourcePins(env.Root)
		if err != nil {
			e := Result{Status: StatusError, Detail: err.Error()}
			out = append(out, PinResult{Key: "pack", Kind: "skill", Upstream: &e})
		}
		for _, sp := range srcs {
			res := measureSource(ctx, env, sp)
			out = append(out, PinResult{Key: sp.Key, Kind: sourceKind(sp), Upstream: &res})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func configError(opts CheckOptions, err error) PinResult {
	e := Result{Status: StatusError, Detail: err.Error()}
	r := PinResult{Key: "lighthouse.toml", Kind: "config"}
	if opts.Upstream {
		r.Upstream = &e
	}
	if opts.Local {
		r.Local = &e
	}
	return r
}

func sourceKind(sp SourcePin) string {
	if strings.Contains(sp.Key, "/skills/") {
		return "skill"
	}
	return "file"
}

func measureSource(ctx context.Context, env Env, sp SourcePin) Result {
	if env.Git == nil {
		return errResult("no git source configured")
	}
	return MeasureSource(ctx, env.Git, sp)
}

func measureUpstream(ctx context.Context, env Env, p Pin) Result {
	if env.Releases == nil {
		return errResult("no release reader configured")
	}
	return MeasureRelease(ctx, env.Releases, p)
}

func measureLocal(ctx context.Context, env Env, p Pin) Result {
	switch {
	case p.Err != nil:
		return errResult("%v", p.Err)
	case p.Kind == KindPlugin:
		return MeasurePlugin(p, env.InstalledPlugins, env.Worktree)
	default:
		return MeasureTool(ctx, env.Run, p, env.ToolTimeout)
	}
}

// Drift keeps the pins that have a measurement which is not current.
func Drift(rs []PinResult) []PinResult {
	var out []PinResult
	for _, r := range rs {
		if r.Upstream != nil && r.Upstream.Status != StatusCurrent || r.Local != nil && r.Local.Status != StatusCurrent {
			out = append(out, r)
		}
	}
	return out
}

// RenderText writes one line per pin per measurement:
// `<key> upstream <status> <detail>` and `<key> lokaal <status> <detail>`.
func RenderText(w io.Writer, rs []PinResult) {
	for _, r := range rs {
		line := func(label string, res *Result) {
			if res == nil {
				return
			}
			s := fmt.Sprintf("%s %s %s", r.Key, label, res.Status)
			if res.Detail != "" {
				s += " " + strings.ReplaceAll(res.Detail, "\n", " ")
			}
			_, _ = fmt.Fprintln(w, s)
		}
		line("upstream", r.Upstream)
		line("lokaal", r.Local)
	}
}

type jsonResult struct {
	Status Status `json:"status"`
	Detail string `json:"detail"`
}

type jsonPin struct {
	Key      string      `json:"key"`
	Kind     string      `json:"kind"`
	Upstream *jsonResult `json:"upstream,omitempty"`
	Local    *jsonResult `json:"local,omitempty"`
}

func toJSONResult(r *Result) *jsonResult {
	if r == nil {
		return nil
	}
	return &jsonResult{Status: r.Status, Detail: r.Detail}
}

// RenderJSON writes the results as one JSON array; a measurement that did not run is absent.
func RenderJSON(w io.Writer, rs []PinResult) error {
	out := make([]jsonPin, 0, len(rs))
	for _, r := range rs {
		out = append(out, jsonPin{Key: r.Key, Kind: r.Kind, Upstream: toJSONResult(r.Upstream), Local: toJSONResult(r.Local)})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
