// Package hostconfig locates host.toml, the per-host settings file outside the repository.
package hostconfig

import (
	"os"
	"path/filepath"
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
