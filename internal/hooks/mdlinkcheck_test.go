package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Classification: this is a hook/gate change; the likely regression surface is which written
// markdown file blocks (exit 2 with the problem lines) and which passes, and that a check that
// could not look says COULD NOT CHECK instead of passing.

const (
	unchecked = "\n\nThis hook checks that markdown links are relative and exist.\n" +
		"Nothing was checked; that is not a green light.\n"
	deadReason     = " — does not exist, resolved against the directory of the file\n"
	absoluteReason = " — an absolute path is dead on the rendered site and as soon as the worktree is gone; use a relative path\n"
	blockHead      = "Linkcheck: markdown link(s) that are not relative-and-existing.\n\n"
)

// gitRepo makes a git repository with notes/exists.md; it returns its resolved root.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := realDir(t, t.TempDir())
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	writeFile(t, filepath.Join(root, "notes", "exists.md"), "# exists\n")
	t.Setenv("LIGHTHOUSE_LINKCHECK_TRACE", "")
	return root
}

func linkPayload(path, cwd string) map[string]any {
	p := map[string]any{"tool_name": "Write", "tool_input": map[string]any{"file_path": path}}
	if cwd != "" {
		p["cwd"] = cwd
	}
	return p
}

func TestLinkCheckVerdicts(t *testing.T) {
	root := gitRepo(t)
	note := filepath.Join(root, "notes", "note.md")
	tests := []struct {
		name, text string
		code       int
		stderr     string
	}{
		{"existing link", "See [x](exists.md) and [u](https://e.org) and [a](#top).\n", 0, ""},
		{"dead link", "See [x](gone.md).\n", 2, blockHead + "  gone.md" + deadReason + "In: " + note + "\n"},
		{"absolute link", "See [x](/abs.md).\n", 2, blockHead + "  /abs.md" + absoluteReason + "In: " + note + "\n"},
		{"dead then absolute", "[y](/abs.md) [x](gone.md)\n", 2,
			blockHead + "  gone.md" + deadReason + "  /abs.md" + absoluteReason + "In: " + note + "\n"},
		{"link in a fence", "```\n[x](gone.md)\n```\n", 0, ""},
		{"target outside the repo", "[x](../../outside.md)\n", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeFile(t, note, tt.text)
			res := call(t, mdLinkCheck, "", linkPayload(note, root))
			if res.Code != tt.code || res.Stderr != tt.stderr || res.Output != nil {
				t.Errorf("code %d stderr %q\nwant %d %q", res.Code, res.Stderr, tt.code, tt.stderr)
			}
		})
	}
}

func TestLinkCheckPasses(t *testing.T) {
	root := gitRepo(t)
	writeFile(t, filepath.Join(root, "notes", "dead.txt"), "[x](gone.md)\n")
	outside := realDir(t, t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(outside))
	writeFile(t, filepath.Join(outside, "note.md"), "[x](gone.md)\n")
	tests := []struct {
		name    string
		payload any
	}{
		{"not markdown", linkPayload(filepath.Join(root, "notes", "dead.txt"), "")},
		{"no file_path", map[string]any{"tool_input": map[string]any{}}},
		{"empty payload", ""},
		{"null payload", "null"},
		{"file does not exist", linkPayload(filepath.Join(root, "notes", "new.md"), "")},
		{"a directory", linkPayload(filepath.Join(root, "dir.md"), "")},
		{"outside git", linkPayload(filepath.Join(outside, "note.md"), "")},
	}
	if err := os.Mkdir(filepath.Join(root, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := call(t, mdLinkCheck, "", tt.payload)
			if res.Code != 0 || res.Stderr != "" || res.Output != nil {
				t.Errorf("%+v, want a silent pass", res)
			}
		})
	}
}

func TestLinkCheckMarkdownSuffix(t *testing.T) {
	root := gitRepo(t)
	note := writeFile(t, filepath.Join(root, "notes", "note.markdown"), "[x](gone.md)\n")
	res := call(t, mdLinkCheck, "", linkPayload(note, ""))
	if res.Code != 2 || res.Stderr != blockHead+"  gone.md"+deadReason+"In: "+note+"\n" {
		t.Errorf("code %d stderr %q", res.Code, res.Stderr)
	}
}

