package server

import (
	"net"
	"net/http"
	"strings"
)

// hostGuard refuses every Host that is not an IP literal, localhost or a *.ts.net name.
// A DNS-rebinding page reaches loopback under its own name; tailscale serve forwards the
// incoming Host unchanged, so tailnet names end in .ts.net.
func hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowHost(r.Host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if net.ParseIP(host) != nil {
		return true
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	return host == "localhost" || (strings.HasSuffix(host, ".ts.net") && len(host) > len(".ts.net"))
}
