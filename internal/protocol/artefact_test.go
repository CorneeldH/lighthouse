package protocol

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/orderedjson"
)

// artefactCase is one case of tests/pack/fixtures/artefacts/cases.json, the fixture that
// tests/pack/test_artefact_cases.py ran against artefacts.py first.
type artefactCase struct {
	Name   string          `json:"name"`
	Fn     string          `json:"fn"`
	Input  artefactInput   `json:"input"`
	Expect json.RawMessage `json:"expect"`
}

type artefactInput struct {
	Meta     *metaIn         `json:"meta"`
	Content  string          `json:"content"`
	Repo     json.RawMessage `json:"repo"`
	Language *string         `json:"language"`
	Extra    json.RawMessage `json:"extra"`
	Body     string          `json:"body"`
	Text     json.RawMessage `json:"text"`
	Limit    *int            `json:"limit"`
	Quarter  bool            `json:"limit_quarter"`
	Chain    []chainIn       `json:"comments"`
	Kind     string          `json:"kind"`
	File     string          `json:"file"`
}

type metaIn struct {
	Kind     string  `json:"kind"`
	V        int     `json:"version"`
	P        int     `json:"part"`
	N        int     `json:"parts"`
	File     string  `json:"file"`
	Commit   string  `json:"commit"`
	Hash     string  `json:"hash"`
	Replaces *string `json:"replaces"`
}

type chainIn struct {
	ID     int64   `json:"id"`
	Meta   *metaIn `json:"meta"`
	Body   string  `json:"body"`
	Legacy bool    `json:"legacy_marker"`
}

func (m metaIn) meta(t *testing.T, extra json.RawMessage) ArtefactMeta {
	t.Helper()
	out := ArtefactMeta{Kind: m.Kind, Version: m.V, Part: m.P, Parts: m.N, File: m.File,
		Commit: m.Commit, Hash: m.Hash, Replaces: m.Replaces}
	if len(extra) > 0 {
		v, err := orderedjson.Parse(extra)
		if err != nil {
			t.Fatal(err)
		}
		out.Extra = v.(*orderedjson.Object)
	}
	return out
}

// text is the `text` input: a string, or {"repeat": [[piece, count], ...]}.
func (in artefactInput) text(t *testing.T) string {
	t.Helper()
	var s string
	if json.Unmarshal(in.Text, &s) == nil {
		return s
	}
	var spec struct {
		Repeat [][]any `json:"repeat"`
	}
	if err := json.Unmarshal(in.Text, &spec); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, piece := range spec.Repeat {
		b.WriteString(strings.Repeat(piece[0].(string), int(piece[1].(float64))))
	}
	return b.String()
}

// repo and language are what the Python runner writes to lighthouse.toml; an explicit null repo
// means no repo at all.
func (in artefactInput) repoAndLanguage() (repo, language string) {
	repo, language = "Owner/repo", "nl"
	if len(in.Repo) > 0 {
		var r *string
		_ = json.Unmarshal(in.Repo, &r)
		repo = ""
		if r != nil {
			repo = *r
		}
	}
	if in.Language != nil {
		language = *in.Language
	}
	return repo, language
}

// generic turns a Go value into what json.Unmarshal gives for its JSON text, to compare with a fixture.
func generic(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func metaView(t *testing.T, m ArtefactMeta, ok bool) any {
	t.Helper()
	if !ok {
		return nil
	}
	extra := map[string]any{}
	if m.Extra != nil {
		raw, err := orderedjson.Canonical(m.Extra)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &extra); err != nil {
			t.Fatal(err)
		}
	}
	return generic(t, map[string]any{"kind": m.Kind, "version": m.Version, "part": m.Part, "parts": m.Parts,
		"file": m.File, "commit": m.Commit, "hash": m.Hash, "replaces": m.Replaces, "extra": extra})
}

func TestArtefactCases(t *testing.T) {
	raw, err := os.ReadFile("../../tests/pack/fixtures/artefacts/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []artefactCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var want any
			if err := json.Unmarshal(c.Expect, &want); err != nil {
				t.Fatal(err)
			}
			got := runArtefactCase(t, c)
			// An error case names a piece of the message: Python's carries the config path too.
			if w, ok := want.(map[string]any); ok && w["error_contains"] != nil {
				g, _ := got.(map[string]any)
				msg, _ := g["error_contains"].(string)
				if !strings.Contains(msg, w["error_contains"].(string)) {
					t.Errorf("error %q lacks %q", msg, w["error_contains"])
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s:\n got  %#v\n want %#v", c.Fn, got, want)
			}
		})
	}
}

