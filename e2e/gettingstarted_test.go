//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A production change that fails this test: a change to the config reader that rejects or
// misreads a key of the lighthouse.toml example in docs/getting-started.md, so a reader who
// copies the example gets an error or another value.
func TestConfigExampleLoads(t *testing.T) {
	guide, err := os.ReadFile(filepath.Join("..", "docs", "getting-started.md"))
	if err != nil {
		t.Fatal(err)
	}
	sec := regexp.MustCompile(`(?ms)^## Configure lighthouse.toml\n(.*?)(?:^## |\z)`).FindSubmatch(guide)
	if sec == nil {
		t.Fatal("docs/getting-started.md lacks '## Configure lighthouse.toml'")
	}
	block := regexp.MustCompile("(?s)```toml\n(.*?)```").FindSubmatch(sec[1])
	if block == nil {
		t.Fatal("Configure lighthouse.toml has no toml block")
	}
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "lighthouse.toml"), block[1], 0o644); err != nil {
		t.Fatal(err)
	}
	bin := buildLighthouse(t)
	env := append(cleanEnv(), "LIGHTHOUSE_REPO_ROOT="+repo)
	for key, want := range map[string]string{"project.language": "en", "forge.kind": "github"} {
		cmd := exec.Command(bin, "config", key)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("lighthouse config %s: %v", key, err)
		}
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("lighthouse config %s = %q, want %q", key, got, want)
		}
	}
}
