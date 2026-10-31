// Package kennforge talks to the kenn-forge HTTP API.
package kennforge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/CorneeldH/lighthouse/internal/workplace"
)

const defaultTimeout = 15 * time.Second

// Client is a kenn-forge HTTP client. The token is read from TokenFile on every request.
type Client struct {
	URL       string
	TokenFile string
	HTTP      *http.Client // nil: the client New builds
}

// New returns a client without proxy and without redirect following. The deadline of a
// request comes from its context, so Client.Timeout stays 0.
func New(url, tokenFile string) *Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = nil
	return &Client{
		URL:       strings.TrimRight(url, "/"),
		TokenFile: tokenFile,
		HTTP: &http.Client{
			Transport: tr,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// StatusError is a non-2xx answer below 500, a 3xx included.
type StatusError struct {
	Status int
	Detail string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("kenn-forge answered %d: %s", e.Status, e.Detail)
}

// serverError is a 5xx: the spawn was not accepted, but the status stays readable.
type serverError struct{ StatusError }

func (e *serverError) Is(target error) bool { return target == workplace.ErrNotAccepted }
func (e *serverError) Unwrap() error        { return &e.StatusError }

// do sends one request. A timeout of 0 means 15s.
func (c *Client) do(ctx context.Context, method, path string, body any, timeout time.Duration, out any) error {
	raw, err := os.ReadFile(c.TokenFile)
	token := strings.TrimSpace(string(raw))
	if err != nil || token == "" {
		return fmt.Errorf("token unreadable or empty: %w", workplace.ErrNotAccepted)
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", workplace.ErrNotAccepted)
		}
		rd = bytes.NewReader(b)
	}
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.URL+path, rd)
	if err != nil {
		return fmt.Errorf("build request: %w", workplace.ErrNotAccepted)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	hc := c.HTTP
	if hc == nil {
		hc = New(c.URL, c.TokenFile).HTTP
	}
	resp, err := hc.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" && ctx.Err() == nil {
			return fmt.Errorf("%s %s: %v: %w", method, path, err, workplace.ErrNotAccepted)
		}
		return fmt.Errorf("%s %s: %v: %w", method, path, err, workplace.ErrUncertain)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: read answer: %v: %w", method, path, err, workplace.ErrUncertain)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%s %s: %w", method, path, &serverError{StatusError{resp.StatusCode, detail(data)}})
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{resp.StatusCode, detail(data)}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s %s: answer is not JSON: %v: %w", method, path, err, workplace.ErrUncertain)
	}
	return nil
}

func detail(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