func runArtefactCase(t *testing.T, c artefactCase) any {
	t.Helper()
	in := c.Input
	repo, language := in.repoAndLanguage()
	render := func(content string) (string, error) {
		return RenderArtefact(in.Meta.meta(t, in.Extra), content, language, repo)
	}
	switch c.Fn {
	case "constants":
		return generic(t, map[string]any{"KINDS": ArtefactKinds, "GITHUB_LIMIT": GithubLimit})
	case "parse_meta":
		m, ok := ParseArtefactMeta(in.Body)
		return metaView(t, m, ok)
	case "roundtrip", "parse_meta_roundtrip":
		body, err := render(in.Content)
		if err != nil {
			t.Fatal(err)
		}
		m, ok := ParseArtefactMeta(body)
		if c.Fn == "roundtrip" {
			return generic(t, map[string]any{"meta": metaView(t, m, ok)})
		}
		return metaView(t, m, ok)
	case "render":
		body, err := render(in.Content)
		if err != nil {
			return map[string]any{"error_contains": err.Error()}
		}
		return map[string]any{"body": body}
	case "hash_of":
		var text string
		if err := json.Unmarshal(in.Text, &text); err != nil {
			t.Fatal(err)
		}
		return ArtefactHash([]byte(text))
	case "split":
		text := in.text(t)
		limit := DefaultSplitLimit
		switch {
		case in.Quarter:
			limit = len([]rune(text))/4 + 1
		case in.Limit != nil:
			limit = *in.Limit
		}
		parts := SplitArtefact(text, limit)
		if strings.Join(parts, "") != text {
			t.Fatalf("the parts do not make the text again")
		}
		out := map[string]any{"parts": parts}
		if len([]rune(text)) > 4000 {
			lengths := make([]int, len(parts))
			for i, p := range parts {
				lengths[i] = len([]rune(p))
			}
			out = map[string]any{"lengths": lengths}
		}
		if in.Quarter || len([]rune(text)) > 4000 {
			out["fits_limit"] = fits(t, parts)
		}
		return generic(t, out)
	case "chain":
		cs := make([]Comment, len(in.Chain))
		for i, item := range in.Chain {
			body := item.Body
			if item.Meta != nil {
				var err error
				if body, err = RenderArtefact(item.Meta.meta(t, nil), "v"+strconv.Itoa(item.Meta.V)+"d"+strconv.Itoa(item.Meta.P), language, repo); err != nil {
					t.Fatal(err)
				}
				if item.Legacy {
					body = strings.Replace(body, "<!-- lighthouse:", "<!-- paia:", 1)
				}
			}
			cs[i] = Comment{ID: item.ID, Body: body}
		}
		ids := []int64{}
		for _, c := range CurrentArtefact(cs, in.Kind, in.File) {
			ids = append(ids, c.ID)
		}
		return generic(t, map[string]any{"current": ids, "next": NextArtefactVersion(cs, in.Kind, in.File)})
	}
	t.Fatalf("unknown fn %q", c.Fn)
	return nil
}

// fits reports that every part still fits a GitHub comment when rendered with the longest header.
func fits(t *testing.T, parts []string) bool {
	t.Helper()
	url := "https://github.com/Owner/repo/issues/1#issuecomment-1234567890"
	m := ArtefactMeta{Kind: "plan", Version: 99, Part: 99, Parts: 99, File: "plans/x.md",
		Commit: strings.Repeat("a", 40), Hash: strings.Repeat("0", 12), Replaces: &url}
	for _, p := range parts {
		body, err := RenderArtefact(m, p, "nl", "Owner/repo")
		if err != nil {
			t.Fatal(err)
		}
		if len([]rune(body)) >= GithubLimit {
			return false
		}
	}
	return true
}

// The source link of the header follows the forge: GitHub's /blob/, GitLab's /-/blob/ on the
// configured host, gitlab.com without one.
func TestRenderArtefactAtLinksToTheForge(t *testing.T) {
	m := ArtefactMeta{Kind: "plan", Version: 1, Part: 1, Parts: 1, File: "plans/x.md",
		Commit: strings.Repeat("a", 40), Hash: strings.Repeat("0", 12)}
	for _, tc := range []struct {
		name string
		src  Source
		want string
	}{
		{"github", Source{Kind: "github", Repo: "Owner/repo"}, `href="https://github.com/Owner/repo/blob/` + m.Commit + `/plans/x.md"`},
		{"gitlab host", Source{Kind: "gitlab", Host: "git.example.org", Repo: "grp/sub/repo"}, `href="https://git.example.org/grp/sub/repo/-/blob/` + m.Commit + `/plans/x.md"`},
		{"gitlab default host", Source{Kind: "gitlab", Repo: "grp/repo"}, `href="https://gitlab.com/grp/repo/-/blob/` + m.Commit + `/plans/x.md"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := RenderArtefactAt(m, "x", "en", tc.src)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body, tc.want) {
				t.Errorf("body lacks %s:\n%s", tc.want, body)
			}
		})
	}
	if _, err := RenderArtefactAt(m, "x", "en", Source{Kind: "gitlab"}); err == nil {
		t.Error("an empty repo rendered")
	}
}
