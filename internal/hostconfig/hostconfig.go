// Package hostconfig locates host.toml, the per-host settings file outside the repository.
package hostconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// HomeRel is the default location of host.toml, relative to the home directory.
const HomeRel = ".config/lighthouse/host.toml"

// Path returns $LIGHTHOUSE_HOST_TOML, else ~/HomeRel, and "" without a home directory.
func Path() string {
	if p := os.Getenv("LIGHTHOUSE_HOST_TOML"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, filepath.FromSlash(HomeRel))
}

// Workplace adapters a host.toml [dispatch] workplace key can choose.
const (
	WorkplaceKennForge = "kenn-forge"
	WorkplaceLocal     = "local"
)

// Dispatch holds the [dispatch] settings of host.toml that lighthouse tick reads.
type Dispatch struct {
	Repos             []string // absolute paths of the repositories the tick serves
	Slots             int      // maximum concurrent runs
	MaxSoftware       int      // maximum concurrent software runs
	MinMemAvailableGB int      // free memory below which no run starts
	ReservePerRunGB   int      // memory reserved per running run
	Night             string   // night window as "HH:MM-HH:MM"
	MaxRuntime        time.Duration
	MaxWait           time.Duration
	AnswerPermissions []string // forge permissions allowed to answer a run
	Workplace         string   // workplace adapter: WorkplaceKennForge or WorkplaceLocal
	Forge             DispatchForge
	Profiles          map[string]Profile
	Nodes             map[string]Node // [dispatch.nodes.<name>]; nil without nodes
}

// Node is a kenn-forge node under [dispatch.nodes.<name>]: the node's id on the fleet hub and
// the number of concurrent runs it takes. The node of [dispatch.forge] itself is "self", the
// empty node id, and is not listed here.
type Node struct {
	NodeID string
	Slots  int
}

// SelfNode is the reserved name of the node of [dispatch.forge].
const SelfNode = "self"

// DispatchForge locates the forge and its token. URL is where lighthouse talks to the forge
// API; WebURL is where a browser opens the forge dashboard, for instance a tailscale serve
// address, and is empty when unset. UnitPrefix is the systemd unit-name prefix of the
// kenn-forge adapter, including the trailing dash; it has no default, and the adapter fails
// when it is empty.
type DispatchForge struct {
	URL        string
	WebURL     string
	TokenFile  string
	UnitPrefix string
}

// LinkURL returns the forge address for a link in the browser: WebURL, else URL.
func (f DispatchForge) LinkURL() string {
	if f.WebURL != "" {
		return f.WebURL
	}
	return f.URL
}

// Profile is a run profile under [dispatch.profiles.<name>].
type Profile struct {
	TargetKey string `toml:"target_key"`
	MCPConfig string `toml:"mcp_config"`
	Node      string `toml:"node"` // name of a [dispatch.nodes] entry; "" is self
}

// NodeID returns the node_id of the node of profile, "" for self or an unknown profile.
func (d Dispatch) NodeID(profile string) string {
	p, ok := d.Profiles[profile]
	if !ok || p.Node == "" {
		return ""
	}
	return d.Nodes[p.Node].NodeID
}

// NodeName returns the name of the node with nodeID: "self" for "", and nodeID itself when
// no node has that id.
func (d Dispatch) NodeName(nodeID string) string {
	if nodeID == "" {
		return SelfNode
	}
	for name, n := range d.Nodes {
		if n.NodeID == nodeID {
			return name
		}
	}
	return nodeID
}

// DefaultDispatch returns the built-in settings of spec section 2.
func DefaultDispatch() Dispatch {
	d := Dispatch{
		Slots:             4,
		MaxSoftware:       2,
		MinMemAvailableGB: 8,
		ReservePerRunGB:   3,
		Night:             "21:00-08:00",
		MaxRuntime:        8 * time.Hour,
		MaxWait:           time.Hour,
		AnswerPermissions: []string{"admin", "maintain"},
		Workplace:         WorkplaceKennForge,
		Forge:             DispatchForge{URL: "http://127.0.0.1:8091"},
	}
	if home, err := os.UserHomeDir(); err == nil {
		d.Forge.TokenFile = filepath.Join(home, ".kenn", "forge", "auth_token")
	}
	return d
}

