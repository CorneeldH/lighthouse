package codelang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CorneeldH/lighthouse/internal/config"
)

// items is ITEMS of tests/pack/test_code_language_llm.py.
var items = [][2]string{{"name", "werkboom"}}

// fakeLLM is tests/pack/_fake_llm.py: an OpenAI-compatible /v1/chat/completions. Mode per
// model: answer (verdict for each numbered item), silent (never replies), drip (headers, then
// one byte per 0.5 s), http500, garbage (a reply that is no JSON). It checks the parts of the
// request that belong to the contract.
type fakeLLM struct {
	url      string
	mu       sync.Mutex
	requests []map[string]any
	headers  []http.Header
}

var itemLine = regexp.MustCompile(`(?m)^\d+\. `)

func newFakeLLM(t *testing.T, modes map[string]string, fallback string) *fakeLLM {
	t.Helper()
	f := &fakeLLM{}
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request %s %s, want POST /v1/chat/completions", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type %q, want application/json", ct)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("body is no JSON: %v", err)
			return
		}
		if body["temperature"] != 0.0 {
			t.Errorf("temperature %v, want 0", body["temperature"])
		}
		if tokens, ok := body["max_tokens"].(float64); !ok || tokens < 512 {
			t.Errorf("max_tokens %v, want at least 512", body["max_tokens"])
		}
		model, _ := body["model"].(string)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		f.headers = append(f.headers, r.Header.Clone())
		f.mu.Unlock()
		mode, ok := modes[model]
		if !ok {
			mode = fallback
		}
		switch mode {
		case "silent":
			select {
			case <-r.Context().Done():
			case <-done:
			}
		case "http500":
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusInternalServerError)
		case "garbage":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "<html>not json</html>")
		case "drip":
			w.Header().Set("Content-Length", "1000")
			w.WriteHeader(http.StatusOK)
			for range 60 {
				if _, err := io.WriteString(w, " "); err != nil {
					return
				}
				w.(http.Flusher).Flush()
				select {
				case <-r.Context().Done():
					return
				case <-done:
					return
				case <-time.After(500 * time.Millisecond):
				}
			}
		default:
			messages, _ := body["messages"].([]any)
			first, _ := messages[0].(map[string]any)
			prompt, _ := first["content"].(string)
			answer := map[string]string{}
			for i := range itemLine.FindAllStringIndex(prompt, -1) {
				answer[fmt.Sprint(i+1)] = "NOT-EN"
			}
			content, _ := json.Marshal(answer)
			reply, _ := json.Marshal(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": string(content)}}},
			})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(reply)
		}
	}))
	t.Cleanup(func() {
		close(done)
		server.Close()
	})
	f.url = server.URL + "/v1"
	return f
}

func (f *fakeLLM) seen() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any{}, f.requests...)
}

func (f *fakeLLM) authorization() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, h := range f.headers {
		out = append(out, h.Get("Authorization"))
	}
	return out
}

func (f *fakeLLM) forget() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests, f.headers = nil, nil
}

// testChain is _chain of the Python tests.
func testChain(url string, models ...string) *Chain {
	if len(models) == 0 {
		models = []string{"m1"}
	}
	return &Chain{URL: url, Models: models, KeyEnv: "LLM_TEST_KEY"}
}

// ask runs Ask with the production deadlines.
func ask(ch *Chain, stateDir string) Reply {
	return Ask(context.Background(), ch, items, stateDir, PerModel, Total)
}

func withKey(t *testing.T) {
	t.Helper()
	t.Setenv("LLM_TEST_KEY", "sekrit")
}

