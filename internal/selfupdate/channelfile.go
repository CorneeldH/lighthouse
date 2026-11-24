package selfupdate

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ChannelPath is ~/.config/lighthouse/channel, or "" without a home directory. It is deliberately
// not derived from hostconfig.Path(): LIGHTHOUSE_HOST_TOML must not move the channel, because the
// update timer does not see that variable.
func ChannelPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".config", "lighthouse", "channel")
}

// ReadChannel reads the stored channel. A missing file is the release channel; empty or unknown
// content is an error naming path, never a silent release.
func ReadChannel(path string) (Channel, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ChannelRelease, nil
	}
	if err != nil {
		return "", fmt.Errorf("read channel %s: %w", path, err)
	}
	c, err := ParseChannel(strings.TrimSpace(string(b)))
	if err != nil {
		return "", fmt.Errorf("channel file %s: %w", path, err)
	}
	return c, nil
}

// WriteChannel stores c at path, creating the directory.
func WriteChannel(path string, c Channel) error {
	if _, err := ParseChannel(string(c)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(string(c)+"\n"), 0o644)
}
