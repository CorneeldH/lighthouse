package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/automemory"
)

// memoryRepo makes a git repository in a temp dir, makes it the cwd and returns its main checkout
// as the scan sees it.
func memoryRepo(t *testing.T) string {
	t.Helper()
	for k, v := range map[string]string{
		"GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_SYSTEM": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1",
	} {
		t.Setenv(k, v)
	}
	repo := t.TempDir()
	ckGit(t, repo, "init", "-q")
	t.Chdir(repo)
	main, err := automemory.MainCheckout(repo)
	if err != nil {
		t.Fatal(err)
	}
	return main
}

func writeMemoryFile(t *testing.T, cfg, repo, name, content string) {
	t.Helper()
	dir := filepath.Join(cfg, "projects", automemory.EncodeProject(repo), "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryScanJSON(t *testing.T) {
	repo := memoryRepo(t)
	cfg := t.TempDir()
	writeMemoryFile(t, cfg, repo, "feedback_a.md", "---\nname: fa\ntype: feedback\n---\nbody\n")

	code, stdout, stderr := runCK(t, "memory", "scan", "--config-dir", cfg)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	var res automemory.ScanResult
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(res.Files) != 1 || res.Files[0].Destination != "instruction" || res.Files[0].Name != "fa" {
		t.Fatalf("Files = %+v, want one instruction file fa", res.Files)
	}
	if !strings.Contains(stdout, "\n  ") {
		t.Errorf("JSON is not indented: %q", stdout)
	}
}

func TestMemoryScanEmpty(t *testing.T) {
	memoryRepo(t)
	code, stdout, stderr := runCK(t, "memory", "scan", "--config-dir", t.TempDir())
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, `"files": []`) {
		t.Errorf("want an empty files list, got %s", stdout)
	}
}

func TestMemoryScanText(t *testing.T) {
	repo := memoryRepo(t)
	cfg := t.TempDir()
	writeMemoryFile(t, cfg, repo, "z_user.md", "---\nname: zu\ntype: user\n---\nb\n")
	writeMemoryFile(t, cfg, repo, "a_fb.md", "---\nname: af\ntype: feedback\n---\nb\n")

	code, stdout, stderr := runCK(t, "memory", "scan", "--format", "text", "--config-dir", cfg)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header and two rows, got %q", stdout)
	}
	if got := strings.Fields(lines[0]); strings.Join(got, " ") != "destination type name path" {
		t.Errorf("header = %q", lines[0])
	}
	// sorted on destination: drop before instruction
	if !strings.HasPrefix(lines[1], "drop") || !strings.HasPrefix(lines[2], "instruction") {
		t.Errorf("rows not sorted on destination: %q", stdout)
	}
}

func TestMemoryScanBadFormat(t *testing.T) {
	memoryRepo(t)
	code, _, stderr := runCK(t, "memory", "scan", "--format", "xml", "--config-dir", t.TempDir())
	if code != 2 || !strings.Contains(stderr, "memory scan: ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestMemoryScanNotARepo(t *testing.T) {
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	dir := t.TempDir()
	t.Chdir(dir)
	code, _, stderr := runCK(t, "memory", "scan", "--config-dir", t.TempDir())
	if code != 2 || !strings.Contains(stderr, "memory scan: ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}

func TestMemoryScanUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode 0o000 directory")
	}
	memoryRepo(t)
	cfg := t.TempDir()
	projects := filepath.Join(cfg, "projects")
	if err := os.Mkdir(projects, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(projects, 0o755) })

	code, _, stderr := runCK(t, "memory", "scan", "--config-dir", cfg)
	if code != 2 || !strings.HasPrefix(stderr, "memory scan: ") {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
}
