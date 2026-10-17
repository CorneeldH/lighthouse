package version

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"":                                     "dev",
		"(devel)":                              "dev",
		"v0.0.0-20261017120000-abcdef123456":   "dev",
		"v0.1.1-0.20261017120000-abcdef123456": "dev",
		"v0.1.0+dirty":                         "dev",
		"v0.1.0":                               "v0.1.0",
		"v1.2.3-rc.1":                          "v1.2.3-rc.1",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
