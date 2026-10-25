package usage

import (
	"errors"
	"io/fs"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds the alarm thresholds and the cache lifetime.
type Config struct {
	HaltPctSession int           // session percentage at which the alarm goes up
	HaltPctWeek    int           // week percentage at which the alarm goes up
	TTL            time.Duration // age after which the cache is stale
}

// DefaultConfig returns the built-in thresholds: 85, 95 and 300 s.
func DefaultConfig() Config {
	return Config{HaltPctSession: 85, HaltPctWeek: 95, TTL: 300 * time.Second}
}

// LoadConfig reads the [usage] table of the host.toml at path. A missing file, an empty
// path or a missing key gives the default. A key outside its range (threshold 1-100,
// TTL >= 0) falls back to its own default, not the whole table. A TOML parse error
// gives DefaultConfig and the error.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	var raw struct {
		Usage struct {
			HaltPctSession  *int `toml:"halt_pct_session"`
			HaltPctWeek     *int `toml:"halt_pct_week"`
			CacheTTLSeconds *int `toml:"cache_ttl_seconds"`
		} `toml:"usage"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return DefaultConfig(), err
	}
	u := raw.Usage
	if v := u.HaltPctSession; v != nil && *v >= 1 && *v <= 100 {
		cfg.HaltPctSession = *v
	}
	if v := u.HaltPctWeek; v != nil && *v >= 1 && *v <= 100 {
		cfg.HaltPctWeek = *v
	}
	if v := u.CacheTTLSeconds; v != nil && *v >= 0 {
		cfg.TTL = time.Duration(*v) * time.Second
	}
	return cfg, nil
}
