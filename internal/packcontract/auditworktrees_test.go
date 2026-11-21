package packcontract

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// audit-worktrees: the skill describes what `lighthouse worktree-audit` prints, so its table
// header, its KPI fields and the `cleanup:` tokens are read from a run of the real command
// (this test binary acting as lighthouse) in a fresh repo. What stays in paia (the opencode lane
// with its port registry, board-sweep) is named only if present, and the protect list comes
// from `[worktree_audit]` in `lighthouse.toml`.

const auditWorktrees = "core/skills/audit-worktrees"

// auditRepo is a git repo with one commit on `main`, pushed to a bare origin, next to a
// lighthouse.toml without `forge.repo`; it returns the directory holding them.
func auditRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "README.md"), "# repo\n")
	writeFile(t, filepath.Join(root, "lighthouse.toml"), "[project]\nlanguage = \"en\"\n")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "README.md"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-qm", "init"},
		{"init", "-q", "--bare", filepath.Join(root, "origin.git")},
		{"remote", "add", "origin", filepath.Join(root, "origin.git")},
		{"push", "-q", "origin", "main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "HOME="+root, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return root
}

// runAudit runs `lighthouse worktree-audit <args>` in the repo of auditRepo and returns stdout and
// the exit code.
func runAudit(t *testing.T, root string, args ...string) (string, int) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, append([]string{"worktree-audit"}, args...)...)
	cmd.Dir = filepath.Join(root, "repo")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + root,
		"LIGHTHOUSE_TOML=" + filepath.Join(root, "lighthouse.toml"),
		"GIT_CONFIG_GLOBAL=/dev/null",
		envActAsLighthouse + "=1",
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	code := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	if code != 0 && code != 1 {
		t.Fatalf("worktree-audit %v: exit %d\n%s", args, code, stderr.String())
	}
	return string(out), code
}

// Classification: fails when the skill stops naming the command line of a view, or the binary
// loses one of the view flags.
func TestAuditWorktreesEveryViewRunsTheBinary(t *testing.T) {
	cliFlags(t, []string{"worktree-audit"}, "cleanup", "sessions", "origin", "kpi", "only", "issue")
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			block := section(t, body, "```bash\nlighthouse worktree-audit ", "```")
			wantAll(t, block,
				"\nlighthouse worktree-audit            # --cleanup (default)\n",
				"\nlighthouse worktree-audit --sessions\n",
				"\nlighthouse worktree-audit --origin\n",
				"\nlighthouse worktree-audit --kpi\n",
				"\nlighthouse worktree-audit --only <branch>\n",
				"\nlighthouse worktree-audit --issue <nr>\n")
		})
	}
}

// Classification: fails when the skill names other `cleanup:` tokens than the command prints,
// or stops pointing `--only`/`--issue` at lighthouse-close-issue. The `no` line comes from a run
// on the protected default branch; a `yes` needs a PR lookup on a forge, so that literal is the
// render fixtures' (internal/worktreeaudit twins).
func TestAuditWorktreesOnlyViewNamesTheCleanupToken(t *testing.T) {
	no, code := runAudit(t, auditRepo(t), "--only", "main")
	if code != 1 || !strings.Contains(no, "\ncleanup: no — ") {
		t.Fatalf("--only main: exit %d, want 1 and a line `cleanup: no — <reason>`:\n%s", code, no)
	}
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, flat(body),
				"`--only` and `--issue` are the views for `lighthouse-close-issue`: one branch, or every branch of an issue, each with a line `cleanup: yes` or `cleanup: no — <reason>`. `--only` exits 0 only at `yes`;")
		})
	}
}

// Classification: fails when the table header in the skill drifts from the one the command
// prints.
func TestAuditWorktreesTableHeaderIsTheCommands(t *testing.T) {
	out, code := runAudit(t, auditRepo(t), "--cleanup")
	header, _, _ := strings.Cut(out, "\n")
	if code != 0 || !strings.HasPrefix(header, "| worktree | branch |") {
		t.Fatalf("--cleanup: exit %d, first line %q", code, header)
	}
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			wantAll(t, section(t, body, "## Output format", "## Overlap"), "\n"+header+"\n")
		})
	}
}

// Classification: fails when the KPI fields in the skill drift from the ones the command
// prints, or the command changes its fields.
func TestAuditWorktreesKPIFieldsAreTheCommands(t *testing.T) {
	out, code := runAudit(t, auditRepo(t), "--kpi")
	var fields []string
	for _, m := range regexp.MustCompile(`(\w+)=`).FindAllStringSubmatch(out, -1) {
		fields = append(fields, m[1])
	}
	if code != 0 || strings.Join(fields, " ") != "worktrees safe remote_branches warn" {
		t.Fatalf("--kpi: exit %d, fields %v, want worktrees safe remote_branches warn", code, fields)
	}
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			kpis := section(t, body, "## KPIs", "## Output format")
			for _, f := range fields {
				if !strings.Contains(kpis, "| `"+f+"=` |") {
					t.Errorf("no KPI row with field `%s=`", f)
				}
			}
		})
	}
}

// Classification: fails when the protect list stops coming from `[worktree_audit]`, names a
// key config does not know, or the default branch stops being always protected.
func TestAuditWorktreesProtectListFromConfig(t *testing.T) {
	for _, key := range []string{"worktree_audit.protected_refs", "worktree_audit.protected_paths"} {
		if !config.IsKnownKey(key) {
			t.Errorf("config does not know %s", key)
		}
	}
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			s := section(t, body, "## The protect list", "## Non-negotiables")
			wantAll(t, flat(s),
				"The default branch is always protected; a setting you can forget to fill in is no protection. On top come the refs and paths only this project knows, from `lighthouse.toml`:")
			wantAll(t, s, "\n[worktree_audit]\nprotected_refs = [", "\nprotected_paths = [")
		})
	}
}

// Classification: fails when a paragraph names the opencode lane or the port registry, which
// stay in paia, without "if present".
func TestAuditWorktreesOpencodeOnlyIfPresent(t *testing.T) {
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			hits := 0
			for _, para := range paragraphs(body) {
				p := flat(para.text)
				if !strings.Contains(p, "opencode") && !strings.Contains(p, "port-registry") {
					continue
				}
				hits++
				if !strings.Contains(p, "if present") {
					t.Errorf("names the opencode lane or port registry without \"if present\": %s", p)
				}
			}
			if hits == 0 {
				t.Error("no paragraph names the opencode lane or the port registry")
			}
		})
	}
}

// Classification: fails when the skill names the source repo, its paths, host or service, or a
// source issue number.
func TestAuditWorktreesIsDetachedFromTheSourceRepo(t *testing.T) {
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, body, "CorneeldH", "personal_ai_agent", "deploy/beelink", "agentsview",
				"tests/unit/", "tests/integration/", "claude-remote-control.service")
			if m := regexp.MustCompile(`#\d{3,}`).FindString(body); m != "" {
				t.Errorf("names issue number %s", m)
			}
		})
	}
}

// Classification: fails when a Dutch sentence enters the skill.
func TestAuditWorktreesIsEnglish(t *testing.T) {
	for variant, body := range variants(t, auditWorktrees) {
		t.Run(variant, func(t *testing.T) {
			wantNone(t, strings.ToLower(body), " je ", " het ", " niet ", " geen ", " een ", " wordt ")
		})
	}
}
