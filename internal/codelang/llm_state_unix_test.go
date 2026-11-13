//go:build unix

package codelang

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// markers lists the marker files in dir.
func markers(t *testing.T, dir string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(dir, "layer2-failed-*"))
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// writeStamp writes a marker with a Unix time age seconds ago.
func writeStamp(t *testing.T, path string, age float64) {
	t.Helper()
	stamp := strconv.FormatFloat(float64(time.Now().UnixNano())/1e9-age, 'f', -1, 64)
	if err := os.WriteFile(path, []byte(stamp), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestQuietPeriod(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	chain := testChain(fake.url)
	if reply := ask(chain, state); reply.NotEnglish != nil {
		t.Fatalf("first: %+v, want a failed chain", reply)
	}
	seen := len(fake.seen())
	again := ask(chain, state)
	if again.NotEnglish != nil || len(again.Failures) != 1 || !strings.HasPrefix(again.Failures[0], "skipped: ") {
		t.Fatalf("again: %+v, want one skipped failure", again)
	}
	if !regexp.MustCompile(`^skipped: the chain failed \d+ s ago$`).MatchString(again.Failures[0]) {
		t.Errorf("again: %q, want the age in whole seconds", again.Failures[0])
	}
	if n := len(fake.seen()); n != seen {
		t.Errorf("%d requests after the skip, want %d", n, seen)
	}
	other := newFakeLLM(t, nil, "answer") // another url is not skipped
	if reply := ask(testChain(other.url), state); !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Errorf("other url: %+v, want [true]", reply)
	}
}

func TestQuietAgeIsRounded(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	fake := newFakeLLM(t, nil, "answer")
	// 30.0 s rounds to 30 until the call is half a second late.
	writeStamp(t, failMarker(state, fake.url), 30)
	reply := ask(testChain(fake.url), state)
	if !reflect.DeepEqual(reply.Failures, []string{"skipped: the chain failed 30 s ago"}) {
		t.Errorf("reply %+v, want skipped 30 s ago", reply)
	}
}

func TestQuietLastsAlmostAMinute(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	fake := newFakeLLM(t, nil, "answer")
	// 58 s old: still quiet, with a second of slack before QUIET_S.
	writeStamp(t, failMarker(state, fake.url), 58)
	reply := ask(testChain(fake.url), state)
	if reply.NotEnglish != nil || len(reply.Failures) != 1 || !strings.HasPrefix(reply.Failures[0], "skipped: ") {
		t.Errorf("reply %+v, want skipped", reply)
	}
	if n := len(fake.seen()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

func TestMarkerOldIsNotQuiet(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	chain := testChain(fake.url)
	ask(chain, state)
	found := markers(t, state)
	if len(found) != 1 {
		t.Fatalf("markers %v, want one", found)
	}
	writeStamp(t, found[0], 65)
	fake.forget()
	ask(chain, state)
	if n := len(fake.seen()); n != 1 {
		t.Errorf("%d requests, want 1", n)
	}
}

func TestMarkerFromTheFutureIsNotQuiet(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	fake := newFakeLLM(t, nil, "answer")
	writeStamp(t, failMarker(state, fake.url), -30)
	if reply := ask(testChain(fake.url), state); !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Errorf("reply %+v, want [true]", reply)
	}
}

func TestMarkerRemovedAfterSuccess(t *testing.T) {
	withKey(t)
	state := t.TempDir()
	failing := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	ask(testChain(failing.url), state)
	found := markers(t, state)
	if len(found) != 1 {
		t.Fatalf("markers %v, want one", found)
	}
	writeStamp(t, found[0], 65)
	fake := newFakeLLM(t, nil, "answer")
	if err := os.Rename(found[0], failMarker(state, fake.url)); err != nil { // the marker of this url
		t.Fatal(err)
	}
	if reply := ask(testChain(fake.url), state); !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Fatalf("reply %+v, want [true]", reply)
	}
	if found := markers(t, state); len(found) != 0 {
		t.Errorf("markers %v, want none", found)
	}
}

func TestMarkerNameIsSha1OfURL(t *testing.T) {
	// sha1("http://h/v1")[:12], the name the Python hook used, so both read one marker.
	if got, want := failMarker("/s", "http://h/v1"), "/s/layer2-failed-e827ef67c972"; got != want {
		t.Errorf("failMarker = %q, want %q", got, want)
	}
}

func TestMarkerUnusableIsIgnored(t *testing.T) {
	withKey(t)
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	writeFile(t, blocker, []byte("x"))
	good := newFakeLLM(t, nil, "answer")
	if reply := ask(testChain(good.url), filepath.Join(blocker, "sub")); !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Fatalf("reply %+v, want [true]", reply)
	}
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	reply := ask(testChain(fake.url), filepath.Join(blocker, "sub"))
	if !reflect.DeepEqual(reply.Failures, []string{"m1: HTTP 500"}) {
		t.Errorf("under a file: %+v, want m1: HTTP 500", reply)
	}
	writeFile(t, failMarker(tmp, fake.url), []byte("x"))
	reply = ask(testChain(fake.url), tmp)
	if !reflect.DeepEqual(reply.Failures, []string{"m1: HTTP 500"}) {
		t.Errorf("unreadable marker: %+v, want m1: HTTP 500", reply)
	}
	if n := len(fake.seen()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

func TestStateDirNewIsPrivate(t *testing.T) {
	withKey(t)
	state := filepath.Join(t.TempDir(), "state")
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	ask(testChain(fake.url), state)
	info, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o700 {
		t.Errorf("state dir mode %o, want 700", mode)
	}
	found := markers(t, state)
	if len(found) != 1 {
		t.Fatalf("markers %v, want one", found)
	}
	info, err = os.Stat(found[0])
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("marker mode %o, want 600", mode)
	}
	raw, err := os.ReadFile(found[0])
	if err != nil {
		t.Fatal(err)
	}
	stamp, err := strconv.ParseFloat(string(raw), 64)
	if now := float64(time.Now().Unix()); err != nil || stamp < now-5 || stamp > now+5 {
		t.Errorf("marker holds %q, want the Unix time now", raw)
	}
}

func TestMarkerSymlinkIsNotFollowed(t *testing.T) {
	withKey(t)
	tmp := t.TempDir()
	state := filepath.Join(tmp, "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(tmp, "victim")
	writeFile(t, victim, []byte("precious"))
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	if err := os.Symlink(victim, failMarker(state, fake.url)); err != nil {
		t.Fatal(err)
	}
	reply := ask(testChain(fake.url), state)
	if !reflect.DeepEqual(reply.Failures, []string{"m1: HTTP 500"}) {
		t.Errorf("reply %+v, want m1: HTTP 500", reply)
	}
	if raw, _ := os.ReadFile(victim); string(raw) != "precious" {
		t.Errorf("victim holds %q, want precious", raw)
	}
}

func TestStateDirSharedIsNotUsed(t *testing.T) {
	withKey(t)
	state := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(state, 0o777); err != nil {
		t.Fatal(err)
	}
	fake := newFakeLLM(t, map[string]string{"m1": "http500"}, "answer")
	ask(testChain(fake.url), state)
	if entries, _ := os.ReadDir(state); len(entries) != 0 {
		t.Errorf("state dir holds %v, want nothing", entries)
	}
	writeStamp(t, failMarker(state, fake.url), 0)
	again := ask(testChain(fake.url), state)
	if !reflect.DeepEqual(again.Failures, []string{"m1: HTTP 500"}) {
		t.Errorf("again %+v, want m1: HTTP 500", again)
	}
	if n := len(fake.seen()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}
