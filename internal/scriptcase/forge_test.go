package scriptcase

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func send(t *testing.T, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestReplayForgeAnswersInOrder(t *testing.T) {
	calls := []ForgeCall{
		{Method: "GET", Path: "/repos/o/r/commits/abc", Status: 404, Body: json.RawMessage(`{"message":"Not Found"}`)},
		{Method: "POST", Path: "/repos/o/r/issues/1/comments", RequestBody: json.RawMessage(`{"body":"x"}`), Status: 201,
			Body: json.RawMessage(`{"id":1}`), Headers: map[string]string{"X-A": "b"}},
	}
	r := &recorder{TB: t}
	url := ReplayForge(r, calls)

	if code, body := send(t, "GET", url+"/repos/o/r/commits/abc", ""); code != 404 || body != `{"message":"Not Found"}` {
		t.Errorf("call 1: %d %q", code, body)
	}
	if code, body := send(t, "POST", url+"/repos/o/r/issues/1/comments", `{ "body": "x" }`); code != 201 || body != `{"id":1}` {
		t.Errorf("call 2: %d %q", code, body)
	}
	r.finish()
	if len(r.errors) != 0 {
		t.Errorf("unexpected failures: %v", r.errors)
	}
}

func TestReplayForgeFailsOnWrongBodyExtraAndMissingCalls(t *testing.T) {
	calls := []ForgeCall{
		{Method: "POST", Path: "/p", RequestBody: json.RawMessage(`{"body":"x"}`), Status: 201},
		{Method: "GET", Path: "/q", Status: 200},
	}
	r := &recorder{TB: t}
	url := ReplayForge(r, calls)

	if code, _ := send(t, "POST", url+"/p", `{"body":"y"}`); code != 599 {
		t.Errorf("wrong body answered %d, want 599", code)
	}
	if code, _ := send(t, "GET", url+"/other", ""); code != 599 {
		t.Errorf("wrong path answered %d, want 599", code)
	}
	r.finish()
	joined := strings.Join(r.errors, "\n")
	for _, want := range []string{"expected request body", "expected POST /p, got GET /other", "2 of 2 scripted calls were not made"} {
		if !strings.Contains(joined, want) {
			t.Errorf("failures %q lack %q", joined, want)
		}
	}
}
