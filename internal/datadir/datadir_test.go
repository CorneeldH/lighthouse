package datadir

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func evalDir(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDataDir(t *testing.T) {
	tests := []struct {
		name string
		env  func(t *testing.T, tmp string) map[string]string
		want func(tmp string) string
	}{
		{
			name: "LIGHTHOUSE_DATA wins over XDG_DATA_HOME",
			env: func(_ *testing.T, tmp string) map[string]string {
				return map[string]string{"LIGHTHOUSE_DATA": filepath.Join(tmp, "records"), "XDG_DATA_HOME": filepath.Join(tmp, "x")}
			},
			want: func(tmp string) string { return filepath.Join(tmp, "records") },
		},
		{
			name: "absolute XDG_DATA_HOME",
			env: func(_ *testing.T, tmp string) map[string]string {
				return map[string]string{"XDG_DATA_HOME": tmp}
			},
			want: func(tmp string) string { return filepath.Join(tmp, "lighthouse", "review-records") },
		},
		{
			name: "no env falls back to HOME",
			env: func(t *testing.T, tmp string) map[string]string {
				t.Setenv("HOME", tmp)
				return map[string]string{}
			},
			want: func(tmp string) string { return filepath.Join(tmp, ".local/share/lighthouse/review-records") },
		},
		{
			name: "relative XDG_DATA_HOME is ignored",
			env: func(t *testing.T, tmp string) map[string]string {
				t.Setenv("HOME", tmp)
				return map[string]string{"XDG_DATA_HOME": "rel"}
			},
			want: func(tmp string) string { return filepath.Join(tmp, ".local/share/lighthouse/review-records") },
		},
		{
			name: "tilde in LIGHTHOUSE_DATA expands to HOME",
			env: func(t *testing.T, tmp string) map[string]string {
				t.Setenv("HOME", tmp)
				return map[string]string{"LIGHTHOUSE_DATA": "~/r"}
			},
			want: func(tmp string) string { return filepath.Join(tmp, "r") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := evalDir(t, t.TempDir())
			env := tt.env(t, tmp)
			got, err := Dir(func(k string) string { return env[k] })
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(tmp); got != want {
				t.Errorf("Dir = %q, want %q", got, want)
			}
			if info, err := os.Stat(got); err != nil || !info.IsDir() {
				t.Errorf("directory %q was not created (err %v)", got, err)
			}
		})
	}
}

func TestDataDirRefusesGitTree(t *testing.T) {
	repo := evalDir(t, t.TempDir())
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	target := filepath.Join(repo, "data")
	_, err := Dir(func(k string) string {
		if k == "LIGHTHOUSE_DATA" {
			return target
		}
		return ""
	})
	if !errors.Is(err, ErrInRepo) {
		t.Fatalf("err = %v, want ErrInRepo", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("%s was created inside the git tree (stat err %v)", target, statErr)
	}
}
