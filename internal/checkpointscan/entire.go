package checkpointscan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// EntireSettings is entire's effective settings, as far as the scan cares.
type EntireSettings struct {
	// SelfPush: whether entire may push checkpoints by itself (with --no-verify, past the hook).
	SelfPush bool
	// CheckpointProvider and CheckpointRepo are strategy_options.checkpoint_remote;
	// CheckpointRepo is empty when there is none.
	CheckpointProvider, CheckpointRepo string
	// PushRemote is strategy_options.checkpoint_push_remote: a remote name, or "".
	PushRemote string
}

func settingsError(path string, why any) error {
	return CouldNotCheck{Msg: fmt.Sprintf("cannot read %s: %v", path, why)}
}

// ReadEntireSettings reads `.entire/settings.json`, then `.entire/settings.local.json` on
// top: top-level keys replace, `strategy_options` keys merge.
func ReadEntireSettings(toplevel string) (EntireSettings, error) {
	merged := map[string]any{}
	strategy := map[string]any{}
	found := false
	for _, name := range []string{"settings.json", "settings.local.json"} {
		path := filepath.Join(toplevel, ".entire", name)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		found = true
		raw, err := os.ReadFile(path)
		if err != nil {
			return EntireSettings{}, settingsError(path, err)
		}
		var data any
		if err := json.Unmarshal(raw, &data); err != nil {
			return EntireSettings{}, settingsError(path, err)
		}
		obj, ok := data.(map[string]any)
		if !ok {
			return EntireSettings{}, settingsError(path, "not a JSON object")
		}
		switch so := obj["strategy_options"].(type) {
		case nil:
		case map[string]any:
			for k, v := range so {
				strategy[k] = v
			}
		default:
			return EntireSettings{}, settingsError(path, "strategy_options is not an object")
		}
		for k, v := range obj {
			merged[k] = v
		}
	}
	s := EntireSettings{}
	enabled := merged["enabled"] != false
	s.SelfPush = found && enabled && strategy["push_sessions"] != false
	switch ckpt := strategy["checkpoint_remote"].(type) {
	case nil:
	case map[string]any:
		if repo, ok := ckpt["repo"].(string); ok && repo != "" {
			s.CheckpointRepo = repo
			s.CheckpointProvider, _ = ckpt["provider"].(string)
		}
	default:
		return EntireSettings{}, CouldNotCheck{Msg: "cannot read .entire settings: checkpoint_remote is not an object"}
	}
	s.PushRemote, _ = strategy["checkpoint_push_remote"].(string)
	return s, nil
}

var forgeHosts = map[string]string{"github": "github.com", "gitlab": "gitlab.com"}

// space is what Python's `\s` matches (Unicode whitespace); Go's `\s` is ASCII only.
const space = `\s\p{Z}\x1c-\x1f\x85`

// urlRE is a URL a forge serves: `<scheme>://[userinfo@]host[:port]/<path>`. The userinfo
// holds no `@`, `/`, `\` or whitespace, so the host is the one after the only `@`; a query or
// a fragment does not match at all. Matched in full: a match on the tail of the URL let
// `https://notgithub.com/<owner>/<repo>` through.
var urlRE = regexp.MustCompile(`\A(?:https?|ssh|git)://(?:[^@/?#\\` + space + `]*@)?(?P<host>[A-Za-z0-9.-]+)(?::[0-9]+)?/(?P<path>[^?#\\` + space + `]*)\z`)

// scpRE is git's scp-like syntax, `[user@]host:<path>`: no slash before the colon.
var scpRE = regexp.MustCompile(`\A(?:[^@/:\\` + space + `]+@)?(?P<host>[A-Za-z0-9.-]+):(?P<path>[^\\` + space + `]*)\z`)

// IsCheckpointRemote reports whether a push to remote (url) is a push to the checkpoint
// remote: by the remote's name when entire has a checkpoint push remote, else by the URL of
// the checkpoint repo: the forge's host exactly, and exactly `<owner>/<repo>`, with `.git` or
// a trailing slash. The one rule for the pre-push hook and `push`.
func IsCheckpointRemote(s EntireSettings, remote, url string) bool {
	if s.PushRemote != "" && remote == s.PushRemote {
		return true
	}
	if s.CheckpointRepo == "" {
		return false
	}
	host, ok := forgeHosts[s.CheckpointProvider]
	if !ok {
		return false
	}
	re := urlRE
	hit := re.FindStringSubmatch(url)
	if hit == nil {
		re = scpRE
		hit = re.FindStringSubmatch(url)
	}
	if hit == nil || !strings.EqualFold(hit[re.SubexpIndex("host")], host) {
		return false
	}
	path := hit[re.SubexpIndex("path")]
	return path == s.CheckpointRepo || path == s.CheckpointRepo+".git" || path == s.CheckpointRepo+"/"
}
