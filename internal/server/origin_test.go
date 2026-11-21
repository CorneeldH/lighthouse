package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
		hasOrg bool
		host   string
		want   bool
	}{
		{name: "https tailnet name", origin: "https://x.ts.net", hasOrg: true, host: "x.ts.net", want: true},
		{name: "localhost with port", origin: "http://localhost:5173", hasOrg: true, host: "localhost:5173", want: true},
		{name: "host is case-insensitive", origin: "https://X.ts.net", hasOrg: true, host: "x.ts.NET", want: true},
		{name: "different host with port", origin: "http://localhost:5173", hasOrg: true, host: "127.0.0.1:8127", want: false},
		{name: "no Origin header", hasOrg: false, host: "x.ts.net", want: false},
		{name: "Origin null", origin: "null", hasOrg: true, host: "x.ts.net", want: false},
		{name: "foreign origin", origin: "https://evil.example", hasOrg: true, host: "x.ts.net", want: false},
		{name: "no default port filled in", origin: "http://x.ts.net:80", hasOrg: true, host: "x.ts.net", want: false},
		{name: "origin without host", origin: "https://", hasOrg: true, host: "x.ts.net", want: false},
		{name: "origin without host, empty request host", origin: "https://", hasOrg: true, host: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/priorities", nil)
			req.Host = tc.host
			if tc.hasOrg {
				req.Header.Set("Origin", tc.origin)
			}
			if got := sameOrigin(req); got != tc.want {
				t.Errorf("sameOrigin = %v, want %v", got, tc.want)
			}
		})
	}
}
