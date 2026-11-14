package dispatch

import (
	"context"
	"net/http"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

func review(sha, verdict string) string {
	return "<!-- roborev-pr-comment -->\n## roborev: Review Complete (`" + sha + "`)\n\n" + verdict + "\n\n### 1. high\n- a.go:3: x"
}

func marker(sha string) string { return "<!-- roborev-verwerkt: " + sha + " -->\nverwerkt" }

type rc struct{ author, body string }

func TestRoborevState(t *testing.T) {
	const bot = "github-actions[bot]"
	const fix = "Changes require fixes"
	combined := "<!-- roborev-pr-comment -->\n## roborev: Combined Review (`abc1234def`)\n\n" + fix
	passed := "<!-- roborev-pr-comment -->\n## roborev: Review Passed (`abc1234def`)\n\n" + fix
	cases := []struct {
		name string
		in   []rc
		want string
	}{
		{"no review", []rc{{"owner", "hallo"}}, ""},
		{"review without marker", []rc{{bot, review("abc1234def", fix)}}, forge.RoborevOpen},
		{"github-actions counts too", []rc{{"github-actions", review("abc1234def", fix)}}, forge.RoborevOpen},
		{"review with marker", []rc{{bot, review("abc1234def", fix)}, {"owner", marker("abc1234def")}}, forge.RoborevProcessed},
		{"short marker prefix", []rc{{bot, review("abc1234def", fix)}, {"owner", marker("abc1234")}}, forge.RoborevProcessed},
		{"marker shorter than 7", []rc{{bot, review("abc1234def", fix)}, {"owner", marker("abc123")}}, forge.RoborevOpen},
		{"empty marker", []rc{{bot, review("abc1234def", fix)}, {"owner", "<!-- roborev-verwerkt:  -->"}}, forge.RoborevOpen},
		{"untrusted marker", []rc{{bot, review("abc1234def", fix)}, {"stranger", marker("abc1234def")}}, forge.RoborevOpen},
		{"review by another author", []rc{{"owner", review("abc1234def", fix)}}, ""},
		{"combined review", []rc{{bot, combined}}, forge.RoborevOpen},
		{"review passed", []rc{{bot, passed}}, ""},
		{"other verdict", []rc{{bot, review("abc1234def", "Looks good")}}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := client(t)
			srv.Roles = map[string]string{"owner": "admin", "stranger": "read"}
			var cs []map[string]any
			for i, x := range tc.in {
				cs = append(cs, comment(int64(i+1), x.author, x.body))
			}
			srv.Comments = map[int][]map[string]any{12: cs}
			got, err := c.Comments(context.Background(), 12)
			if err != nil {
				t.Fatal(err)
			}
			state, err := roborevState(context.Background(), got, NewTrust(c, defaultAllowed))
			if err != nil {
				t.Fatal(err)
			}
			if state != tc.want {
				t.Errorf("roborevState = %q, want %q", state, tc.want)
			}
		})
	}
}

func TestRoborevStaleMarkerStaysOpen(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"owner": "admin"}
	srv.Comments = map[int][]map[string]any{12: {
		comment(1, "github-actions[bot]", review("aaa1111", "Changes require fixes")),
		comment(2, "owner", marker("aaa1111")),
		comment(3, "github-actions[bot]", review("bbb2222", "Changes require fixes")),
	}}
	got, _ := c.Comments(context.Background(), 12)
	state, err := roborevState(context.Background(), got, NewTrust(c, defaultAllowed))
	if err != nil {
		t.Fatal(err)
	}
	if state != forge.RoborevOpen {
		t.Errorf("roborevState = %q, want open: the marker is for an older review", state)
	}
}

func TestRoborevStateRoleErrorFails(t *testing.T) {
	srv, c := client(t)
	srv.RoleStatus = http.StatusInternalServerError
	srv.Comments = map[int][]map[string]any{12: {
		comment(1, "github-actions[bot]", review("abc1234def", "Changes require fixes")),
		comment(2, "owner", marker("abc1234def")),
	}}
	got, _ := c.Comments(context.Background(), 12)
	if _, err := roborevState(context.Background(), got, NewTrust(c, defaultAllowed)); err == nil {
		t.Fatal("roborevState with a failing role request gave no error")
	}
}
