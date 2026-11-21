package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/CorneeldH/lighthouse/internal/forge"
)

// Classification: these tests fail when the command drops or renames a JSON key, reorders the
// releases, stops passing --repo or the default repo to the reader, breaks --field, or turns a
// reader error into exit 0. They observe stdout, stderr and the exit code only.

type fakeReleaseReader struct {
	rels []forge.Release
	err  error
	repo string
}

func (f *fakeReleaseReader) Releases(_ context.Context, repo string) ([]forge.Release, error) {
	f.repo = repo
	return f.rels, f.err
}

func twoReleases() []forge.Release {
	return []forge.Release{
		{Tag: "v1.1.0-rc1", Name: "rc", Body: "b2", URL: "u2", Prerelease: true},
		{Tag: "v1.0.0", Name: "one", Body: "b1", URL: "u1"},
	}
}

func runReleases(rr forge.ReleaseReader, args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	cmd := newReleasesCommandWith(rr)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	code := execute(cmd, &errOut)
	return out.String(), errOut.String(), code
}

func TestReleasesPrintsJSON(t *testing.T) {
	fr := &fakeReleaseReader{rels: twoReleases()}
	out, errOut, code := runReleases(fr)
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	want := `[
  {
    "body": "b2",
    "draft": false,
    "name": "rc",
    "prerelease": true,
    "tag": "v1.1.0-rc1",
    "url": "u2"
  },
  {
    "body": "b1",
    "draft": false,
    "name": "one",
    "prerelease": false,
    "tag": "v1.0.0",
    "url": "u1"
  }
]
`
	if out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
	if fr.repo != "CorneeldH/lighthouse" {
		t.Errorf("repo = %q, want the default", fr.repo)
	}
}

func TestReleasesRepoFlag(t *testing.T) {
	fr := &fakeReleaseReader{}
	if _, _, code := runReleases(fr, "--repo", "openai/codex"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if fr.repo != "openai/codex" {
		t.Errorf("repo = %q", fr.repo)
	}
}

func TestReleasesField(t *testing.T) {
	out, _, code := runReleases(&fakeReleaseReader{rels: twoReleases()}, "--field", "tag")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := "v1.1.0-rc1\nv1.0.0\n"; out != want {
		t.Errorf("stdout = %q, want %q", out, want)
	}
}

func TestReleasesError(t *testing.T) {
	out, errOut, code := runReleases(&fakeReleaseReader{err: errors.New("boom")})
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if out != "" || !strings.Contains(errOut, "boom") {
		t.Errorf("stdout %q, stderr %q", out, errOut)
	}
}
