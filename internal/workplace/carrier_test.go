package workplace

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testUUID = "0b8f3a52-6c1e-4d7a-9f20-3a1c5e7b9d11"

func baseSpec() LaunchSpec {
	return LaunchSpec{
		Repo: "CorneeldH/lighthouse", Issue: 113, SessionID: testUUID,
		Mode: "start", Profile: "dev", Prompt: "do the thing",
		Native: true, InstructionsFile: "/abs/instr.md", MCPConfig: "/abs/mcp.json",
	}
}

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func TestNewCarrierArgsStart(t *testing.T) {
	c, err := NewCarrier(baseSpec(), "/state", testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--permission-mode", "auto", "--strict-mcp-config",
		"--mcp-config", "/abs/mcp.json",
		"--append-system-prompt-file", "/abs/instr.md",
		"--session-id", testUUID,
		"--model", "sonnet",
		"do the thing",
	}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args:\n got %q\nwant %q", c.Args, want)
	}
	wantEnv := map[string]string{"LIGHTHOUSE_CONTROLLER_RUN": testUUID, "LIGHTHOUSE_STATE_DIR": "/state"}
	if !reflect.DeepEqual(c.Env, wantEnv) {
		t.Errorf("env: got %v, want %v", c.Env, wantEnv)
	}
	if c.Version != 1 || c.Repo != "CorneeldH/lighthouse" || c.Issue != 113 ||
		c.SessionID != testUUID || c.Mode != "start" || c.Profile != "dev" ||
		c.CreatedAt != "2026-10-31T12:00:00Z" {
		t.Errorf("fields: %+v", c)
	}
}

func TestNewCarrierArgsResumeNoMCP(t *testing.T) {
	l := baseSpec()
	l.Mode, l.MCPConfig, l.Native = "resume", "", false
	c, err := NewCarrier(l, "/state", testNow)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"--permission-mode", "auto", "--strict-mcp-config",
		"--append-system-prompt-file", "/abs/instr.md",
		"--resume", testUUID,
		"do the thing",
	}
	if !reflect.DeepEqual(c.Args, want) {
		t.Errorf("args:\n got %q\nwant %q", c.Args, want)
	}
}

func TestNewCarrierRejects(t *testing.T) {
	cases := map[string]func(*LaunchSpec){
		"session not uuid":   func(l *LaunchSpec) { l.SessionID = "abc" },
		"session upper case": func(l *LaunchSpec) { l.SessionID = strings.ToUpper(testUUID) },
		"mode x":             func(l *LaunchSpec) { l.Mode = "x" },
		"relative instr":     func(l *LaunchSpec) { l.InstructionsFile = "instr.md" },
		"empty instr":        func(l *LaunchSpec) { l.InstructionsFile = "" },
		"relative mcp":       func(l *LaunchSpec) { l.MCPConfig = "mcp.json" },
		"NUL in prompt":      func(l *LaunchSpec) { l.Prompt = "a\x00b" },
	}
	for name, mutate := range cases {
		l := baseSpec()
		mutate(&l)
		if _, err := NewCarrier(l, "/state", testNow); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestWriteCarrierAtomic0600(t *testing.T) {
	dir := t.TempDir()
	c, err := NewCarrier(baseSpec(), dir, testNow)
	if err != nil {
		t.Fatal(err)
	}
	path, err := WriteCarrier(dir, "ws-1", c)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "carriers", "ws-1.json"); path != want || CarrierPath(dir, "ws-1") != want {
		t.Errorf("path %q, CarrierPath %q, want %q", path, CarrierPath(dir, "ws-1"), want)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(path)
	var back Carrier
	if err := json.Unmarshal(raw, &back); err != nil || back.Version != 1 || back.SessionID != testUUID {
		t.Errorf("read back: %+v err=%v", back, err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "carriers"))
	if len(entries) != 1 {
		t.Errorf("want only the carrier in the dir, got %d entries", len(entries))
	}
	if err := RemoveCarrier(dir, "ws-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("carrier still present: %v", err)
	}
	if err := RemoveCarrier(dir, "ws-1"); err != nil {
		t.Errorf("removing a missing carrier: %v", err)
	}
}

func TestWriteCarrierRejectsWorkspaceID(t *testing.T) {
	dir := t.TempDir()
	c, _ := NewCarrier(baseSpec(), dir, testNow)
	for _, id := range []string{"../x", ".x", "a/b", ""} {
		if _, err := WriteCarrier(dir, id, c); err == nil {
			t.Errorf("id %q: want error", id)
		}
		if err := RemoveCarrier(dir, id); err == nil {
			t.Errorf("remove id %q: want error", id)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("something was written under the state dir: %v (err %v)", entries, err)
	}
}

func TestCarrierHasNoToken(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_secret")
	t.Setenv("GITHUB_TOKEN", "gho_secret")
	dir := t.TempDir()
	c, _ := NewCarrier(baseSpec(), dir, testNow)
	path, err := WriteCarrier(dir, "ws-1", c)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, bad := range []string{"ghp_secret", "gho_secret"} {
		if bytes.Contains(raw, []byte(bad)) {
			t.Errorf("carrier contains %q", bad)
		}
	}
	var back Carrier
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	for k := range back.Env {
		if strings.Contains(strings.ToUpper(k), "TOKEN") {
			t.Errorf("carrier env has a token key %q", k)
		}
	}
}
