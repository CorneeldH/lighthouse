package checkpointscan

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func entireDir(t *testing.T, shared, local string) string {
	t.Helper()
	top := t.TempDir()
	dir := filepath.Join(top, ".entire")
	noErr(t, os.Mkdir(dir, 0o755))
	if shared != "" {
		noErr(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte(shared), 0o600))
	}
	if local != "" {
		noErr(t, os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(local), 0o600))
	}
	return top
}

const githubCkpt = `{"strategy_options": {"push_sessions": false, "checkpoint_remote": {"provider": "github", "repo": "Acme/ckpt"}}}`

func settingsFor(t *testing.T, shared, local string) EntireSettings {
	t.Helper()
	s, err := ReadEntireSettings(entireDir(t, shared, local))
	noErr(t, err)
	return s
}

func TestIsCheckpointRemoteByNameAndURLAndSCP(t *testing.T) {
	s := settingsFor(t, githubCkpt, "")
	for url, want := range map[string]bool{
		"https://github.com/Acme/ckpt.git":    true,
		"https://github.com/Acme/ckpt":        true,
		"https://github.com/Acme/ckpt/":       true,
		"git@github.com:Acme/ckpt.git":        true,
		"git@github.com:Acme/ckpt":            true,
		"ssh://git@github.com/Acme/ckpt.git":  true,
		"https://github.com/Acme/widgets.git": false,
		"https://gitlab.com/Acme/ckpt.git":    false,
		"/srv/ckpt.git":                       false,
	} {
		eq(t, want, IsCheckpointRemote(s, "c", url), url)
	}

	// A remote by name, from the local settings; the shared strategy options stay.
	s = settingsFor(t, githubCkpt, `{"strategy_options": {"checkpoint_push_remote": "ckpt"}}`)
	isTrue(t, IsCheckpointRemote(s, "ckpt", "/srv/anything.git"))
	isFalse(t, IsCheckpointRemote(s, "origin", "/srv/anything.git"))
	isTrue(t, IsCheckpointRemote(s, "c", "https://github.com/Acme/ckpt.git"))
}

func TestIsCheckpointRemoteEmptyNameNeverMatchesWithoutPushRemote(t *testing.T) {
	s := settingsFor(t, githubCkpt, "")
	isFalse(t, IsCheckpointRemote(s, "", "/srv/x.git"))
}

func TestIsCheckpointRemoteAcceptsEveryURLFormOfTheHost(t *testing.T) {
	s := settingsFor(t, githubCkpt, "")
	for _, url := range []string{
		"http://github.com/Acme/ckpt.git",
		"https://GitHub.com/Acme/ckpt.git",
		"https://github.com:443/Acme/ckpt.git",
		"https://x-access-token:tok@github.com/Acme/ckpt.git",
		"github.com:Acme/ckpt.git",
		"ssh://github.com/Acme/ckpt",
		"ssh://git@github.com:22/Acme/ckpt.git",
		"ssh://git@github.com/Acme/ckpt/",
		"git://github.com/Acme/ckpt.git",
	} {
		isTrue(t, IsCheckpointRemote(s, "c", url), url)
	}
}

