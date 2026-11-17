package cli

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// apiForge records the APIRequest it gets and answers with out or err.
type apiForge struct {
	forge.Forge
	got []forge.APIRequest
	out string
	err error
}

func (a *apiForge) API(_ context.Context, r forge.APIRequest) ([]byte, error) {
	a.got = append(a.got, r)
	return []byte(a.out), a.err
}

func TestAPIDefaultMethodGETWithoutFields(t *testing.T) {
	f := &apiForge{out: `{"id":1}`}
	code, out, errOut := runForge(t, f, "api", "repos/{owner}/{repo}/issues")
	if code != 0 || out != `{"id":1}` {
		t.Fatalf("code %d out %q err %q, want 0 and the body unchanged", code, out, errOut)
	}
	want := []forge.APIRequest{{Path: "repos/{owner}/{repo}/issues"}}
	if !reflect.DeepEqual(f.got, want) {
		t.Errorf("request = %+v, want %+v (empty method: GET without fields)", f.got, want)
	}
}

func TestAPIFieldsGivePOSTAndExplicitMethodWins(t *testing.T) {
	f := &apiForge{}
	if code, _, e := runForge(t, f, "api", "x", "-f", "a=b"); code != 0 {
		t.Fatalf("code %d: %s", code, e)
	}
	if code, _, e := runForge(t, f, "api", "x", "-X", "PATCH", "-f", "a=b"); code != 0 {
		t.Fatalf("code %d: %s", code, e)
	}
	want := []forge.APIRequest{
		{Path: "x", Fields: []forge.APIField{{Name: "a", Value: "b"}}},
		{Method: "PATCH", Path: "x", Fields: []forge.APIField{{Name: "a", Value: "b"}}},
	}
	if !reflect.DeepEqual(f.got, want) {
		t.Errorf("requests = %+v, want %+v", f.got, want)
	}
}

func TestAPITypedFields(t *testing.T) {
	f := &apiForge{}
	code, _, e := runForge(t, f, "api", "x", "-F", "n=3", "-F", "ok=true", "-F", "x=null", "-F", "body=@/tmp/b.md", "-f", "s=3", "-f", "eq=a=b")
	if code != 0 {
		t.Fatalf("code %d: %s", code, e)
	}
	want := []forge.APIField{
		{Name: "n", Value: "3", Typed: true}, {Name: "ok", Value: "true", Typed: true},
		{Name: "x", Value: "null", Typed: true}, {Name: "body", Value: "@/tmp/b.md", Typed: true},
		{Name: "s", Value: "3"}, {Name: "eq", Value: "a=b"},
	}
	if len(f.got) != 1 || !reflect.DeepEqual(f.got[0].Fields, want) {
		t.Errorf("fields = %+v, want %+v", f.got, want)
	}
}

func TestAPIRejectsOtherFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--field", "a=b"}, {"--raw-field", "a=b"}, {"--jq", ".x"}, {"-q", ".x"},
		{"-f", "noequals"}, {"-f"}, {"-X"},
	} {
		f := &apiForge{}
		code, _, errOut := runForge(t, f, append([]string{"api", "x"}, args...)...)
		if code != 2 || len(f.got) != 0 {
			t.Errorf("%v: code %d, %d requests, want exit 2 and no request (%s)", args, code, len(f.got), errOut)
		}
	}
	if code, _, _ := runForge(t, &apiForge{}, "api"); code != 2 {
		t.Errorf("no path: code %d, want 2", code)
	}
	if code, _, _ := runForge(t, &apiForge{}, "api", "a", "b"); code != 2 {
		t.Errorf("two paths: code %d, want 2", code)
	}
}

func TestAPIGraphQLPath(t *testing.T) {
	for _, tc := range []struct{ kind, path string }{{"github", "graphql"}, {"gitlab", "api/graphql"}} {
		f := &apiForge{}
		code, _, e := runForge(t, f, "api", tc.path, "-f", "query={ x }")
		if code != 0 || len(f.got) != 1 || f.got[0].Path != tc.path {
			t.Errorf("%s: code %d requests %+v (%s), want path %q unchanged", tc.kind, code, f.got, e, tc.path)
		}
	}
}

func TestAPIForgeErrorExit1(t *testing.T) {
	f := &apiForge{err: errors.New("status 422: Validation Failed")}
	code, out, errOut := runForge(t, f, "api", "x")
	if code != 1 || out != "" || !strings.Contains(errOut, "Validation Failed") {
		t.Errorf("code %d out %q err %q, want 1, no output and the API message", code, out, errOut)
	}
}
