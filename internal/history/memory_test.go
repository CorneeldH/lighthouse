package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkMemory(t *testing.T, proj string, n int) string {
	t.Helper()
	dir := filepath.Join(proj, "memory")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("note-%d.md", i)), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeIndex(t *testing.T, dir string, lines int) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(strings.Repeat("- entry\n", lines)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func caseInsensitiveFS(t *testing.T, root string) bool {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := os.Mkdir(filepath.Join(root, "PROBE"), 0o755)
	return err != nil
}

func TestMemory_CaseVariantDirsAndFiles(t *testing.T) {
	root := t.TempDir()
	if caseInsensitiveFS(t, root) {
		t.Skip("case-insensitive filesystem")
	}
	a := filepath.Join(root, "d--work-r")
	b := filepath.Join(root, "D--work-r")
	mkMemory(t, a, 100)
	mkMemory(t, b, 113)
	d := newMemory(Env{ProjectDirs: []string{a, b}})
	f := d.Finish(Env{ProjectDirs: []string{a, b}})
	if f.Key != "memory" || f.Status != "found" || f.Measured["files"] != 213 || f.Measured["case_variant_dirs"] != 1 {
		t.Fatalf("%+v", f)
	}
	if f.Proposal.Kind != "advice" {
		t.Fatalf("%+v", f.Proposal)
	}
}

func TestMemory_IndexLimit(t *testing.T) {
	for lines, want := range map[int]bool{200: true, 199: false, 250: true} {
		proj := t.TempDir()
		writeIndex(t, mkMemory(t, proj, 1), lines)
		env := Env{ProjectDirs: []string{proj}}
		f := newMemory(env).Finish(env)
		if f.Measured["index_at_limit"] != want || f.Measured["index_lines"] != lines {
			t.Errorf("%d lines: %+v", lines, f.Measured)
		}
	}
}

func TestMemory_NoMemoryDir(t *testing.T) {
	env := Env{ProjectDirs: []string{t.TempDir()}}
	f := newMemory(env).Finish(env)
	if f.Status != "none" || f.Measured["files"] != 0 || f.Proposal.Kind != "advice" {
		t.Fatalf("%+v", f)
	}
	f = newMemory(Env{}).Finish(Env{})
	if f.Status != "none" {
		t.Fatalf("%+v", f)
	}
}

func TestMemory_CaseVariantCwds(t *testing.T) {
	d := newMemory(Env{})
	for _, c := range []string{`D:\x`, `d:\x`, `d:\x`, `D:\other`, "/home/u/r"} {
		d.Add(Record{File: "f.jsonl", Line: 1, Type: "user", Cwd: c})
	}
	f := d.Finish(Env{})
	if f.Measured["case_variant_cwds"] != 1 || f.Status != "found" {
		t.Fatalf("%+v", f)
	}
	d = newMemory(Env{})
	d.Add(Record{Cwd: `D:\x`})
	d.Add(Record{Cwd: `D:\y`})
	d.Add(Record{Cwd: `D:\X`}) // differs in the path, not the drive letter
	if f := d.Finish(Env{}); f.Measured["case_variant_cwds"] != 0 {
		t.Fatalf("%+v", f)
	}
}

func TestMemory_NeverContent(t *testing.T) {
	proj := t.TempDir()
	dir := mkMemory(t, proj, 1)
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("SECRET-MEMORY-abc\nline\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("SECRET-MEMORY-def"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := Env{ProjectDirs: []string{proj}}
	f := newMemory(env).Finish(env)
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "SECRET-MEMORY") {
		t.Fatalf("content leaked: %s", b)
	}
	if f.Measured["index_lines"] != 2 {
		t.Fatalf("%+v", f.Measured)
	}
}