// A relative file_path resolves against cwd from the payload, else the working directory of the
// hook; a symlink is reported by its target.
func TestLinkCheckResolvesThePath(t *testing.T) {
	root := gitRepo(t)
	note := writeFile(t, filepath.Join(root, "notes", "note.md"), "[x](gone.md)\n")
	want := blockHead + "  gone.md" + deadReason + "In: " + note + "\n"

	t.Run("cwd in payload", func(t *testing.T) {
		res := call(t, mdLinkCheck, "", linkPayload("notes/note.md", root))
		if res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q", res.Code, res.Stderr)
		}
	})
	t.Run("working directory", func(t *testing.T) {
		t.Chdir(filepath.Join(root, "notes"))
		res := call(t, mdLinkCheck, "", linkPayload("note.md", ""))
		if res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q", res.Code, res.Stderr)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privileges on windows")
		}
		link := filepath.Join(realDir(t, t.TempDir()), "link.md")
		if err := os.Symlink(note, link); err != nil {
			t.Fatal(err)
		}
		res := call(t, mdLinkCheck, "", linkPayload(link, ""))
		if res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q", res.Code, res.Stderr)
		}
	})
}

func TestLinkCheckTrace(t *testing.T) {
	root := gitRepo(t)
	t.Setenv("LIGHTHOUSE_LINKCHECK_TRACE", "1")
	note := writeFile(t, filepath.Join(root, "notes", "note.md"), "[x](exists.md)\n")
	res := call(t, mdLinkCheck, "", linkPayload(note, ""))
	if res.Code != 0 || res.Stderr != "linkcheck: checking in "+root+"\n" {
		t.Errorf("code %d stderr %q", res.Code, res.Stderr)
	}
	writeFile(t, note, "[x](gone.md)\n")
	res = call(t, mdLinkCheck, "", linkPayload(note, ""))
	want := "linkcheck: checking in " + root + "\n" + blockHead + "  gone.md" + deadReason + "In: " + note + "\n"
	if res.Code != 2 || res.Stderr != want {
		t.Errorf("code %d stderr %q", res.Code, res.Stderr)
	}
}

// fakeGit puts a git on PATH that fails rev-parse with "fatal: broken".
func fakeGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake git is a sh script")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\necho 'fatal: broken' >&2\nexit 128\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

func TestLinkCheckCouldNotCheck(t *testing.T) {
	root := gitRepo(t)
	note := writeFile(t, filepath.Join(root, "notes", "note.md"), "[x](exists.md)\n")
	notesDir := filepath.Join(root, "notes")

	t.Run("no git", func(t *testing.T) {
		emptyPath(t)
		res := call(t, mdLinkCheck, "", linkPayload(note, ""))
		if want := "Linkcheck: COULD NOT CHECK — missing tooling: git" + unchecked; res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q", res.Code, res.Stderr)
		}
	})
	t.Run("payload not JSON", func(t *testing.T) {
		for _, raw := range []string{"{nope", `{"tool_input":"x"}`, `[1]`} {
			res := call(t, mdLinkCheck, "", raw)
			if want := "Linkcheck: COULD NOT CHECK — the payload is not valid JSON." + unchecked; res.Code != 2 || res.Stderr != want {
				t.Errorf("%s: code %d stderr %q", raw, res.Code, res.Stderr)
			}
		}
	})
	t.Run("git fails", func(t *testing.T) {
		fakeGit(t)
		res := call(t, mdLinkCheck, "", linkPayload(note, ""))
		want := "Linkcheck: COULD NOT CHECK — git could not read '" + notesDir + "'." + unchecked + "\nfatal: broken\n"
		if res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q\nwant %q", res.Code, res.Stderr, want)
		}
	})
	t.Run("unreadable file", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a mode 000 file that the user cannot read")
		}
		if err := os.Chmod(note, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(note, 0o644) })
		res := call(t, mdLinkCheck, "", linkPayload(note, ""))
		want := "Linkcheck: COULD NOT CHECK — the link parser failed." + unchecked + "\nopen " + note + ": permission denied\n"
		if res.Code != 2 || res.Stderr != want {
			t.Errorf("code %d stderr %q\nwant %q", res.Code, res.Stderr, want)
		}
	})
}

// Python crashed on invalid UTF-8; the Go parser reads on and still finds the dead link.
func TestLinkCheckInvalidUTF8ReadsOn(t *testing.T) {
	root := gitRepo(t)
	note := writeFile(t, filepath.Join(root, "notes", "note.md"), "\xff\xfe [x](gone.md)\n")
	res := call(t, mdLinkCheck, "", linkPayload(note, ""))
	if want := blockHead + "  gone.md" + deadReason + "In: " + note + "\n"; res.Code != 2 || res.Stderr != want {
		t.Errorf("code %d stderr %q", res.Code, res.Stderr)
	}
}
