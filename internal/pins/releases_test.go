package pins

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
	"github.com/CorneeldH/lighthouse/internal/forge/forgetest"
)

type fakeReleases struct {
	rels []forge.Release
	err  error
}

func (f fakeReleases) Releases(context.Context, string) ([]forge.Release, error) {
	return f.rels, f.err
}

func rel(tag string) forge.Release { return forge.Release{Tag: tag, URL: "https://x/" + tag} }

func tags(rs []forge.Release) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Tag)
	}
	return out
}

func pin(version string) Pin {
	return Pin{Name: "p", Kind: KindPlugin, Repo: "up/stream", Version: version, TagPrefix: "v"}
}

func TestMeasureRelease_current(t *testing.T) {
	got := MeasureRelease(context.Background(), fakeReleases{rels: []forge.Release{rel("v1.2.0"), rel("v1.1.0")}}, pin("1.2.0"))
	if got.Status != StatusCurrent || got.Target != "1.2.0" || len(got.Releases) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestMeasureRelease_behindListsIntermediate(t *testing.T) {
	// unsorted on purpose: order comes from semver, not from the API
	rs := []forge.Release{rel("v1.1.0"), rel("v1.3.0"), rel("v1.0.0"), rel("v1.2.0")}
	got := MeasureRelease(context.Background(), fakeReleases{rels: rs}, pin("1.1.0"))
	if got.Status != StatusBehind || got.Target != "1.3.0" {
		t.Fatalf("got %+v", got)
	}
	if want := []string{"v1.3.0", "v1.2.0"}; !reflect.DeepEqual(tags(got.Releases), want) {
		t.Fatalf("releases = %v, want %v (newest first, excluding the pin)", tags(got.Releases), want)
	}
	if !strings.Contains(got.Detail, "2") {
		t.Fatalf("detail %q should count the releases behind", got.Detail)
	}
}

func TestMeasureRelease_pinOnPage2IsNotError(t *testing.T) {
	srv := forgetest.New(t)
	var items []map[string]any
	for i := 200; i >= 1; i-- { // newest first, as GitHub; 200 releases, pin is the 146th
		items = append(items, map[string]any{"tag_name": fmt.Sprintf("v1.%d.0", i), "html_url": "u"})
	}
	srv.Releases = map[string][]map[string]any{"up/stream": items}
	c := forge.New("o/r", "tok")
	c.BaseURL = srv.URL()
	got := MeasureRelease(context.Background(), c, pin("1.55.0")) // 200-146+1 = 55
	if got.Status != StatusBehind || got.Target != "1.200.0" || len(got.Releases) != 145 {
		t.Fatalf("status %s target %s releases %d detail %q", got.Status, got.Target, len(got.Releases), got.Detail)
	}
}

func TestMeasureRelease_errors(t *testing.T) {
	cases := []struct {
		name string
		rr   fakeReleases
		pin  Pin
	}{
		{"pin release missing", fakeReleases{rels: []forge.Release{rel("v2.0.0"), rel("v1.0.0")}}, pin("1.5.0")},
		{"latest release below pin", fakeReleases{rels: []forge.Release{rel("v1.0.0")}}, pin("1.5.0")},
		{"no usable release", fakeReleases{rels: []forge.Release{rel("latest"), {Tag: "v1.0.0", Draft: true}}}, pin("1.0.0")},
		{"api unreachable", fakeReleases{err: errors.New("dial tcp: refused")}, pin("1.0.0")},
		{"pin tag does not parse", fakeReleases{rels: []forge.Release{rel("v1.0.0")}}, pin("one")},
		{"pin load error", fakeReleases{rels: []forge.Release{rel("v1.0.0")}}, Pin{Name: "p", Repo: "up/stream", Err: errors.New("pin p: version is missing")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MeasureRelease(context.Background(), tc.rr, tc.pin)
			if got.Status != StatusError || got.Detail == "" {
				t.Fatalf("got %+v, want error with detail", got)
			}
		})
	}
}

func TestMeasureRelease_skipsDraftPrereleaseAndForeignTags(t *testing.T) {
	rs := []forge.Release{
		rel("rust-v9.0.0"), rel("latest"), rel("v2.0.0-rc.1"),
		{Tag: "v3.0.0", Draft: true}, {Tag: "v4.0.0", Prerelease: true},
		rel("v1.1.0"), rel("v1.0.0"),
	}
	got := MeasureRelease(context.Background(), fakeReleases{rels: rs}, pin("1.0.0"))
	if got.Status != StatusBehind || got.Target != "1.1.0" || !reflect.DeepEqual(tags(got.Releases), []string{"v1.1.0"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestMeasureRelease_tagPrefix(t *testing.T) {
	rs := []forge.Release{rel("rust-v0.170.0"), rel("rust-v0.169.0"), rel("v9.9.9"), rel("rust-v0.157.0")}
	p := Pin{Name: "codex", Kind: KindTool, Repo: "openai/codex", Version: "0.157.0", TagPrefix: "rust-v"}
	got := MeasureRelease(context.Background(), fakeReleases{rels: rs}, p)
	if got.Status != StatusBehind || got.Target != "0.170.0" ||
		!reflect.DeepEqual(tags(got.Releases), []string{"rust-v0.170.0", "rust-v0.169.0"}) {
		t.Fatalf("got %+v", got)
	}
}
