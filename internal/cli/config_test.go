package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Classification: these tests fail when the config command maps an outcome to the wrong exit
// code, writes a different stderr text, or when --json renders a value in another shape. They
// observe only the exit code, stdout and stderr of the whole command.

const configTOML = `[forge]
kind = "github"
repo = "acme/widgets"
[forge.board]
project_number = 7
project_id = "PVT_x"
status_field_id = "PVTSSF_s"
[forge.board.status_options]
"To do" = "a1"
"In progress" = "a2"
Done = "a3"
"On hold" = "a4"
[x]
when = 2026-01-02T03:04:05Z
`

func runConfig(t *testing.T, src string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lighthouse.toml")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LIGHTHOUSE_TOML", path)
	t.Setenv("LIGHTHOUSE_REPO_ROOT", dir)
	var out, errOut bytes.Buffer
	root := NewRoot(bytes.NewReader(nil), &out, &errOut)
	root.SetArgs(append([]string{"config"}, args...))
	code = execute(root, &errOut)
	return code, out.String(), errOut.String()
}

func TestConfigCommandExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantOut    string
		wantStderr string
	}{
		{"value", []string{"forge.repo"}, 0, "acme/widgets\n", ""},
		{"not set", []string{"forge.board.priority_field_id"}, 1, "", "config: forge.board.priority_field_id is not set\n"},
		{"table", []string{"forge.board"}, 2, "", "config: forge.board is a table, not a value\n"},
		{"datetime", []string{"x.when"}, 2, "", "config: x.when is a table, not a value\n"},
		{"default list", []string{"paths.search_dirs"}, 0, ".\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runConfig(t, configTOML, tt.args...)
			if code != tt.wantCode || out != tt.wantOut || errOut != tt.wantStderr {
				t.Errorf("got (%d, %q, %q), want (%d, %q, %q)", code, out, errOut, tt.wantCode, tt.wantOut, tt.wantStderr)
			}
		})
	}
}

func TestConfigInvalidFileExitsTwo(t *testing.T) {
	code, out, errOut := runConfig(t, "[forge\n", "forge.repo")
	if code != 2 || out != "" || len(errOut) < len("config: ") || errOut[:8] != "config: " {
		t.Errorf("got (%d, %q, %q), want rc 2, no stdout, stderr starting with %q", code, out, errOut, "config: ")
	}
}

func TestConfigJSON(t *testing.T) {
	t.Run("board", func(t *testing.T) {
		code, out, errOut := runConfig(t, configTOML, "--json", "forge.board")
		if code != 0 || errOut != "" {
			t.Fatalf("got (%d, %q), want rc 0 and no stderr", code, errOut)
		}
		var got struct {
			ProjectNumber *float64          `json:"project_number"`
			ProjectID     string            `json:"project_id"`
			StatusFieldID string            `json:"status_field_id"`
			StatusOptions map[string]string `json:"status_options"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("stdout %q is not JSON: %v", out, err)
		}
		if got.ProjectNumber == nil || *got.ProjectNumber != 7 || got.ProjectID != "PVT_x" ||
			got.StatusFieldID != "PVTSSF_s" || got.StatusOptions["In progress"] != "a2" {
			t.Errorf("board = %+v from %q", got, out)
		}
	})
	t.Run("default list", func(t *testing.T) {
		code, out, _ := runConfig(t, configTOML, "--json", "paths.search_dirs")
		if code != 0 || out != "[\".\"]\n" {
			t.Errorf("got (%d, %q), want (0, %q)", code, out, "[\".\"]\n")
		}
	})
	t.Run("datetime", func(t *testing.T) {
		code, out, _ := runConfig(t, configTOML, "--json", "x.when")
		if code != 0 || out != "\"2026-01-02T03:04:05Z\"\n" {
			t.Errorf("got (%d, %q)", code, out)
		}
	})
	t.Run("not set", func(t *testing.T) {
		code, out, errOut := runConfig(t, configTOML, "--json", "forge.board.priority_field_id")
		if code != 1 || out != "" || errOut != "config: forge.board.priority_field_id is not set\n" {
			t.Errorf("got (%d, %q, %q)", code, out, errOut)
		}
	})
}
