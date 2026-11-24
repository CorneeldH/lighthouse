// Package hostconfig locates host.toml, the per-host settings file outside the repository.
package hostconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
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
	WorkflowPush      *WorkflowPush   // [dispatch.workflow_push]; nil switches the route off
}

// Review modes of [dispatch.workflow_push] review.
const (
	ReviewShadow  = "shadow"
	ReviewEnforce = "enforce"
)

// DefaultPushTimeout is [dispatch.workflow_push] timeout when host.toml sets none.
const DefaultPushTimeout = 30 * time.Minute

// MinPushTimeout is the smallest [dispatch.workflow_push] timeout: the pusher keeps a post window
// and a margin before it (pusher.workDeadline), and needs time left for the floor and the review.
const MinPushTimeout = 10 * time.Minute

// WorkflowPush is [dispatch.workflow_push]: the token and the reviewers of the workflow pusher.
// TokenFile and the Binds of a reviewer have a leading ~ expanded.
type WorkflowPush struct {
	TokenFile string
	Review    string // ReviewShadow or ReviewEnforce
	Timeout   time.Duration
	Reviewers []Reviewer
	Intent    *Reviewer // [dispatch.workflow_push.intent]: the intent check; Topic empty, nil without the table
}

// Reviewer is one [[dispatch.workflow_push.reviewers]] entry. Command holds the placeholder
// {out} (the path of the output file) and "-" for stdin. Env lists names of environment
// variables the reviewer inherits, never a workflow token.
type Reviewer struct {
	Topic   string // supply-chain, secrets or injection
	Command []string
	Binds   []string
	Env     []string
}

// Node is a kenn-forge node under [dispatch.nodes.<name>]: the node's id on the fleet hub and
// the number of concurrent runs it takes. The node of [dispatch.forge] itself is "self", the
// empty node id, and is not listed here.
type Node struct {
	NodeID         string
	Slots          int
	ServeURL       string // base URL of lighthouse serve on the node
	ServeTokenFile string // absolute path of the bearer token file for ServeURL
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

				ServeURL       *string `toml:"serve_url"`
				ServeTokenFile *string `toml:"serve_token_file"`
			} `toml:"nodes"`
			WorkflowPush *struct {
				TokenFile *string `toml:"token_file"`
				Review    *string `toml:"review"`
				Timeout   *string `toml:"timeout"`
				Reviewers []struct {
					Topic   string   `toml:"topic"`
					Command []string `toml:"command"`
					Binds   []string `toml:"binds"`
					Env     []string `toml:"env"`
				} `toml:"reviewers"`
				Intent *struct {
					Command []string `toml:"command"`
					Binds   []string `toml:"binds"`
					Env     []string `toml:"env"`
				} `toml:"intent"`
			} `toml:"workflow_push"`
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
		if n.ServeURL == nil || *n.ServeURL == "" {
			return DefaultDispatch(), fmt.Errorf("%s.serve_url: missing; add serve_url = \"https://<node>:<port>\" to [%s]", key, key)
		}
		if err := checkServeURL(key+".serve_url", *n.ServeURL); err != nil {
			return DefaultDispatch(), err
		}
		if n.ServeTokenFile == nil || *n.ServeTokenFile == "" {
			return DefaultDispatch(), fmt.Errorf("%s.serve_token_file: missing; add serve_token_file = \"/absolute/path\" to [%s]", key, key)
		}
		if !filepath.IsAbs(*n.ServeTokenFile) {
			return DefaultDispatch(), fmt.Errorf("%s.serve_token_file: %q is not an absolute path", key, *n.ServeTokenFile)
		}
		if d.Nodes == nil {
			d.Nodes = map[string]Node{}
		}
		d.Nodes[name] = Node{NodeID: *n.NodeID, Slots: *n.Slots, ServeURL: *n.ServeURL, ServeTokenFile: *n.ServeTokenFile}
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
	if r.WorkflowPush != nil {
		var intent *Reviewer
		if in := r.WorkflowPush.Intent; in != nil {
			intent = &Reviewer{Command: in.Command, Binds: in.Binds, Env: in.Env}
		}
		wp, err := buildWorkflowPush(r.WorkflowPush.TokenFile, r.WorkflowPush.Review, r.WorkflowPush.Timeout, len(r.WorkflowPush.Reviewers), func(i int) Reviewer {
			rv := r.WorkflowPush.Reviewers[i]
			return Reviewer{Topic: rv.Topic, Command: rv.Command, Binds: rv.Binds, Env: rv.Env}
		}, intent)
		if err != nil {
			return DefaultDispatch(), err
		}
		d.WorkflowPush = wp
	}
	return d, nil
}

