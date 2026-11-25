package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const postFailedSession = "11111111-2222-4333-8444-555555555555"

func TestWritePostFailedFieldOrder(t *testing.T) {
	dir := t.TempDir()
	n := PostFailed{At: time.Date(2026, 11, 20, 21, 4, 5, 123e6, time.UTC), Issue: 121, Command: "forge issue comment", Error: "a\nb", Body: "🤖 Handoff\nissue: #121\n"}
	if err := WritePostFailed(dir, postFailedSession, n); err != nil {
		t.Fatal(err)
	}
	path := PostFailedPath(dir, postFailedSession)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"at":"2026-11-20T21:04:05.123Z","issue":121,"command":"forge issue comment","error":"a; b","body":`
	if !strings.HasPrefix(string(data), want) {
		t.Fatalf("note = %s, want prefix %s", data, want)
	}
	got, err := ReadPostFailed(dir, postFailedSession)
	if err != nil || got == nil {
		t.Fatalf("read = %v, %v", got, err)
	}
	if got.Body != n.Body || got.Error != "a; b" || got.Issue != 121 || !got.At.Equal(n.At) {
		t.Fatalf("round trip = %+v", got)
	}
	skipOnWindows(t, "NTFS has no POSIX mode bits")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestWritePostFailedNormalisesTime(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 20, 22, 4, 5, 123456789, time.FixedZone("CET", 3600))
	if err := WritePostFailed(dir, postFailedSession, PostFailed{At: at, Issue: 1, Command: "ask"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(PostFailedPath(dir, postFailedSession))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"at":"2026-11-20T21:04:05.123Z"`) {
		t.Fatalf("note = %s", data)
	}
}

func TestWritePostFailedOverwrites(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 11, 20, 21, 4, 5, 0, time.UTC)
	for _, body := range []string{"first", "second"} {
		if err := WritePostFailed(dir, postFailedSession, PostFailed{At: at, Issue: 1, Command: "ask", Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ReadPostFailed(dir, postFailedSession)
	if err != nil || got == nil || got.Body != "second" {
		t.Fatalf("read = %+v, %v", got, err)
	}
}

func TestReadPostFailedMissing(t *testing.T) {
	got, err := ReadPostFailed(t.TempDir(), postFailedSession)
	if got != nil || err != nil {
		t.Fatalf("read = %v, %v, want nil, nil", got, err)
	}
}

func TestReadPostFailedUnreadable(t *testing.T) {
	for _, content := range []string{
		`{`,
		`{"version":2,"at":"2026-11-20T21:04:05.123Z"}`,
		`{"version":1}`,
		`{"version":1,"at":"0001-01-01T00:00:00Z"}`,
	} {
		dir := t.TempDir()
		path := PostFailedPath(dir, postFailedSession)
		writeFile(t, path, content)
		mtime := time.Date(2026, 11, 19, 8, 30, 0, 0, time.UTC)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		got, err := ReadPostFailed(dir, postFailedSession)
		if err != nil {
			t.Fatalf("%s: err = %v", content, err)
		}
		if got == nil || !got.Unreadable || !got.At.Equal(mtime) || got.Issue != 0 || got.Body != "" {
			t.Fatalf("%s: got %+v, want Unreadable at the file's mtime %v", content, got, mtime)
		}
	}
}

func TestPostFailedPathRefusesSession(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadPostFailed(dir, "../x"); err == nil {
		t.Fatal("read: want error")
	}
	if err := WritePostFailed(dir, "..", PostFailed{}); err == nil {
		t.Fatal("write: want error")
	}
	if _, err := os.Stat(filepath.Join(dir, "runs")); err == nil {
		t.Fatal("refused write created runs dir")
	}
}
