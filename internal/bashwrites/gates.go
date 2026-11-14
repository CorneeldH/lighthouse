package bashwrites

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// hookDir is the directory of the gates, under the repo root.
const hookDir = ".claude/hooks/lighthouse"

// Gates are the gates of each extension, in the order they run. A rewriting gate always comes
// before code-language-check.sh, so that one reads the version after `ruff format` or `gofmt`.
var Gates = map[string][]string{
	".sh":       {"shell-lint-check.sh", "code-language-check.sh"},
	".bash":     {"shell-lint-check.sh", "code-language-check.sh"},
	".py":       {"ruff-format-check.sh", "code-language-check.sh"},
	".go":       {"gofmt-check.sh", "code-language-check.sh"},
	".md":       {"md-link-check"},
	".markdown": {"md-link-check"},
}

// GatePacks is the package that supplies each gate; a missing gate's notice names it.
var GatePacks = map[string]string{
	"shell-lint-check.sh":    "shell",
	"ruff-format-check.sh":   "python",
	"gofmt-check.sh":         "go",
	"md-link-check":          "core",
	"code-language-check.sh": "core",
}

// GateArgv holds a gate that runs as `lighthouse hook <name>`: it has no script of its own, and
// `run.sh <name>` in the same directory runs it. Every other gate is the script named by the gate.
var GateArgv = map[string][]string{
	"md-link-check":          {"run.sh", "md-link-check"},
	"code-language-check.sh": {"run.sh", "code-language-check"},
}

// GatePath is the path of the file that runs gate under repoRoot.
func GatePath(repoRoot, gate string) string {
	name := gate
	if argv, ok := GateArgv[gate]; ok {
		name = argv[0]
	}
	return filepath.Join(repoRoot, hookDir, name)
}

// GateLabel is the name of gate in the output: the script name, `<name>.sh` for a run.sh gate, so
// the text a user sees did not change when the gate moved into `lighthouse hook`.
func GateLabel(gate string) string {
	if strings.HasSuffix(gate, ".sh") {
		return gate
	}
	return gate + ".sh"
}

const cannotCheck = "COULD NOT CHECK"

// Classify is the verdict of a gate: "ok", "finding" or "unchecked". rc 0 is ok; rc 2 is a
// finding unless the first output line holds COULD NOT CHECK (shellcheck and ruff quote source
// lines, so a file that contains the token must not undo a real verdict); every other rc is
// unchecked.
func Classify(rc int, output []byte) string {
	if rc == 0 {
		return "ok"
	}
	if rc != 2 {
		return "unchecked"
	}
	first, _, _ := strings.Cut(decodeReplace(output), "\n")
	if strings.Contains(first, cannotCheck) {
		return "unchecked"
	}
	return "finding"
}

// MarkerKey is the tool_use_id, otherwise session_id + "-" + sha256(tool_input.command)[:16]. A "/"
// is replaced, so the marker name cannot fall outside the state directory. A command that is not a
// string, without a tool_use_id, is an error (the Python crashes there).
func MarkerKey(raw map[string]json.RawMessage) (string, error) {
	var key string
	if id, ok := truthyText(raw["tool_use_id"]); ok {
		key = id
	} else {
		command := ""
		if input, ok := raw["tool_input"]; ok {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(input, &fields); err != nil {
				return "", errors.New("tool_input is not an object")
			}
			if c, ok := fields["command"]; ok {
				if err := json.Unmarshal(c, &command); err != nil {
					return "", errors.New("command is not a string")
				}
			}
		}
		sum := sha256.Sum256([]byte(command))
		session := ""
		if s, ok := raw["session_id"]; ok {
			if err := json.Unmarshal(s, &session); err != nil {
				session = strings.TrimSpace(string(s))
			}
		}
		key = session + "-" + hex.EncodeToString(sum[:])[:16]
	}
	return strings.ReplaceAll(key, "/", "_"), nil
}

// truthyText is str(value) for a JSON value that Python finds truthy.
func truthyText(raw json.RawMessage) (string, bool) {
	text := strings.TrimSpace(string(raw))
	switch text {
	case "", "null", "false", "0", `""`, "[]", "{}", "0.0":
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return text, true
}

// ManifestError means `.lighthouse/manifest.toml` exists but cannot be read as a manifest.
type ManifestError struct{ Msg string }

func (e *ManifestError) Error() string { return e.Msg }

// ManagedFiles are the file units of `<root>/.lighthouse/manifest.toml`: repo-relative path to
// sha256. Those files are lighthouse's own: a gate that rewrites one makes `init --upgrade` refuse
// it. A block, hook or permission unit sits inside a file of the project, so it does not count.
// The manifest is read leniently, as the Python does: only `unit` rows with kind "file" count and
// everything else is ignored. No manifest is an empty map.
func ManagedFiles(root string) (map[string]string, error) {
	path := filepath.Join(root, ".lighthouse", "manifest.toml")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return map[string]string{}, nil
	}
	fail := func(cause error) error {
		return &ManifestError{Msg: "cannot read " + path + ": " + cause.Error()}
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, fail(err)
	}
	var data map[string]any
	if _, err := toml.Decode(string(text), &data); err != nil {
		return nil, fail(err)
	}
	units, _ := data["unit"].([]map[string]any)
	managed := map[string]string{}
	for _, unit := range units {
		if unit["kind"] != "file" {
			continue
		}
		p, pOK := unit["path"].(string)
		sum, sOK := unit["sha256"].(string)
		if !pOK || !sOK {
			return nil, fail(fmt.Errorf("a file unit needs a string path and sha256, got %v", unit))
		}
		managed[p] = sum
	}
	return managed, nil
}

// asInstalled says whether rel is a file unit whose bytes are still the sha256 that init
// recorded. Only then is there nothing for a gate to judge.
func asInstalled(root, rel string, managed map[string]string) bool {
	want, ok := managed[rel]
	if !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]) == want
}
