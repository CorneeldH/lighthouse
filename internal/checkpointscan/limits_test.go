package checkpointscan

import (
	"os"
	"path/filepath"
	"testing"
)

func hostToml(t *testing.T, extra string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host.toml")
	noErr(t, os.WriteFile(path, []byte("[host]\nname = \"test\"\n"+extra), 0o600))
	t.Setenv("LIGHTHOUSE_HOST_TOML", path)
}

func TestLoadLimitsDefaultsWhenUnset(t *testing.T) {
	t.Run("host.toml absent", func(t *testing.T) {
		t.Setenv("LIGHTHOUSE_HOST_TOML", filepath.Join(t.TempDir(), "absent.toml"))
		got, err := LoadLimits()
		noErr(t, err)
		eq(t, Limits{MemoryMax: "2G", CPUQuota: "200%"}, got)
	})
	t.Run("host.toml without the keys", func(t *testing.T) {
		hostToml(t, "")
		got, err := LoadLimits()
		noErr(t, err)
		eq(t, Limits{MemoryMax: "2G", CPUQuota: "200%"}, got)
	})
}

func TestLoadLimitsFromHostToml(t *testing.T) {
	hostToml(t, "scan_memory_max = \"1G\"\nscan_cpu_quota = \"100%\"\ngate_slice = \"s.slice\"\n")
	got, err := LoadLimits()
	noErr(t, err)
	slice := "s.slice"
	eq(t, Limits{MemoryMax: "1G", CPUQuota: "100%", Slice: &slice}, got)
}

func TestLoadLimitsInvalidHostTomlIsCouldNotCheck(t *testing.T) {
	hostToml(t, "scan_memory_max = 5\n")
	_, err := LoadLimits()
	contains(t, cnc(t, err).Msg, "scan_memory_max")
}