func parseTOML(t *testing.T, body string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(body), filepath.Join(t.TempDir(), "lighthouse.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestChainFrom(t *testing.T) {
	if got, err := ChainFrom(parseTOML(t, "")); got != nil || err != nil {
		t.Fatalf("no table: got %+v, %v; want nil, nil", got, err)
	}
	home := t.TempDir()
	env := filepath.Join(home, "env")
	writeFile(t, env, []byte("K=1\n"))
	t.Setenv("HOME", home)
	full := "[code_language_llm]\nurl = \"http://h:8099/v1\"\nmodels = [\"a\", \"b\"]\n" +
		"key_env = \"K\"\nenv_file = \"~/env\"\n"
	got, err := ChainFrom(parseTOML(t, full))
	want := &Chain{URL: "http://h:8099/v1", Models: []string{"a", "b"}, KeyEnv: "K", EnvFile: env}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("full: got %+v, %v; want %+v", got, err, want)
	}
	for _, c := range []struct{ body, want string }{
		{"[code_language_llm]\n", "key code_language_llm.url is missing"},
		{"[code_language_llm]\nmodels = [\"a\"]\n", "key code_language_llm.url is missing"},
		{"[code_language_llm]\nurl = \"u\"\n", "key code_language_llm.models is missing or empty"},
		{"[code_language_llm]\nurl = \"u\"\nmodels = []\n", "key code_language_llm.models is missing or empty"},
		{"[code_language_llm]\nurl = \"u\"\nmodels = [\"a\"]\nenv_file = \"rel/env\"\n",
			"code_language_llm.env_file is relative: rel/env"},
		{"[code_language_llm]\nurl = \"u\"\nmodels = [\"a\"]\nenv_file = \"/no/such/env\"\n",
			"code_language_llm.env_file does not exist: /no/such/env"},
		{"[code_language_llm]\nurl = \"u\"\nmodels = [\"a\"]\nenv_file = \"~/missing\"\n",
			"code_language_llm.env_file does not exist: " + filepath.Join(home, "missing")},
		{"[code_language_llm]\nurl = \"u\"\nmodels = [\"a\"]\nenv_file = \"" + home + "\"\n",
			"code_language_llm.env_file does not exist: " + home},
	} {
		got, err := ChainFrom(parseTOML(t, c.body))
		var ce *config.Error
		if got != nil || !errors.As(err, &ce) || ce.Msg != c.want {
			t.Errorf("%q: got %+v, %v; want config error %q", c.body, got, err, c.want)
		}
	}
	bare, err := ChainFrom(parseTOML(t, "[code_language_llm]\nurl = \"u\"\nmodels = [\"a\"]\n"))
	if err != nil || bare.KeyEnv != "" || bare.EnvFile != "" {
		t.Errorf("bare: got %+v, %v; want no key_env and no env_file", bare, err)
	}
}

// llmFixture is tests/pack/fixtures/code_language/llm.json.
type llmFixture struct {
	Prompt []struct {
		Items [][2]string `json:"items"`
		Want  string      `json:"want"`
	} `json:"prompt"`
	Parse []struct {
		Content string `json:"content"`
		N       int    `json:"n"`
		Want    []bool `json:"want"`
	} `json:"parse"`
}

func TestPromptFixture(t *testing.T) {
	var fx llmFixture
	readFixture(t, "llm.json", &fx)
	if len(fx.Prompt) == 0 {
		t.Fatal("llm.json has no prompt cases")
	}
	for _, c := range fx.Prompt {
		if got := BuildPrompt(c.Items); got != c.Want {
			t.Errorf("BuildPrompt(%q) = %q, want %q", c.Items, got, c.Want)
		}
	}
}

