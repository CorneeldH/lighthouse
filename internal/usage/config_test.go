package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	def := Config{HaltPctSession: 85, HaltPctWeek: 95, TTL: 300 * time.Second}
	cases := []struct {
		name    string
		content *string // nil: no file
		path    *string // overrides the temp path
		want    Config
		wantErr bool
	}{
		{name: "no file", want: def},
		{name: "empty path", path: ptr(""), want: def},
		{name: "no usage table", content: ptr("[other]\nx = 1\n"), want: def},
		{name: "session set", content: ptr("[usage]\nhalt_pct_session = 70\n"), want: Config{70, 95, 300 * time.Second}},
		{name: "ttl zero", content: ptr("[usage]\ncache_ttl_seconds = 0\n"), want: Config{85, 95, 0}},
		{name: "all invalid", content: ptr("[usage]\nhalt_pct_session = 0\nhalt_pct_week = 101\ncache_ttl_seconds = -1\n"), want: def},
		{name: "per key fallback", content: ptr("[usage]\nhalt_pct_session = 70\nhalt_pct_week = 101\n"), want: Config{70, 95, 300 * time.Second}},
		{name: "parse error", content: ptr("[usage"), want: def, wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "host.toml")
			if c.content != nil {
				if err := os.WriteFile(path, []byte(*c.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if c.path != nil {
				path = *c.path
			}
			got, err := LoadConfig(path)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func ptr(s string) *string { return &s }
