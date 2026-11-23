package checkpointscan

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRemoteExclusionOnlyWherePushGoesToTheFetchURL(t *testing.T) {
	for name, c := range map[string]struct {
		setup    [][]string
		excluded bool
	}{
		"one url":      {nil, true},
		"same pushurl": {[][]string{{"--push", "ckpt", "{url}"}}, true},
		"pushurl":      {[][]string{{"--push", "ckpt", "/srv/other.git"}}, false},
		"two urls":     {[][]string{{"--add", "ckpt", "/srv/other.git"}}, false},
		"two pushurls": {[][]string{{"--add", "--push", "ckpt", "{url}"}, {"--add", "--push", "ckpt", "/srv/other.git"}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			repo := initRepo(t)
			url := filepath.Join(t.TempDir(), "ckpt.git")
			noErr(t, exec.Command("git", "init", "-q", "--bare", url).Run())
			_, err := git(repo, nil, "remote", "add", "ckpt", url)
			noErr(t, err)
			for _, args := range c.setup {
				for i, a := range args {
					if a == "{url}" {
						args[i] = url
					}
				}
				_, err := git(repo, nil, append([]string{"remote", "set-url"}, args...)...)
				noErr(t, err)
			}

			got, err := RemoteExclusion(repo, "ckpt")
			noErr(t, err)
			if c.excluded {
				eq(t, []string{"--remotes=ckpt"}, got)
			} else {
				empty(t, got)
			}
			// A push to a URL, not a configured remote, has no remote-tracking refs.
			byURL, err := RemoteExclusion(repo, url)
			noErr(t, err)
			empty(t, byURL)
		})
	}
}

func TestRemoteExclusionOutsideARepoIsCouldNotCheck(t *testing.T) {
	_, err := RemoteExclusion(t.TempDir(), "ckpt")
	_ = cnc(t, err)
}