func TestParseFixture(t *testing.T) {
	var fx llmFixture
	readFixture(t, "llm.json", &fx)
	if len(fx.Parse) == 0 {
		t.Fatal("llm.json has no parse cases")
	}
	for _, c := range fx.Parse {
		got, err := ParseAnswer(c.Content, c.N)
		if c.Want == nil {
			if err == nil {
				t.Errorf("ParseAnswer(%q) = %v, want an error", c.Content, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.Want) {
			t.Errorf("ParseAnswer(%q) = %v, %v; want %v", c.Content, got, err, c.Want)
		}
	}
}

func TestParseAnswerEdges(t *testing.T) {
	for _, c := range []struct {
		content string
		want    []bool
	}{
		{`a {"1": "NOT-EN"} b }`, nil},   // the greedy match ends at the last brace
		{`[{"1": "EN"}]`, []bool{false}}, // the object inside the brackets
		{`{"1": "EN", "2": "EN"}`, []bool{false}},
		{"{\n\"1\": \"NOT-EN\"\n}", []bool{true}},
		{`{"1": "EN"`, nil},
		{"", nil},
	} {
		got, err := ParseAnswer(c.content, 1)
		if c.want == nil {
			if err == nil {
				t.Errorf("ParseAnswer(%q) = %v, want an error", c.content, got)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseAnswer(%q) = %v, %v; want %v", c.content, got, err, c.want)
		}
	}
}

func TestAskAnswerUsed(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, map[string]string{"m1": "answer"}, "answer")
	reply := ask(testChain(fake.url, "m1", "m2"), t.TempDir())
	if !reflect.DeepEqual(reply.NotEnglish, []bool{true}) || reply.Model != "m1" || len(reply.Failures) != 0 {
		t.Fatalf("reply %+v, want [true] from m1 without failures", reply)
	}
	seen := fake.seen()
	if len(seen) != 1 {
		t.Fatalf("%d requests, want 1", len(seen))
	}
	if seen[0]["model"] != "m1" {
		t.Errorf("model %v, want m1", seen[0]["model"])
	}
	prompt := seen[0]["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(prompt, "1. name: werkboom") {
		t.Errorf("prompt %q lacks the item", prompt)
	}
	if got := fake.authorization(); got[0] != "Bearer sekrit" {
		t.Errorf("Authorization %q, want Bearer sekrit", got[0])
	}
}

func TestAskMaxTokensLeavesRoomToReason(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "answer")
	many := make([][2]string, 100)
	for i := range many {
		many[i] = [2]string{"name", fmt.Sprintf("unit%d", i)}
	}
	ask(testChain(fake.url), t.TempDir())
	Ask(context.Background(), testChain(fake.url), many, t.TempDir(), PerModel, Total)
	var got []any
	for _, r := range fake.seen() {
		got = append(got, r["max_tokens"])
	}
	if want := []any{512.0, 1220.0}; !reflect.DeepEqual(got, want) {
		t.Errorf("max_tokens %v, want %v", got, want)
	}
}

func TestKeyFromEnvFile(t *testing.T) {
	t.Setenv("LLM_TEST_KEY", "")
	env := filepath.Join(t.TempDir(), "env")
	writeFile(t, env, []byte("# comment\n\nOTHER=x\nLLM_TEST_KEY=\"abc\"\n"))
	fake := newFakeLLM(t, nil, "answer")
	chain := testChain(fake.url)
	chain.EnvFile = env
	reply := ask(chain, t.TempDir())
	if !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Fatalf("reply %+v, want [true]", reply)
	}
	if got := fake.authorization(); got[0] != "Bearer abc" {
		t.Errorf("Authorization %q, want Bearer abc", got[0])
	}
}

func TestKeyFromEnvFileEdges(t *testing.T) {
	t.Setenv("LLM_TEST_KEY", "")
	for _, c := range []struct{ file, want string }{
		{" LLM_TEST_KEY = 'x=y' \r\n", "x=y"},      // spaces, quotes, a second =
		{"LLM_TEST_KEY=\nLLM_TEST_KEY=late\n", ""}, // the first match counts, empty is none
		{"LLM_TEST_KEYS=no\nLLM_TEST_KEY\nLLM_TEST_KEY=yes", "yes"},
		{"\xff\nLLM_TEST_KEY=bad\n", ""}, // not UTF-8
	} {
		env := filepath.Join(t.TempDir(), "env")
		writeFile(t, env, []byte(c.file))
		if got := key(&Chain{KeyEnv: "LLM_TEST_KEY", EnvFile: env}); got != c.want {
			t.Errorf("key from %q = %q, want %q", c.file, got, c.want)
		}
	}
}

