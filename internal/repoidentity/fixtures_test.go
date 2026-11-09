package repoidentity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/fixturesetup"
)

type fixtureCase struct {
	Name    string           `json:"name"`
	Kind    string           `json:"kind"`
	Setup   []map[string]any `json:"setup"`
	URL     string           `json:"url"`
	Project string           `json:"project"`
	Root    string           `json:"root"`
	Expect  json.RawMessage  `json:"expect"`
}

func TestFixtures(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/repo_identity/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []fixtureCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			switch c.Kind {
			case "remote_key":
				var want *string
				if err := json.Unmarshal(c.Expect, &want); err != nil {
					t.Fatal(err)
				}
				got, ok := RemoteKey(c.URL)
				if want == nil {
					if ok {
						t.Fatalf("RemoteKey(%q) = %q, want none", c.URL, got)
					}
					return
				}
				if !ok || got != *want {
					t.Fatalf("RemoteKey(%q) = %q, %v; want %q", c.URL, got, ok, *want)
				}
			case "same_origin":
				root := t.TempDir()
				fixturesetup.Build(t, root, c.Setup)
				same, reason, err := SameOrigin(context.Background(),
					fixturesetup.Fill(t, c.Project, root), fixturesetup.Fill(t, c.Root, root))
				var word string
				if json.Unmarshal(c.Expect, &word) == nil && word == "raises" {
					var oe *OriginError
					if !errors.As(err, &oe) {
						t.Fatalf("err = %v, want *OriginError", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var want struct {
					same   bool
					reason string
				}
				var pair []any
				if err := json.Unmarshal(c.Expect, &pair); err != nil || len(pair) != 2 {
					t.Fatalf("bad expect %s", c.Expect)
				}
				want.same, _ = pair[0].(bool)
				want.reason, _ = pair[1].(string)
				if same != want.same || reason != want.reason {
					t.Fatalf("SameOrigin = %v, %q; want %v, %q", same, reason, want.same, want.reason)
				}
			default:
				t.Fatalf("unknown kind %q", c.Kind)
			}
		})
	}
}

func TestOriginUnsetAndSet(t *testing.T) {
	root := t.TempDir()
	fixturesetup.Build(t, root, []map[string]any{{"repo": "r", "branch": "main"}})
	dir := root + "/r"
	if url, set, err := Origin(context.Background(), dir); err != nil || set || url != "" {
		t.Fatalf("Origin = %q, %v, %v; want unset", url, set, err)
	}
	fixturesetup.Git(t, dir, "remote", "add", "origin", "git@h:o/r.git")
	if url, set, err := Origin(context.Background(), dir); err != nil || !set || url != "git@h:o/r.git" {
		t.Fatalf("Origin = %q, %v, %v", url, set, err)
	}
}

func TestOriginNotARepoIsAnError(t *testing.T) {
	_, _, err := Origin(context.Background(), t.TempDir()+"/missing")
	var oe *OriginError
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want *OriginError", err)
	}
}

func TestOriginTimeoutText(t *testing.T) {
	// A git that outlives the deadline is killed by the context: that must read as a timeout,
	// not as "git failed (rc -1)".
	bin := t.TempDir()
	if err := os.WriteFile(bin+"/git", []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _, err := Origin(ctx, "/some/root")
	var oe *OriginError
	if !errors.As(err, &oe) || oe.Error() != "git timeout reading the origin of /some/root" {
		t.Fatalf("err = %v, want the timeout text", err)
	}
}
