package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandlerServesAFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loop.html"), []byte("<p>loop</p>"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler(dir).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/loop.html", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "<p>loop</p>" {
		t.Fatalf("body = %q", got)
	}
}

func TestHandlerMissingFileIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(t.TempDir()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope.html", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func neverServe(t *testing.T) func(string, http.Handler) error {
	return func(string, http.Handler) error {
		t.Error("serve must not be called")
		return nil
	}
}

func TestRunWithoutDirExits2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr, neverServe(t)); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRunWithMissingDirExits2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	missing := filepath.Join(t.TempDir(), "absent")
	if code := run([]string{missing}, &stdout, &stderr, neverServe(t)); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty")
	}
}

func TestRunPrintsTheURL(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	var gotAddr string
	serve := func(addr string, _ http.Handler) error { gotAddr = addr; return nil }
	if code := run([]string{dir}, &stdout, &stderr, serve); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	want := "serving " + dir + " at http://localhost:8000/\n"
	if stdout.String() != want {
		t.Fatalf("stdout = %q, want %q", stdout.String(), want)
	}
	if gotAddr != "localhost:8000" {
		t.Fatalf("addr = %q", gotAddr)
	}
}
