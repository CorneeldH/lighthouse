package checkinstr

import (
	"path/filepath"
	"strings"
	"testing"
)

// Red when the space between `run.sh` and the hook name is matched as RE2's ASCII `\s` instead
// of Python's: then the hook reads as `run`, which ends on neither -gate nor -check.
func TestHookNamesRunArgumentAfterPythonWhitespace(t *testing.T) {
	// Each space as a JSON escape.
	for name, space := range map[string]string{"vertical tab": `\u000b`, "no-break space": `\u00a0`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".claude", "settings.json"),
				`{"hooks": {"PreToolUse": [{"hooks": [{"type": "command", "command": `+
					`".claude/hooks/lighthouse/run.sh`+space+`main-commit-gate"}]}]}}`)
			_, out := runRepo(t, root)
			if !strings.Contains(out, "ok    every lighthouse hook is named after what it does\n") {
				t.Errorf("want check 14 ok, got:\n%s", out)
			}
		})
	}
}

// Red when a `hooks` value that is present but not an object (null included) passes, or when
// check 8 treats a null `hooks` as a broken settings.json instead of "not registered".
func TestHooksNullIsNotAnObject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), `{"hooks": null}`)
	writeFile(t, filepath.Join(root, ".claude", "hooks", "strict.sh"), "")
	skill := "---\nname: strict\nmetadata:\n  binding: hard\n---\nSee `.claude/hooks/strict.sh`.\n"
	writeFile(t, filepath.Join(root, ".claude", "skills", "strict", "SKILL.md"), skill)
	writeFile(t, filepath.Join(root, ".agents", "skills", "strict", "SKILL.md"), skill)

	code, out := runRepo(t, root)
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	for _, want := range []string{
		"FAIL  pairing: 'strict' carries 'binding: hard' but names no counterpart that exists and is registered in settings.json\n",
		"FAIL  hook-names: '" + root + "/.claude/settings.json' is not valid JSON (or not an object)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
