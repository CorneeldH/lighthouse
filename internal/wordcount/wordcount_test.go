package wordcount

import (
	"errors"
	"testing"
)

// Red when Count counts lines or characters instead of words.
func TestCountMatchesWC(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"", 0},
		{"een twee\tdrie\n", 3},
		{"a\r\nb  c", 3},
		{"<!-- lighthouse:begin core -->\n", 4},
	} {
		got, err := Count([]byte(c.in))
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%q: got %d, want %d", c.in, got, c.want)
		}
	}
}

// Red when Count falls back on strings.Fields instead of returning ErrNoWC.
func TestCountWithoutWC(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Count([]byte("a b")); !errors.Is(err, ErrNoWC) {
		t.Fatalf("got %v, want ErrNoWC", err)
	}
}
