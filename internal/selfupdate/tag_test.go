package selfupdate

import "testing"

func TestParseTagAcceptsChannelSuffixes(t *testing.T) {
	for _, ok := range []string{"v0.28.0", "v0.28.0-dev.1", "v0.28.0-rc.2"} {
		if _, err := ParseTag(ok); err != nil {
			t.Errorf("ParseTag(%q): unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"0.28.0", "v0.28.0-beta.1", "v0.28.0-test.3", "v0.28", "v0.28.0-dev.01"} {
		if _, err := ParseTag(bad); err == nil {
			t.Errorf("ParseTag(%q): expected error", bad)
		}
	}
}

func TestCompareTagsOrder(t *testing.T) {
	chain := []string{"v0.28.0-dev.5", "v0.28.0-rc.1", "v0.28.0", "v0.29.0-dev.1"}
	for i := 0; i+1 < len(chain); i++ {
		for _, c := range []struct {
			a, b string
			want int
		}{{chain[i], chain[i+1], -1}, {chain[i+1], chain[i], 1}, {chain[i], chain[i], 0}} {
			got, err := CompareTags(c.a, c.b)
			if err != nil || got != c.want {
				t.Errorf("CompareTags(%q, %q) = %d, %v; want %d", c.a, c.b, got, err, c.want)
			}
		}
	}
	if got, err := CompareTags("v0.28.0-dev.10", "v0.28.0-dev.9"); err != nil || got != 1 {
		t.Errorf("dev.10 vs dev.9 = %d, %v; want 1", got, err)
	}
	if _, err := CompareTags("junk", "v0.28.0"); err == nil {
		t.Error("expected error for invalid tag")
	}
}

func TestHighestTagSkipsInvalid(t *testing.T) {
	got, err := HighestTag([]string{"junk", "v0.27.0", "v0.28.0-dev.2", "v0.28.0-test.9"})
	if err != nil || got != "v0.28.0-dev.2" {
		t.Errorf("HighestTag = %q, %v; want v0.28.0-dev.2", got, err)
	}
	if _, err := HighestTag([]string{"junk"}); err == nil {
		t.Error("expected error when no valid tag")
	}
}
