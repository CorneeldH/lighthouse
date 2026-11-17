package hostconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// NodeServe holds the [node] table of host.toml: what lighthouse serve on a node needs.
type NodeServe struct {
	TokenFile string // absolute path of the bearer token file the node-API checks
}

// LoadNode reads [node] of the host.toml at path. The bool reports whether [node] is present:
// a missing file, an empty path or a file without [node] gives false and no error.
func LoadNode(path string) (NodeServe, bool, error) {
	if path == "" {
		return NodeServe{}, false, nil
	}
	var raw struct {
		Node *struct {
			TokenFile *string `toml:"token_file"`
		} `toml:"node"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return NodeServe{}, false, nil
		}
		return NodeServe{}, false, fmt.Errorf("read %s: %w", path, err)
	}
	if raw.Node == nil {
		return NodeServe{}, false, nil
	}
	if raw.Node.TokenFile == nil || *raw.Node.TokenFile == "" {
		return NodeServe{}, false, fmt.Errorf("node.token_file: missing; add token_file = \"/absolute/path\" to [node]")
	}
	if !filepath.IsAbs(*raw.Node.TokenFile) {
		return NodeServe{}, false, fmt.Errorf("node.token_file: %q is not an absolute path", *raw.Node.TokenFile)
	}
	return NodeServe{TokenFile: *raw.Node.TokenFile}, true, nil
}

// ValidateNodeForge checks that the token file of [dispatch.forge] exists and is not empty,
// which a node needs to read its own forge.
func ValidateNodeForge(d Dispatch) error {
	b, err := os.ReadFile(d.Forge.TokenFile)
	if err != nil {
		return fmt.Errorf("dispatch.forge.token_file: %w", err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return fmt.Errorf("dispatch.forge.token_file: %s is empty", d.Forge.TokenFile)
	}
	return nil
}