func TestKeyMissingFailsWithoutCall(t *testing.T) {
	t.Setenv("LLM_TEST_KEY", "")
	fake := newFakeLLM(t, nil, "answer")
	reply := ask(testChain(fake.url), t.TempDir())
	want := []string{"key: LLM_TEST_KEY is set in neither the environment nor env_file"}
	if reply.NotEnglish != nil || !reflect.DeepEqual(reply.Failures, want) {
		t.Fatalf("reply %+v, want failures %q", reply, want)
	}
	if n := len(fake.seen()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

func TestKeyEnvAbsentSendsNoAuthorization(t *testing.T) {
	fake := newFakeLLM(t, nil, "answer")
	chain := testChain(fake.url)
	chain.KeyEnv = ""
	reply := ask(chain, t.TempDir())
	if !reflect.DeepEqual(reply.NotEnglish, []bool{true}) {
		t.Fatalf("reply %+v, want [true]", reply)
	}
	if got := fake.authorization(); got[0] != "" {
		t.Errorf("Authorization %q, want none", got[0])
	}
}

func TestAskChainFallsThrough(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, map[string]string{"m1": "http500", "m2": "answer"}, "answer")
	reply := ask(testChain(fake.url, "m1", "m2"), t.TempDir())
	if reply.Model != "m2" || !reflect.DeepEqual(reply.NotEnglish, []bool{true}) ||
		!reflect.DeepEqual(reply.Failures, []string{"m1: HTTP 500"}) {
		t.Fatalf("reply %+v, want [true] from m2 after m1: HTTP 500", reply)
	}
}

func TestAskEveryFailureKind(t *testing.T) {
	withKey(t)
	for _, c := range []struct{ mode, want string }{
		{"silent", "m1: no answer within 0.5 s"},
		{"drip", "m1: no answer within 0.5 s"},
		{"http500", "m1: HTTP 500"},
		{"garbage", "m1: unreadable answer"},
		{"invalid", "m1: "},
	} {
		t.Run(c.mode, func(t *testing.T) {
			url := "http://lighthouse-test.invalid/v1"
			if c.mode != "invalid" {
				url = newFakeLLM(t, map[string]string{"m1": c.mode}, "answer").url
			}
			start := time.Now()
			reply := Ask(context.Background(), testChain(url), items, t.TempDir(),
				500*time.Millisecond, time.Second)
			if elapsed := time.Since(start); elapsed >= 1500*time.Millisecond {
				t.Errorf("took %v, want under 1.5 s", elapsed)
			}
			if reply.NotEnglish != nil || reply.Model != "" || len(reply.Failures) != 1 {
				t.Fatalf("reply %+v, want one failure and no answer", reply)
			}
			got := reply.Failures[0]
			if c.mode == "invalid" {
				if !strings.HasPrefix(got, c.want) || strings.Contains(got, "lighthouse-test.invalid/v1") {
					t.Errorf("failure %q, want prefix %q without the URL", got, c.want)
				}
				return
			}
			if got != c.want {
				t.Errorf("failure %q, want %q", got, c.want)
			}
		})
	}
}

func TestAskTotalDeadline(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "silent")
	start := time.Now()
	reply := Ask(context.Background(), testChain(fake.url, "m1", "m2", "m3"), items, t.TempDir(),
		500*time.Millisecond, 800*time.Millisecond)
	if elapsed := time.Since(start); elapsed >= 1200*time.Millisecond {
		t.Errorf("took %v, want under 1.2 s", elapsed)
	}
	if reply.NotEnglish != nil || len(reply.Failures) != 3 {
		t.Fatalf("reply %+v, want three failures and no answer", reply)
	}
	if got, want := reply.Failures[0], "m1: no answer within 0.5 s"; got != want {
		t.Errorf("failure 1 %q, want %q", got, want)
	}
	if !strings.HasPrefix(reply.Failures[1], "m2: no answer within 0.") {
		t.Errorf("failure 2 %q, want m2 cut off by the rest of the total", reply.Failures[1])
	}
	if got, want := reply.Failures[2], "m3: total time of 0.8 s is up"; got != want {
		t.Errorf("failure 3 %q, want %q", got, want)
	}
	if n := len(fake.seen()); n != 2 {
		t.Errorf("%d requests, want 2", n)
	}
}

func TestAskCanceledContextStopsTheChain(t *testing.T) {
	withKey(t)
	fake := newFakeLLM(t, nil, "silent")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	reply := Ask(ctx, testChain(fake.url, "m1", "m2"), items, t.TempDir(), time.Minute, time.Minute)
	if elapsed := time.Since(start); elapsed >= 5*time.Second {
		t.Errorf("took %v, want an immediate stop", elapsed)
	}
	if reply.NotEnglish != nil || len(reply.Failures) != 2 ||
		!strings.HasPrefix(reply.Failures[0], "m1: ") || !strings.HasPrefix(reply.Failures[1], "m2: ") {
		t.Fatalf("reply %+v, want a failure per model", reply)
	}
}

func TestStateDirDefault(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1")
	if got, want := stateDirFor(4242), filepath.Join("/run/user/1", "lighthouse-code-language-4242"); got != want {
		t.Errorf("with XDG_RUNTIME_DIR: %q, want %q", got, want)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got, want := stateDirFor(4242), filepath.Join("/tmp", "lighthouse-code-language-4242"); got != want {
		t.Errorf("without XDG_RUNTIME_DIR: %q, want %q", got, want)
	}
	if got, want := DefaultStateDir(), stateDirFor(os.Getuid()); got != want {
		t.Errorf("DefaultStateDir() = %q, want %q", got, want)
	}
}
