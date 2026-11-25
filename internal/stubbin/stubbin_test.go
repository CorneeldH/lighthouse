package stubbin

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMakeRunsAndPrintsStdout(t *testing.T) {
	dir := t.TempDir()
	Make(t, dir, "fakebin", Spec{Stdout: "hi", Exit: 3})
	Prepend(t, dir)
	if _, err := exec.LookPath("fakebin"); err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	out, err := exec.Command("fakebin").Output()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("want exit 3, got %v", err)
	}
	if string(out) != "hi" {
		t.Fatalf("stdout = %q", out)
	}
}

func TestMakeLogsArguments(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	Make(t, dir, "fakelog", Spec{LogFile: log})
	Prepend(t, dir)
	for _, args := range [][]string{{"a", "b"}, {"c"}} {
		if err := exec.Command("fakelog", args...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	got := ReadLog(t, log)
	if want := []string{"a b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestMakeEscapesSpecials(t *testing.T) {
	dir := t.TempDir()
	want := "100% & <ok> | ^x 'q' \"d\""
	Make(t, dir, "fakespecial", Spec{Stdout: want})
	Prepend(t, dir)
	out, err := exec.Command("fakespecial").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestOnlyUsesSeparatorAndKeepsTools(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	Only(t, dir, "git")
	p := os.Getenv("PATH")
	if !strings.Contains(p, string(os.PathListSeparator)) {
		t.Fatalf("PATH %q has no separator", p)
	}
	if !strings.HasPrefix(p, dir) {
		t.Fatalf("PATH %q does not start with %q", p, dir)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git not found: %v", err)
	}
}

func TestOnlyFailsWithoutTool(t *testing.T) {
	if _, err := lookDirs([]string{"no-such-tool-xyz"}); err == nil {
		t.Fatal("want an error for a missing tool")
	}
}

func TestWindowsCmdText(t *testing.T) {
	got := cmdText(Spec{Stdout: "100% & <ok>", Stderr: "err", Exit: 7, LogFile: `C:\l\calls.log`}, `C:\d\tool`)
	for _, want := range []string{
		`@echo off`,
		`type "C:\d\tool.stdout"`,
		`type "C:\d\tool.stderr" 1>&2`,
		`>>"C:\l\calls.log" echo(%*`,
		`exit /b 7`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("cmd text lacks %q:\n%s", want, got)
		}
	}
	for _, bad := range []string{"100%", "<ok>"} {
		if strings.Contains(got, bad) {
			t.Errorf("cmd text echoes user output %q:\n%s", bad, got)
		}
	}
	// The redirect comes first: `echo(%*>>"log"` would read a trailing digit argument as a handle.
	if strings.Contains(got, `%*>>`) {
		t.Errorf("redirect must precede echo:\n%s", got)
	}
	plain := cmdText(Spec{}, `C:\d\tool`)
	if strings.Contains(plain, ".stderr") || strings.Contains(plain, "echo(") {
		t.Errorf("empty spec must not reference stderr or log:\n%s", plain)
	}
}

func TestPathValueSeparator(t *testing.T) {
	if got := joinPath(";", "a", "b", "a"); got != "a;b" {
		t.Fatalf("joinPath = %q", got)
	}
	if got := joinPath(":", "x", "y"); got != "x:y" {
		t.Fatalf("joinPath = %q", got)
	}
	t.Setenv("PATH", "/orig")
	if got, want := PathValue(t, "/d"), "/d"+string(os.PathListSeparator)+"/orig"; got != want {
		t.Fatalf("PathValue = %q, want %q", got, want)
	}
}

func TestReadLogNormalizesCRLF(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l")
	if err := os.WriteFile(p, []byte("a b\r\nc\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := ReadLog(t, p), []string{"a b", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestMakeCreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "bin")
	Make(t, dir, "fakebin", Spec{})
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("dir not created: %v", err)
	}
}

func TestMakeScriptCreatesMissingDir(t *testing.T) {
	RequirePOSIX(t, "script")
	dir := filepath.Join(t.TempDir(), "a", "bin")
	MakeScript(t, dir, "fakebin", "exit 0\n")
	if _, err := os.Stat(filepath.Join(dir, "fakebin")); err != nil {
		t.Fatal(err)
	}
}
