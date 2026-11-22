package server

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOrigin reports whether the request's Origin header names the host the
// request was addressed to. A missing, unparsable or host-less Origin (such as
// "null") is never the same origin. The comparison is literal: a port in
// Origin must match a port in Host, and no default port is filled in.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
