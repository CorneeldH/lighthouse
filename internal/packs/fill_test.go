package packs

import (
	"errors"
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) Lookup {
	return func(key string) (string, bool, error) {
		v, ok := m[key]
		return v, ok, nil
	}
}

func TestFill(t *testing.T) {
	in := []byte("runs-on: @@ci.runs_on@@\nid: ${{ secrets.X }}\n")
	out, missing, err := Fill(in, lookupFrom(map[string]string{"ci.runs_on": "ubuntu-latest"}))
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing %v err %v", missing, err)
	}
	if string(out) != "runs-on: ubuntu-latest\nid: ${{ secrets.X }}\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestFillMissingKey(t *testing.T) {
	in := []byte("a: @@ci.runs_on@@\nb: @@ci.bot_login@@\nc: @@ci.bot_login@@\n")
	out, missing, err := Fill(in, lookupFrom(map[string]string{"ci.runs_on": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "ci.bot_login" {
		t.Fatalf("missing = %v", missing)
	}
	if string(out) != string(in) {
		t.Fatalf("out changed: %q", out)
	}
}

func TestFillLookupError(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := Fill([]byte("@@ci.runs_on@@"), func(string) (string, bool, error) { return "", false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestFillWithoutPlaceholders(t *testing.T) {
	called := false
	out, missing, err := Fill([]byte("on: push\n"), func(string) (string, bool, error) { called = true; return "", false, nil })
	if err != nil || len(missing) != 0 || string(out) != "on: push\n" || called {
		t.Fatalf("out %q missing %v err %v called %v", out, missing, err, called)
	}
}