func TestIsCheckpointRemoteRefusesAnyOtherHost(t *testing.T) {
	s := settingsFor(t, githubCkpt, "")
	for _, url := range []string{
		"https://notgithub.com/Acme/ckpt.git",
		"git@notgithub.com:Acme/ckpt.git",
		"ssh://git@notgithub.com/Acme/ckpt.git",
		"https://github.com.evil.com/Acme/ckpt.git",
		"git@github.com.evil.com:Acme/ckpt.git",
		"https://evil.com/github.com/Acme/ckpt.git",
		"https://evil.com/github.com:Acme/ckpt.git",
		"https://evil.com/?x=github.com/Acme/ckpt",
		"https://evil.com/x#github.com/Acme/ckpt",
		"ssh://git@evil.com/github.com:Acme/ckpt.git",
		"https://github.com@evil.com/Acme/ckpt.git",
		"https://github.com:443@evil.com/Acme/ckpt.git",
		"https://evil.com\\@github.com/Acme/ckpt.git",
		"git@evil.com@github.com:Acme/ckpt.git",
		"https://github.com/acme/ckpt.git",
		"https://github.com/Acme/ckptx.git",
		"https://github.com/Other/Acme/ckpt.git",
		"https://github.com//Acme/ckpt.git",
		"https://github.com/Acme/ckpt.git?x=1",
		"https://github.com/Acme/ckpt.git#x",
		"git@github.com:/Acme/ckpt.git",
		"https://github.com/Acme/ckpt.git\n",
		"https://x @github.com/Acme/ckpt.git",
		"ftp://github.com/Acme/ckpt.git",
		"file:///github.com/Acme/ckpt.git",
		"github.com/Acme/ckpt.git",
		"./github.com:Acme/ckpt.git",
	} {
		isFalse(t, IsCheckpointRemote(s, "c", url), url)
	}
}

func TestIsCheckpointRemoteByProvider(t *testing.T) {
	gl := settingsFor(t, `{"strategy_options": {"checkpoint_remote": {"provider": "gitlab", "repo": "Acme/ckpt"}}}`, "")
	isTrue(t, IsCheckpointRemote(gl, "c", "git@gitlab.com:Acme/ckpt.git"))
	isFalse(t, IsCheckpointRemote(gl, "c", "git@github.com:Acme/ckpt.git"))

	other := settingsFor(t, `{"strategy_options": {"checkpoint_remote": {"provider": "forgejo", "repo": "Acme/ckpt"}}}`, "")
	isFalse(t, IsCheckpointRemote(other, "c", "https://forgejo.example/Acme/ckpt.git"))

	// A remote given as a URL, not a name, matches by URL; a 64-hex ref is irrelevant here.
	url := "https://github.com/CorneeldH/lighthouse-checkpoints.git"
	lh := settingsFor(t, `{"strategy_options": {"push_sessions": false, "checkpoint_remote": {"provider": "github", "repo": "CorneeldH/lighthouse-checkpoints"}}}`, "")
	isTrue(t, IsCheckpointRemote(lh, url, url))
	public := "https://github.com/CorneeldH/lighthouse.git"
	isFalse(t, IsCheckpointRemote(lh, public, public))
}

func TestEntireSelfPush(t *testing.T) {
	for name, c := range map[string]struct {
		shared, local string
		want          bool
	}{
		"no settings":       {"", "", false},
		"default on":        {`{"strategy_options": {}}`, "", true},
		"push sessions off": {`{"strategy_options": {"push_sessions": false}}`, "", false},
		"disabled":          {`{"enabled": false, "strategy_options": {}}`, "", false},
		"local turns on":    {`{"strategy_options": {"push_sessions": false}}`, `{"strategy_options": {"push_sessions": true}}`, true},
		"local disables":    {`{"strategy_options": {}}`, `{"enabled": false}`, false},
	} {
		eq(t, c.want, settingsFor(t, c.shared, c.local).SelfPush, name)
	}
}

func TestReadEntireSettingsBrokenJSONIsCouldNotCheck(t *testing.T) {
	_, err := ReadEntireSettings(entireDir(t, "{nope", ""))
	contains(t, cnc(t, err).Msg, "settings.json")

	_, err = ReadEntireSettings(entireDir(t, "[1]", ""))
	contains(t, cnc(t, err).Msg, "not a JSON object")
}

func TestReadEntireSettingsUnreadableDirIsCouldNotCheck(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permissions are not enforced here")
	}
	top := t.TempDir()
	dir := filepath.Join(top, ".entire")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := ReadEntireSettings(top)
	contains(t, cnc(t, err).Msg, "settings.json")
}
