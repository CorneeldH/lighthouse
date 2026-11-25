package created

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMergeOnlyGrows(t *testing.T) {
	c := Created{Files: []string{"a", "b"}, SettingsKeys: []string{"x"}, SettingsLanguage: "dutch"}
	got := c.Merge(Created{Files: []string{"b", "c"}, Gitattributes: []string{"g"}})
	want := Created{Files: []string{"a", "b", "c"}, Gitattributes: []string{"g"}, SettingsKeys: []string{"x"}, SettingsLanguage: "dutch"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge = %+v, want %+v", got, want)
	}
	if got := c.Merge(Created{}); !reflect.DeepEqual(got, c) {
		t.Errorf("empty add changed the value: %+v", got)
	}
	if got := c.Merge(Created{SettingsLanguage: "english"}); got.SettingsLanguage != "english" {
		t.Errorf("language = %q, want add's value", got.SettingsLanguage)
	}
	if len(c.Files) != 2 {
		t.Errorf("receiver mutated: %v", c.Files)
	}
}

func TestEncodeReadRoundTrip(t *testing.T) {
	c := Created{
		Files:            []string{"lighthouse.toml", "AGENTS.md"},
		Gitattributes:    []string{"*.sh eol=lf"},
		SettingsLanguage: "dutch",
	}
	data := Encode(c)
	s := string(data)
	if !strings.HasPrefix(s, "# Managed by lighthouse init. Do not edit by hand.\n") {
		t.Errorf("missing header:\n%s", s)
	}
	if strings.Contains(s, "settings_keys") {
		t.Errorf("empty key written:\n%s", s)
	}
	root := t.TempDir()
	p := filepath.Join(root, ".lighthouse", "created.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got, found, err := Read(root)
	if err != nil || !found {
		t.Fatalf("Read: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(got, c) {
		t.Errorf("round trip = %+v, want %+v", got, c)
	}
	if Encode(Created{}) == nil || strings.Contains(string(Encode(Created{})), "=") {
		t.Errorf("empty encode should be header only: %q", Encode(Created{}))
	}
}

func TestReadMissing(t *testing.T) {
	c, found, err := Read(t.TempDir())
	if found || err != nil || !c.Empty() {
		t.Errorf("got %+v found=%v err=%v", c, found, err)
	}
}

func TestReadCorrupt(t *testing.T) {
	for name, body := range map[string]string{"not toml": "this is = = not toml", "unknown key": "bogus = [\"a\"]\n"} {
		root := t.TempDir()
		p := filepath.Join(root, ".lighthouse", "created.toml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, err := Read(root)
		if err == nil || !strings.Contains(err.Error(), ".lighthouse/created.toml") {
			t.Errorf("%s: err = %v, want one naming the path", name, err)
		}
	}
}

func TestEmpty(t *testing.T) {
	if !(Created{}).Empty() {
		t.Error("zero value should be empty")
	}
	for _, c := range []Created{{Files: []string{"a"}}, {Gitattributes: []string{"a"}}, {SettingsKeys: []string{"a"}}, {SettingsLanguage: "a"}} {
		if c.Empty() {
			t.Errorf("%+v should not be empty", c)
		}
	}
}
