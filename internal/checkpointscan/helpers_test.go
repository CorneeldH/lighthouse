package checkpointscan

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func msg(args []any) string {
	if len(args) == 0 {
		return ""
	}
	return " (" + fmt.Sprint(args...) + ")"
}

func eq(t *testing.T, want, got any, note ...any) {
	t.Helper()
	if !reflect.DeepEqual(want, got) {
		t.Errorf("got %#v, want %#v%s", got, want, msg(note))
	}
}

func ne(t *testing.T, a, b any, note ...any) {
	t.Helper()
	if reflect.DeepEqual(a, b) {
		t.Errorf("both are %#v%s", a, msg(note))
	}
}

func isTrue(t *testing.T, got bool, note ...any) {
	t.Helper()
	if !got {
		t.Errorf("got false, want true%s", msg(note))
	}
}

func isFalse(t *testing.T, got bool, note ...any) {
	t.Helper()
	if got {
		t.Errorf("got true, want false%s", msg(note))
	}
}

func contains(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("%q does not contain %q", s, sub)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// cnc fails unless err is a CouldNotCheck, and returns it.
func cnc(t *testing.T, err error) CouldNotCheck {
	t.Helper()
	var c CouldNotCheck
	if !errors.As(err, &c) {
		t.Fatalf("error = %v, want a CouldNotCheck", err)
	}
	return c
}

func empty[T any](t *testing.T, v []T) {
	t.Helper()
	if len(v) != 0 {
		t.Errorf("got %v, want empty", v)
	}
}
