package packcontract

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// expo pack: the skill refresh-expo-lockfiles finds the apps itself, guards what a project may
// not have, and runs its commands in a subshell. Name, links and English are covered generically.

const expoSkill = "expo/skills/refresh-expo-lockfiles/SKILL.md"

var bashBlockRe = regexp.MustCompile("(?s)```bash\n(.*?)```")

func expoBashBlocks(t *testing.T) []string {
	t.Helper()
	var blocks []string
	for _, m := range bashBlockRe.FindAllStringSubmatch(readPack(t, expoSkill), -1) {
		blocks = append(blocks, m[1])
	}
	return blocks
}

// Classification: fails when a reference a project may not have (dependabot.yml, expo-upgrade,
// the workflows) is named in a paragraph without "if present", so the skill would send the
// reader to a file that is not there.
func TestExpoOptionalReferencesAreGuarded(t *testing.T) {
	text := readPack(t, expoSkill)
	for _, ref := range []string{"dependabot.yml", "expo-upgrade", ".github/workflows"} {
		if !strings.Contains(text, ref) {
			t.Errorf("no %s in the skill", ref)
		}
		for _, p := range textParagraphs(text) {
			if strings.Contains(p, ref) && !strings.Contains(p, "if present") {
				t.Errorf("%s without 'if present':\n%s", ref, p)
			}
		}
	}
}

// Classification: fails when the issue link reads the area label prefix from a fixed value
// instead of `lighthouse config labels.area_prefix`.
func TestExpoWriteIssueLinkReadsThePrefix(t *testing.T) {
	text := readPack(t, expoSkill)
	wantAll(t, text, "/lighthouse-write-issue", "lighthouse config labels.area_prefix")
	wantNone(t, text, "2-area:")
}

// Classification: fails when the skill stops checking that both node and npm exist before it
// starts, so a missing npm surfaces halfway through.
func TestExpoChecksNodeAndNpm(t *testing.T) {
	wantAll(t, readPack(t, expoSkill), "command -v node", "command -v npm")
}

// Classification: fails when the diff check comes after the commit step, where it would see an
// empty diff.
func TestExpoDiffIsCheckedBeforeTheCommit(t *testing.T) {
	text := readPack(t, expoSkill)
	check := strings.Index(text, "Check that the diff is what you think")
	loc := regexp.MustCompile(`(?m)^### \d+\. Commit$`).FindStringIndex(text)
	if check < 0 || loc == nil {
		t.Fatal("diff check or commit step missing")
	}
	if check > loc[0] {
		t.Error("the diff check runs after the commit, so it sees an empty diff")
	}
}

// Classification: fails when a bash block does a bare `cd`, which would move the next app's
// command into the previous app's directory.
func TestExpoCdStaysInASubshell(t *testing.T) {
	for _, block := range expoBashBlocks(t) {
		for line := range strings.SplitSeq(block, "\n") {
			if strings.HasPrefix(strings.TrimLeft(line, " \t"), "cd ") {
				t.Errorf("cd outside a subshell: %s", line)
			}
		}
	}
}

// Classification: runs the app loop of the skill in a repo of six tracked files and fails when
// it lists a directory that names expo but has no package-lock.json (a library, a workspace
// member), or misses the one app, since `npm ci` needs that lockfile.
func TestExpoAppLoopFindsOnlyAppsWithALockfile(t *testing.T) {
	bash := needTool(t, "bash")
	git := needTool(t, "git")
	var loop string
	for _, b := range expoBashBlocks(t) {
		if strings.Contains(b, "git ls-files") {
			loop = b
		}
	}
	if loop == "" {
		t.Fatal("no bash block with git ls-files")
	}
	dir := t.TempDir()
	files := map[string]string{
		"package.json":             `{"keywords": ["expo"]}` + "\n",
		"apps/a/package.json":      `{"dependencies": {"expo": "~1.0.0"}}` + "\n",
		"apps/a/package-lock.json": "{}\n",
		"lib/b/package.json":       `{"peerDependencies": {"expo": "*"}}` + "\n",
		"web/package.json":         `{"dependencies": {"react": "1"}}` + "\n",
		"web/package-lock.json":    "{}\n",
	}
	for rel, content := range files {
		writeFile(t, filepath.Join(dir, rel), content)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}} {
		cmd := exec.Command(git, append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	cmd := exec.Command(bash, "-c", loop)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(out)); len(got) != 1 || got[0] != "apps/a" {
		t.Errorf("apps = %v, want [apps/a]", got)
	}
}
