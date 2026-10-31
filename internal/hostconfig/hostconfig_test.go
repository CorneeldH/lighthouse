package hostconfig

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPath(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_HOST_TOML", "/a/b.toml")
		if got := Path(); got != "/a/b.toml" {
			t.Errorf("Path() = %q, want /a/b.toml", got)
		}
	})
	t.Run("home default", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_HOST_TOML", "")
		t.Setenv("HOME", "/h")
		if got := Path(); got != "/h/.config/lighthouse/host.toml" {
			t.Errorf("Path() = %q, want /h/.config/lighthouse/host.toml", got)
		}
	})
}

func writeHost(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "host.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDispatchDefaults(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	want := Dispatch{
		Slots:             4,
		MaxSoftware:       2,
		MinMemAvailableGB: 8,
		ReservePerRunGB:   3,
		Night:             "21:00-08:00",
		MaxRuntime:        8 * time.Hour,
		MaxWait:           time.Hour,
		AnswerPermissions: []string{"admin", "maintain"},
		Forge: DispatchForge{
			URL:       "http://127.0.0.1:8091",
			TokenFile: filepath.Join(home, ".kenn", "forge", "auth_token"),
		},
	}
	cases := map[string]string{
		"no path":         "",
		"missing file":    filepath.Join(t.TempDir(), "nope.toml"),
		"no dispatch tbl": writeHost(t, "[usage]\nhalt_pct_week = 90\n"),
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := LoadDispatch(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadDispatchRepos(t *testing.T) {
	p := writeHost(t, "[dispatch]\nrepos = [\"/srv/b\", \"/srv/a\"]\nslots = 6\nmax_runtime = \"2h30m\"\n"+
		"[dispatch.forge]\nurl = \"http://x:1\"\n")
	got, err := LoadDispatch(p)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Repos, []string{"/srv/b", "/srv/a"}) {
		t.Errorf("Repos = %v", got.Repos)
	}
	if got.Slots != 6 || got.MaxSoftware != 2 {
		t.Errorf("Slots/MaxSoftware = %d/%d, want 6/2", got.Slots, got.MaxSoftware)
	}
	if got.MaxRuntime != 150*time.Minute || got.MaxWait != time.Hour {
		t.Errorf("MaxRuntime/MaxWait = %v/%v", got.MaxRuntime, got.MaxWait)
	}
	if got.Forge.URL != "http://x:1" || got.Forge.TokenFile == "" {
		t.Errorf("Forge = %+v, want url overridden and default token file kept", got.Forge)
	}
}

func TestLoadDispatchRelativeRepo(t *testing.T) {
	_, err := LoadDispatch(writeHost(t, "[dispatch]\nrepos = [\"/ok\", \"rel/repo\"]\n"))
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{"dispatch.repos", "rel/repo"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q lacks %q", err, s)
		}
	}
}

func TestLoadDispatchInvalid(t *testing.T) {
	cases := []struct{ body, key string }{
		{"slots = 0", "dispatch.slots"},
		{"max_software = 0", "dispatch.max_software"},
		{"min_mem_available_gb = -1", "dispatch.min_mem_available_gb"},
		{"reserve_per_run_gb = -2", "dispatch.reserve_per_run_gb"},
		{`night = "21-08"`, "dispatch.night"},
		{`night = "25:00-08:00"`, "dispatch.night"},
		{`max_wait = "soon"`, "dispatch.max_wait"},
		{`max_runtime = "0s"`, "dispatch.max_runtime"},
		{`max_runtime = "-1h"`, "dispatch.max_runtime"},
	}
	for _, c := range cases {
		t.Run(c.body, func(t *testing.T) {
			_, err := LoadDispatch(writeHost(t, "[dispatch]\n"+c.body+"\n"))
			if err == nil {
				t.Fatal("want error")
			}
			if !strings.Contains(err.Error(), c.key) {
				t.Errorf("error %q lacks key %q", err, c.key)
			}
		})
	}
}

func TestLoadDispatchParseErrorNamesPath(t *testing.T) {
	p := writeHost(t, "[dispatch\nslots = \n")
	_, err := LoadDispatch(p)
	if err == nil || !strings.Contains(err.Error(), p) {
		t.Errorf("err = %v, want it to name %s", err, p)
	}
}

func TestLoadDispatchProfiles(t *testing.T) {
	p := writeHost(t, "[dispatch.profiles.software]\ntarget_key = \"paia-software\"\nmcp_config = \"/etc/mcp.json\"\n")
	got, err := LoadDispatch(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Profile{"software": {TargetKey: "paia-software", MCPConfig: "/etc/mcp.json"}}
	if !reflect.DeepEqual(got.Profiles, want) {
		t.Errorf("Profiles = %+v, want %+v", got.Profiles, want)
	}
}
