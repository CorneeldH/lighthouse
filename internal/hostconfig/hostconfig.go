// Package hostconfig locates host.toml, the per-host settings file outside the repository.
package hostconfig

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
	Forge             DispatchForge
	Profiles          map[string]Profile
}

// DispatchForge locates the forge and its token.
type DispatchForge struct {
	URL       string
	TokenFile string
}

// Profile is a run profile under [dispatch.profiles.<name>].
type Profile struct {
	TargetKey string `toml:"target_key"`
	MCPConfig string `toml:"mcp_config"`
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
			Forge             struct {
				URL       *string `toml:"url"`
				TokenFile *string `toml:"token_file"`
			} `toml:"forge"`
			Profiles map[string]Profile `toml:"profiles"`
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
	if r.Forge.TokenFile != nil {
		d.Forge.TokenFile = *r.Forge.TokenFile
	}
	d.Profiles = r.Profiles
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
