package pins

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// Kind says what a pin tracks.
type Kind string

const (
	// KindPlugin is a plugin whose upstream release is tracked.
	KindPlugin Kind = "plugin"
	// KindTool is a command-line tool that also has a locally installed version.
	KindTool Kind = "tool"
)

// Pin is one `[pins.<name>]` entry of lighthouse.toml with its defaults filled in.
type Pin struct {
	Name        string
	Kind        Kind
	Repo        string
	Version     string
	Command     string
	VersionArgs []string
	TagPrefix   string
	// Err is set when the entry does not load; the measurement reports it as `error`.
	Err error
}

type rawPin struct {
	Kind        string   `toml:"kind"`
	Repo        string   `toml:"repo"`
	Version     string   `toml:"version"`
	Command     string   `toml:"command"`
	VersionArgs []string `toml:"version_args"`
	TagPrefix   string   `toml:"tag_prefix"`
}

// LoadPins reads the `[pins.<name>]` sections of the lighthouse.toml at path, sorted by name.
// Without a `[pins]` section it returns nil, nil. A pin that does not load (unknown kind,
// missing repo or version, wrong field type) comes back with Err set instead of failing the
// whole call.
func LoadPins(path string) ([]Pin, error) {
	var doc struct {
		Pins map[string]toml.Primitive `toml:"pins"`
	}
	md, err := toml.DecodeFile(path, &doc)
	if err != nil {
		return nil, fmt.Errorf("read pins from %s: %w", path, err)
	}
	if len(doc.Pins) == 0 {
		return nil, nil
	}
	out := make([]Pin, 0, len(doc.Pins))
	for name, prim := range doc.Pins {
		var raw rawPin
		if err := md.PrimitiveDecode(prim, &raw); err != nil {
			out = append(out, Pin{Name: name, Err: fmt.Errorf("pin %s: %w", name, err)})
			continue
		}
		out = append(out, buildPin(name, raw))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func buildPin(name string, raw rawPin) Pin {
	p := Pin{
		Name: name, Kind: Kind(raw.Kind), Repo: raw.Repo, Version: raw.Version,
		Command: raw.Command, VersionArgs: raw.VersionArgs, TagPrefix: raw.TagPrefix,
	}
	if p.Command == "" {
		p.Command = name
	}
	if len(p.VersionArgs) == 0 {
		p.VersionArgs = []string{"--version"}
	}
	if p.TagPrefix == "" {
		p.TagPrefix = "v"
	}
	switch {
	case p.Kind != KindPlugin && p.Kind != KindTool:
		p.Err = fmt.Errorf("pin %s: unknown kind %q (want %q or %q)", name, raw.Kind, KindPlugin, KindTool)
	case p.Repo == "":
		p.Err = fmt.Errorf("pin %s: repo is missing", name)
	case p.Version == "":
		p.Err = fmt.Errorf("pin %s: version is missing", name)
	}
	return p
}
