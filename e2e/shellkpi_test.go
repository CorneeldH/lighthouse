//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// kpiRows is the (command, target) of each row of the `## KPIs` table in skill.
func kpiRows(t *testing.T, skill string) [][2]string {
	t.Helper()
	_, after, ok := strings.Cut(skill, "## KPIs")
	if !ok {
		t.Fatal("no ## KPIs section in the skill")
	}
	section, _, _ := strings.Cut(after, "\n## ")
	var rows [][2]string
	for _, line := range strings.Split(section, "\n") {
		cells := splitCells(strings.Trim(strings.TrimSpace(line), "|"))
		if len(cells) == 3 && strings.HasPrefix(cells[1], "`") {
			command := strings.ReplaceAll(strings.Trim(cells[1], "`"), `\|`, "|")
			rows = append(rows, [2]string{command, cells[2]})
		}
	}
	return rows
}

// splitCells splits a markdown table line on every `|` that is not escaped, and trims the cells.
func splitCells(line string) []string {
	var cells []string
	start := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '|' && (i == 0 || line[i-1] != '\\') {
			cells = append(cells, strings.TrimSpace(line[start:i]))
			start = i + 1
		}
	}
	return append(cells, strings.TrimSpace(line[start:]))
}

func TestShellKPI(t *testing.T) {
	bin := buildLighthouse(t)

	// newRepo is a repository with the shell pack installed and committed, and its KPI rows from
	// the installed skill, not from pack/.
	newRepo := func(t *testing.T) (string, [][2]string) {
		t.Helper()
		repo := shellRepo(t, bin)
		git(t, repo, "add", "-A")
		git(t, repo, "commit", "-qm", "install")
		skill, err := os.ReadFile(filepath.Join(repo, ".claude", "skills", "lighthouse-shell-standard", "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		return repo, kpiRows(t, string(skill))
	}
	// kpi runs one KPI command in repo and returns its rc; shellcheck is the pinned one when
	// wanted, LIGHTHOUSE_TOML when toml is not empty.
	kpi := func(t *testing.T, repo, command string, shellcheck bool, toml string) int {
		t.Helper()
		env := append(hookEnv(bin), gitEnv...)
		env = overrideEnv(env, "SHELLCHECK_BIN=", "LIGHTHOUSE_REPO_ROOT="+repo)
		if shellcheck {
			env = overrideEnv(env, "SHELLCHECK_BIN="+pinnedShellcheck(t))
		}
		if toml != "" {
			env = overrideEnv(env, "LIGHTHOUSE_TOML="+toml)
		}
		code, _, _ := runIn(t, repo, env, "bash", "-c", command)
		return code
	}

	t.Run("commands_run", func(t *testing.T) {
		skipOnWindows(t, "shellcheck has no MINGW release, so the pinned shellcheck cannot be fetched")
		// Each KPI command runs in a repo with the shell pack installed and meets its target
		// there: a KPI row nobody can run measures nothing.
		repo, rows := newRepo(t)
		if len(rows) < 3 {
			t.Fatalf("%d KPI rows, want at least 3: %v", len(rows), rows)
		}
		for _, row := range rows {
			m := regexp.MustCompile(`^rc (\d)$`).FindStringSubmatch(row[1])
			if m == nil {
				t.Fatalf("target %q is not `rc N`", row[1])
			}
			want, _ := strconv.Atoi(m[1])
			if got := kpi(t, repo, row[0], true, ""); got != want {
				t.Errorf("%s: rc %d, want %d", row[0], got, want)
			}
		}
	})

	t.Run("rows_go_red", func(t *testing.T) {
		skipOnWindows(t, "shellcheck has no MINGW release, so the pinned shellcheck cannot be fetched")
		// Each row misses its target once the thing it measures is broken.
		repo, rows := newRepo(t)
		if len(rows) != 3 {
			t.Fatalf("%d KPI rows, want 3: %v", len(rows), rows)
		}
		strict, allShell, ignored := rows[0][0], rows[1][0], rows[2][0]
		writeFile(t, repo, ".claude/hooks/style.sh", styleOnlySh)
		if got := kpi(t, repo, strict, true, ""); got == 0 {
			t.Error("the strict row stays green on a style finding in .claude/hooks")
		}
		writeFile(t, repo, "tools/warn.bash", brokenSh)
		if got := kpi(t, repo, allShell, true, ""); got == 0 {
			t.Error("the all-shell row stays green on a warning in a .bash file")
		}
		writeFile(t, repo, ".gitignore", "lib/\n")
		writeFile(t, repo, ".claude/hooks/lib/a.bash", "#!/bin/bash\n")
		if got := kpi(t, repo, ignored, true, ""); got == 1 {
			t.Error("the gitignore row stays rc 1 (green) with a gate script outside git")
		}
	})

	t.Run("gitignore_row_is_not_green_on_broken_config", func(t *testing.T) {
		repo, rows := newRepo(t)
		toml := tomlFile(t, "[shell]\nstrict_dirs = \".claude/hooks\"\n")
		if got := kpi(t, repo, rows[2][0], false, toml); got == 1 {
			t.Error("the gitignore row is rc 1 (green) on a config it cannot read")
		}
	})

	t.Run("gitignore_row_handles_a_space", func(t *testing.T) {
		repo, rows := newRepo(t)
		toml := tomlFile(t, "[shell]\nstrict_dirs = [\"my hooks\"]\n")
		writeFile(t, repo, ".gitignore", "lib/\n")
		writeFile(t, repo, "my hooks/lib/a.sh", "#!/bin/bash\n")
		if got := kpi(t, repo, rows[2][0], false, toml); got != 0 {
			t.Errorf("the gitignore row: rc %d, want 0 for a directory name with a space", got)
		}
	})
}
