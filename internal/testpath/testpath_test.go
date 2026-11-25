package testpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAbsIsAbsolute(t *testing.T) {
	if got := Abs("h", "x"); !filepath.IsAbs(got) {
		t.Fatalf("Abs(h, x) = %q, want an absolute path", got)
	}
}

func TestAbsRootWithoutElements(t *testing.T) {
	got := Abs()
	if got == "" || !filepath.IsAbs(got) {
		t.Fatalf("Abs() = %q, want a non-empty absolute root", got)
	}
}

func TestAbsCleans(t *testing.T) {
	if got, want := Abs("a", "..", "b"), Abs("b"); got != want {
		t.Fatalf("Abs(a, .., b) = %q, want %q", got, want)
	}
}

func TestSetHomeSetsBoth(t *testing.T) {
	dir := Abs("h")
	SetHome(t, dir)
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if got := os.Getenv(k); got != dir {
			t.Errorf("%s = %q, want %q", k, got, dir)
		}
	}
	got, err := os.UserHomeDir()
	if err != nil || got != dir {
		t.Errorf("UserHomeDir() = %q, %v; want %q", got, err, dir)
	}
}

func TestSetHomeEmptyClearsBoth(t *testing.T) {
	SetHome(t, "")
	for _, k := range []string{"HOME", "USERPROFILE"} {
		if got := os.Getenv(k); got != "" {
			t.Errorf("%s = %q, want empty", k, got)
		}
	}
}
