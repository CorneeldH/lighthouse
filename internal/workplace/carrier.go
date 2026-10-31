package workplace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// CarrierVersion is the version field of a written carrier.
const CarrierVersion = 1

var (
	// A lower-case UUID v4, as runs.NewSessionID makes it.
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	// A workspace id becomes a file name.
	safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
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
	CreatedAt string            `json:"created_at"`
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
	args := []string{"--permission-mode", "auto", "--strict-mcp-config"}
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
	return Carrier{
		Version: CarrierVersion, Repo: l.Repo, Issue: l.Issue,
		SessionID: l.SessionID, Mode: l.Mode, Profile: l.Profile,
		Args: args,
		Env: map[string]string{
			"LIGHTHOUSE_CONTROLLER_RUN": l.SessionID,
			"LIGHTHOUSE_STATE_DIR":      stateDir,
		},
		CreatedAt: now.UTC().Format(time.RFC3339),
	}, nil
}

// CarrierPath is <stateDir>/carriers/<workspaceID>.json.
func CarrierPath(stateDir, workspaceID string) string {
	return filepath.Join(stateDir, "carriers", workspaceID+".json")
}

func checkWorkspaceID(id string) error {
	if !safeIDPattern.MatchString(id) {
		return fmt.Errorf("carrier: workspace id %q is not a safe file name", id)
	}
	return nil
}

// WriteCarrier writes c atomically with mode 0600 and returns its path.
func WriteCarrier(stateDir, workspaceID string, c Carrier) (string, error) {
	if err := checkWorkspaceID(workspaceID); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	path := CarrierPath(stateDir, workspaceID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// MkdirAll leaves an existing directory as it is.
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".carrier-*.tmp")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after the rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

// RemoveCarrier deletes the carrier; a missing file is no error.
func RemoveCarrier(stateDir, workspaceID string) error {
	if err := checkWorkspaceID(workspaceID); err != nil {
		return err
	}
	if err := os.Remove(CarrierPath(stateDir, workspaceID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
