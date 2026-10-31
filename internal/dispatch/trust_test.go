package dispatch

import (
	"context"
	"net/http"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

// client returns a forgetest server and a client for o/r pointed at it.
func client(t *testing.T) (*forgetest.Server, *forge.Client) {
	t.Helper()
	srv := forgetest.New(t)
	c := forge.New("o/r", "secret-token")
	c.BaseURL = srv.URL()
	return srv, c
}

var defaultAllowed = []string{"admin", "maintain"}

func TestTrustedRoles(t *testing.T) {
	cases := []struct {
		name       string
		roles      map[string]string
		roleStatus int
		want       bool
		wantErr    bool
	}{
		{name: "admin", roles: map[string]string{"ann": "admin"}, want: true},
		{name: "maintain", roles: map[string]string{"ann": "maintain"}, want: true},
		{name: "write", roles: map[string]string{"ann": "write"}, want: false},
		{name: "triage", roles: map[string]string{"ann": "triage"}, want: false},
		{name: "read", roles: map[string]string{"ann": "read"}, want: false},
		{name: "unknown login answers 404", roles: map[string]string{}, want: false},
		{name: "5xx is an error", roles: map[string]string{"ann": "admin"}, roleStatus: http.StatusBadGateway, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := client(t)
			srv.Roles = tc.roles
			srv.RoleStatus = tc.roleStatus
			got, err := NewTrust(c, defaultAllowed).Trusted(context.Background(), "ann")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Trusted(ann) = %v, nil; want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Trusted(ann) = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTrustCachesPerLogin(t *testing.T) {
	srv, c := client(t)
	srv.Roles = map[string]string{"ann": "admin", "bob": "read"}
	trust := NewTrust(c, defaultAllowed)
	for _, login := range []string{"ann", "bob", "ann", "bob"} {
		if _, err := trust.Trusted(context.Background(), login); err != nil {
			t.Fatal(err)
		}
	}
	got, err := trust.Trusted(context.Background(), "ann")
	if err != nil || !got {
		t.Fatalf("third Trusted(ann) = %v, %v; want true from the cache", got, err)
	}
	for _, login := range []string{"ann", "bob"} {
		if n := srv.Count("/repos/o/r/collaborators/" + login + "/permission"); n != 1 {
			t.Errorf("permission requests for %s = %d, want 1", login, n)
		}
	}
}

func TestTrustedEmptyLoginMakesNoRequest(t *testing.T) {
	srv, c := client(t)
	srv.RoleStatus = http.StatusInternalServerError // any request would be an error
	got, err := NewTrust(c, defaultAllowed).Trusted(context.Background(), "")
	if err != nil || got {
		t.Fatalf(`Trusted("") = %v, %v; want false, nil`, got, err)
	}
	if n := srv.Count("/repos/o/r/collaborators//permission"); n != 0 {
		t.Errorf("permission requests = %d, want 0", n)
	}
}