// LoadDispatch reads [dispatch], [dispatch.forge] and [dispatch.profiles.<name>] of the
// host.toml at path. A missing file, an empty path or a missing key gives the default.
// Unlike usage.LoadConfig an invalid value or an unknown key under dispatch is an error naming
// its key, because the tick must stop loudly on it. Keys outside dispatch are ignored.
func LoadDispatch(path string) (Dispatch, error) {
	d := DefaultDispatch()
	if path == "" {
		return d, nil
	}
	var raw struct {
		Dispatch struct {
			Repos             []string `toml:"repos"`
			Slots             *int     `toml:"slots"`
			MaxSoftware       *int     `toml:"max_software"`
			MinMemAvailableGB *int     `toml:"min_mem_available_gb"`
			ReservePerRunGB   *int     `toml:"reserve_per_run_gb"`
			Night             *string  `toml:"night"`
			MaxRuntime        *string  `toml:"max_runtime"`
			MaxWait           *string  `toml:"max_wait"`
			AnswerPermissions []string `toml:"answer_permissions"`
			Workplace         *string  `toml:"workplace"`
			Forge             struct {
				URL        *string `toml:"url"`
				WebURL     *string `toml:"web_url"`
				TokenFile  *string `toml:"token_file"`
				UnitPrefix *string `toml:"unit_prefix"`
			} `toml:"forge"`
			Profiles map[string]Profile `toml:"profiles"`
			Nodes    map[string]struct {
				NodeID *string `toml:"node_id"`
				Slots  *int    `toml:"slots"`
			} `toml:"nodes"`
		} `toml:"dispatch"`
	}
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return d, nil
		}
		return DefaultDispatch(), fmt.Errorf("read %s: %w", path, err)
	}
	for _, key := range md.Undecoded() {
		if key[0] == "dispatch" {
			return DefaultDispatch(), fmt.Errorf("%s: unknown key in %s", key, path)
		}
	}
	r := raw.Dispatch
	for _, repo := range r.Repos {
		if !filepath.IsAbs(repo) {
			return DefaultDispatch(), fmt.Errorf("dispatch.repos: %q is not an absolute path", repo)
		}
	}
	d.Repos = r.Repos
	if r.Workplace != nil {
		switch *r.Workplace {
		case WorkplaceKennForge, WorkplaceLocal:
			d.Workplace = *r.Workplace
		default:
			return DefaultDispatch(), fmt.Errorf("dispatch.workplace: %q is neither %q nor %q", *r.Workplace, WorkplaceKennForge, WorkplaceLocal)
		}
	}
	if err := setMin1("dispatch.slots", r.Slots, &d.Slots); err != nil {
		return DefaultDispatch(), err
	}
	if err := setMin1("dispatch.max_software", r.MaxSoftware, &d.MaxSoftware); err != nil {
		return DefaultDispatch(), err
	}
	if err := setNonNegative("dispatch.min_mem_available_gb", r.MinMemAvailableGB, &d.MinMemAvailableGB); err != nil {
		return DefaultDispatch(), err
	}
	if err := setNonNegative("dispatch.reserve_per_run_gb", r.ReservePerRunGB, &d.ReservePerRunGB); err != nil {
		return DefaultDispatch(), err
	}
	if r.Night != nil {
		if !validNight(*r.Night) {
			return DefaultDispatch(), fmt.Errorf("dispatch.night: %q is not HH:MM-HH:MM", *r.Night)
		}
		d.Night = *r.Night
	}
	if err := setDuration("dispatch.max_runtime", r.MaxRuntime, &d.MaxRuntime); err != nil {
		return DefaultDispatch(), err
	}
	if err := setDuration("dispatch.max_wait", r.MaxWait, &d.MaxWait); err != nil {
		return DefaultDispatch(), err
	}
	if r.AnswerPermissions != nil {
		d.AnswerPermissions = r.AnswerPermissions
	}
	if r.Forge.URL != nil {
		d.Forge.URL = *r.Forge.URL
	}
	if r.Forge.WebURL != nil {
		d.Forge.WebURL = *r.Forge.WebURL
	}
	if r.Forge.TokenFile != nil {
		d.Forge.TokenFile = *r.Forge.TokenFile
	}
	if r.Forge.UnitPrefix != nil {
		d.Forge.UnitPrefix = *r.Forge.UnitPrefix
	}
	d.Profiles = r.Profiles
	names := make([]string, 0, len(r.Nodes))
	for name := range r.Nodes {
		names = append(names, name)
	}
	sort.Strings(names)
	ids := map[string]string{}
	for _, name := range names {
		n := r.Nodes[name]
		key := "dispatch.nodes." + name
		if name == SelfNode {
			return DefaultDispatch(), fmt.Errorf("%s: the name %q is reserved", key, SelfNode)
		}
		if n.NodeID == nil || *n.NodeID == "" {
			return DefaultDispatch(), fmt.Errorf("%s.node_id: missing", key)
		}
		if *n.NodeID == SelfNode {
			return DefaultDispatch(), fmt.Errorf("%s.node_id: %q is reserved", key, SelfNode)
		}
		if other, dup := ids[*n.NodeID]; dup {
			return DefaultDispatch(), fmt.Errorf("%s.node_id: %q is also the node_id of dispatch.nodes.%s", key, *n.NodeID, other)
		}
		ids[*n.NodeID] = name
		if n.Slots == nil {
			return DefaultDispatch(), fmt.Errorf("%s.slots: missing", key)
		}
		if err := setMin1(key+".slots", n.Slots, new(int)); err != nil {
			return DefaultDispatch(), err
		}
		if d.Nodes == nil {
			d.Nodes = map[string]Node{}
		}
		d.Nodes[name] = Node{NodeID: *n.NodeID, Slots: *n.Slots}
	}
	profiles := make([]string, 0, len(d.Profiles))
	for name := range d.Profiles {
		profiles = append(profiles, name)
	}
	sort.Strings(profiles)
	for _, name := range profiles {
		if node := d.Profiles[name].Node; node != "" {
			if _, ok := d.Nodes[node]; !ok {
				return DefaultDispatch(), fmt.Errorf("dispatch.profiles.%s.node: %q is not a node in dispatch.nodes", name, node)
			}
		}
	}
	return d, nil
}

func setMin1(key string, v *int, dst *int) error {
	if v == nil {
		return nil
	}
	if *v < 1 {
		return fmt.Errorf("%s: %d is below 1", key, *v)
	}
	*dst = *v
	return nil
}

func setNonNegative(key string, v *int, dst *int) error {
	if v == nil {
		return nil
	}
	if *v < 0 {
		return fmt.Errorf("%s: %d is negative", key, *v)
	}
	*dst = *v
	return nil
}

func setDuration(key string, v *string, dst *time.Duration) error {
	if v == nil {
		return nil
	}
	dur, err := time.ParseDuration(*v)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if dur <= 0 {
		return fmt.Errorf("%s: %q must be positive", key, *v)
	}
	*dst = dur
	return nil
}

// validNight reports whether s is "HH:MM-HH:MM" with valid clock times.
func validNight(s string) bool {
	from, to, ok := strings.Cut(s, "-")
	if !ok {
		return false
	}
	for _, c := range []string{from, to} {
		if _, err := time.Parse("15:04", c); err != nil {
			return false
		}
	}
	return true
}
