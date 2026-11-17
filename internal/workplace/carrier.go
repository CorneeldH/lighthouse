package workplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// CarrierVersion is the version field of a written carrier without unset.
const CarrierVersion = 1

// CarrierVersionUnset is the version of a carrier with unset. A target must refuse a version it
// does not know; a target that knows only version 1 then refuses to start an agent whose
// environment it would not clean, instead of starting it with the token in place.
const CarrierVersionUnset = 2

// pluginCacheReadRule lets a run read the files of its plugins without a prompt. A skill
// reads its own reference files from that cache, which lies outside the working directory,
// and in auto mode the classifier can ask for a permission that no one will give (#246).
// The flag takes several values, so the rule is joined with "=" to keep the next argument
// out of it.
const pluginCacheReadRule = "--allowedTools=Read(~/.claude/plugins/cache/**)"

var (
	// A lower-case UUID v4, as runs.NewSessionID makes it.
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	// SafeID is a workspace id or node that may become a file name.
	SafeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	// SafeSession is a session id that may become a path element and a pattern argument.
	SafeSession = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	// An environment variable name; it reaches an argv as `env -u <name>`.
	envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// Carrier is the file a forge target reads to start the agent. It never holds
// a token.
type Carrier struct {
	Version   int               `json:"version"`
	Repo      string            `json:"repo"`
	Issue     int               `json:"issue"`
	SessionID string            `json:"session_id"`
	Mode      string            `json:"mode"`
	Profile   string            `json:"profile"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	// Unset are the variables the target removes from the agent's environment before it starts
	// it, sorted; absent for a run that removes none.
	Unset     []string `json:"unset,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// NewCarrier validates l and builds the carrier for it.
func NewCarrier(l LaunchSpec, stateDir string, now time.Time) (Carrier, error) {
	if !uuidPattern.MatchString(l.SessionID) {
		return Carrier{}, fmt.Errorf("carrier: session id %q is not a UUID", l.SessionID)
	}
	if l.Mode != "start" && l.Mode != "resume" {
		return Carrier{}, fmt.Errorf("carrier: mode %q is neither start nor resume", l.Mode)
	}
	if !filepath.IsAbs(l.InstructionsFile) {
		return Carrier{}, fmt.Errorf("carrier: instructions file %q is not absolute", l.InstructionsFile)
	}
	if l.MCPConfig != "" && !filepath.IsAbs(l.MCPConfig) {
		return Carrier{}, fmt.Errorf("carrier: mcp config %q is not absolute", l.MCPConfig)
	}
	if strings.ContainsRune(l.Prompt, 0) {
		return Carrier{}, fmt.Errorf("carrier: prompt contains a NUL byte")
	}
	// The prompt is the last positional argument: claude would read "-x" as a flag.
	if strings.HasPrefix(l.Prompt, "-") {
		return Carrier{}, fmt.Errorf("carrier: prompt starts with %q", "-")
	}
	if !filepath.IsAbs(stateDir) {
		return Carrier{}, fmt.Errorf("carrier: state dir %q is not absolute", stateDir)
	}
	var unset []string
	for _, name := range l.Unset {
		if !envNamePattern.MatchString(name) {
			return Carrier{}, fmt.Errorf("carrier: unset %q is no environment variable name", name)
		}
		unset = append(unset, name)
	}
	slices.Sort(unset)
	unset = slices.Compact(unset)
	args := []string{"--permission-mode", "auto", "--strict-mcp-config", pluginCacheReadRule}
	if len(l.Tools) > 0 {
		// Evidence, claude 2.1.316: the CLI accepts --tools (`claude --tools=Read --help` runs, an
		// unknown flag fails with "unknown option"), and `claude --help` printed "--tools <tools...>
		// Specify the list of available tools from the built-in set" when this was written, which
		// another run of 2.1.316 did not show. That Bash is really unavailable under --tools is NOT
		// proven by a run: an authenticated check was not allowed here. --allowedTools is only
		// pre-approval and leaves Bash available (spec section 4), so it is no restriction. The flag is
		// variadic, so the value is joined with "=" and commas.
		// pluginCacheReadRule stays: it only pre-approves a read, so it grants nothing when Read
		// is not in Tools, and --tools stays the one thing that decides what exists.
		args = append(args, "--tools="+strings.Join(l.Tools, ","))
	}
	if l.MCPConfig != "" {
		args = append(args, "--mcp-config", l.MCPConfig)
	}
	args = append(args, "--append-system-prompt-file", l.InstructionsFile)
	if l.Mode == "start" {
		args = append(args, "--session-id", l.SessionID)
	} else {
		args = append(args, "--resume", l.SessionID)
	}
	if l.Native {
		args = append(args, "--model", "sonnet")
	}
	args = append(args, l.Prompt)
	version := CarrierVersion
	if len(unset) > 0 {
		version = CarrierVersionUnset
	}
	return Carrier{
		Version: version, Repo: l.Repo, Issue: l.Issue,
		SessionID: l.SessionID, Mode: l.Mode, Profile: l.Profile,
		Args: args,
		Env: map[string]string{
			"LIGHTHOUSE_CONTROLLER_RUN": l.SessionID,
			"LIGHTHOUSE_STATE_DIR":      stateDir,
		},
		Unset:     unset,
		CreatedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

// CarrierPath is <stateDir>/carriers/<workspaceID>.json on self (node ""), and
// <stateDir>/carriers/<node>/<workspaceID>.json on another node: two nodes can hand out the same
// workspace id.
func CarrierPath(stateDir, node, workspaceID string) string {
	if node == "" {
		return filepath.Join(stateDir, "carriers", workspaceID+".json")
	}
	return filepath.Join(stateDir, "carriers", node, workspaceID+".json")
}

// checkCarrierKey checks that the workspace id, and a node other than self, are safe file names.
func checkCarrierKey(node, id string) error {
	if !SafeID.MatchString(id) {
		return fmt.Errorf("carrier: workspace id %q is not a safe file name", id)
	}
	if node != "" && !SafeID.MatchString(node) {
		return fmt.Errorf("carrier: node %q is not a safe file name", node)
	}
	return nil
}

// WriteCarrier writes c atomically with mode 0600 and returns its path.
func WriteCarrier(stateDir, node, workspaceID string, c Carrier) (string, error) {
	if err := checkCarrierKey(node, workspaceID); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	path := CarrierPath(stateDir, node, workspaceID)
	if err := WriteFileAtomic(path, data); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveCarrier deletes the carrier; a missing file is no error.
func RemoveCarrier(stateDir, node, workspaceID string) error {
	if err := checkCarrierKey(node, workspaceID); err != nil {
		return err
	}
	if err := os.Remove(CarrierPath(stateDir, node, workspaceID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