// buildWorkflowPush validates the raw [dispatch.workflow_push] values and expands the paths.
func buildWorkflowPush(tokenFile, review, timeout *string, n int, reviewer func(int) Reviewer, intent *Reviewer) (*WorkflowPush, error) {
	const key = "dispatch.workflow_push"
	wp := &WorkflowPush{Review: ReviewShadow, Timeout: DefaultPushTimeout}
	if tokenFile == nil || *tokenFile == "" {
		return nil, fmt.Errorf("%s.token_file: missing", key)
	}
	home, homeErr := os.UserHomeDir()
	if homeErr == nil && home == "" {
		homeErr = errors.New("empty")
	}
	if homeErr != nil {
		return nil, fmt.Errorf("%s: home directory unknown, cannot expand ~ or check binds: %w", key, homeErr)
	}
	wp.TokenFile = expandHome(*tokenFile, home)
	if review != nil {
		if *review != ReviewShadow && *review != ReviewEnforce {
			return nil, fmt.Errorf("%s.review: %q is neither %q nor %q", key, *review, ReviewShadow, ReviewEnforce)
		}
		wp.Review = *review
	}
	if err := setDuration(key+".timeout", timeout, &wp.Timeout); err != nil {
		return nil, err
	}
	if wp.Timeout < MinPushTimeout {
		return nil, fmt.Errorf("%s.timeout: %q is under the minimum of %s", key, *timeout, "10m")
	}
	protected := filepath.Join(home, ".config", "lighthouse")
	for i := range n {
		rv := reviewer(i)
		rkey := fmt.Sprintf("%s.reviewers[%d]", key, i)
		switch rv.Topic {
		case "supply-chain", "secrets", "injection":
		default:
			return nil, fmt.Errorf("%s.topic: %q is not supply-chain, secrets or injection", rkey, rv.Topic)
		}
		rv, err := checkReviewer(rkey, rv, home, protected)
		if err != nil {
			return nil, err
		}
		for _, prev := range wp.Reviewers {
			if prev.Topic == rv.Topic {
				return nil, fmt.Errorf("%s.topic: %s has a reviewer already; each reviewer needs its own topic", rkey, rv.Topic)
			}
		}
		wp.Reviewers = append(wp.Reviewers, rv)
	}
	if intent != nil {
		iv, err := checkReviewer(key+".intent", *intent, home, protected)
		if err != nil {
			return nil, err
		}
		wp.Intent = &iv
	}
	// Zero reviewers is a pusher without review: a post-merge change pushes, a pre-merge one
	// always asks. One reviewer alone is never a review (spec §4: two or three).
	if len(wp.Reviewers) == 1 {
		return nil, fmt.Errorf("%s.reviewers: one reviewer; the review needs two or three with distinct topics", key)
	}
	return wp, nil
}

// checkReviewer validates the command, env and binds of a reviewer or of the intent check and
// expands the binds; the topic is the caller's concern.
func checkReviewer(rkey string, rv Reviewer, home, protected string) (Reviewer, error) {
	if len(rv.Command) == 0 || rv.Command[0] == "" {
		return Reviewer{}, fmt.Errorf("%s.command: missing", rkey)
	}
	for _, name := range rv.Env {
		switch name {
		case "GH_TOKEN", "GITHUB_TOKEN":
			return Reviewer{}, fmt.Errorf("%s.env: %s is a token and never reaches a reviewer", rkey, name)
		}
	}
	binds := make([]string, 0, len(rv.Binds))
	for _, b := range rv.Binds {
		b = expandHome(b, home)
		if cb := filepath.Clean(b); cb == protected || strings.HasPrefix(cb, protected+string(filepath.Separator)) {
			return Reviewer{}, fmt.Errorf("%s.binds: %q is under %s", rkey, b, protected)
		}
		binds = append(binds, b)
	}
	rv.Binds = nil
	if len(binds) > 0 {
		rv.Binds = binds
	}
	return rv, nil
}

// expandHome replaces a leading ~ or ~/ in p with home; other paths stay as they are.
func expandHome(p, home string) string {
	if home == "" || (p != "~" && !strings.HasPrefix(p, "~/")) {
		return p
	}
	return filepath.Join(home, p[1:])
}

// checkServeURL accepts https, or http to a loopback host.
func checkServeURL(key, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("%s: %q is not a URL with a host", key, raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("%s: %q uses http to a host that is not loopback; use https", key, raw)
	}
	return fmt.Errorf("%s: %q must use https (or http to loopback)", key, raw)
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
