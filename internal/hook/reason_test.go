package hook

import (
	"reflect"
	"testing"
)

// isolateReasons gives a test an empty reason registry and restores the real one afterwards.
func isolateReasons(t *testing.T) {
	t.Helper()
	mu.Lock()
	saved := reasons
	reasons = map[string][]string{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		reasons = saved
		mu.Unlock()
	})
}

// mustPanic runs f and requires a panic whose message is exactly want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		got := recover()
		if got == nil {
			t.Errorf("expected a panic %q", want)
			return
		}
		if got != want {
			t.Errorf("panic %q, want %q", got, want)
		}
	}()
	f()
}

func TestBlockPrefixesTheTagOnTheFirstLine(t *testing.T) {
	isolateReasons(t)
	Declare("h", "x")
	got := Block(Reason{"h", "x"}, "msg\nmore\n")
	if got.Code != 2 || got.Stderr != "[h/x] msg\nmore\n" {
		t.Errorf("got code %d stderr %q", got.Code, got.Stderr)
	}
}

func TestBlockTaggedRequiresEveryTag(t *testing.T) {
	isolateReasons(t)
	rs := Declare("h", "a", "b")
	msg := "[h/a] one\n[h/b] two\n"
	got := BlockTagged(msg, rs...)
	if got.Code != 2 || got.Stderr != msg {
		t.Errorf("got code %d stderr %q", got.Code, got.Stderr)
	}
	mustPanic(t, "hook: message lacks [h/b]", func() { BlockTagged("[h/a] one\n", rs...) })
}

func TestCouldNotCheckRejectsAMessageWithoutTheToken(t *testing.T) {
	got := CouldNotCheck("trace: COULD NOT CHECK the thing\n")
	if got.Code != 2 || got.Stderr != "trace: COULD NOT CHECK the thing\n" {
		t.Errorf("got code %d stderr %q", got.Code, got.Stderr)
	}
	mustPanic(t, "hook: message lacks COULD NOT CHECK", func() { CouldNotCheck("could not check, lower case\n") })
}

func TestBlockRejectsUndeclaredReason(t *testing.T) {
	isolateReasons(t)
	Declare("h", "x")
	mustPanic(t, "hook: undeclared reason [h/y]", func() { Block(Reason{"h", "y"}, "msg\n") })
	mustPanic(t, "hook: undeclared reason [other/x]", func() { Block(Reason{"other", "x"}, "msg\n") })
}

func TestBlockTaggedRejectsUndeclaredReason(t *testing.T) {
	isolateReasons(t)
	Declare("h", "x")
	mustPanic(t, "hook: undeclared reason [h/y]", func() { BlockTagged("[h/y] msg\n", Reason{"h", "y"}) })
}

func TestDeclareRejectsDuplicates(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		pre  bool
		want string
	}{
		{"empty id", []string{"a", ""}, false, "hook: empty reason ID for h"},
		{"duplicate id", []string{"a", "a"}, false, "hook: duplicate reason h/a"},
		{"second declare for the same hook", []string{"a"}, true, "hook: reasons of h declared twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateReasons(t)
			if c.pre {
				Declare("h", "first")
			}
			mustPanic(t, c.want, func() { Declare("h", c.ids...) })
		})
	}
}

func TestReasonsKeepDeclarationOrder(t *testing.T) {
	isolateReasons(t)
	Declare("zeta", "c", "a", "b")
	Declare("alpha", "x")
	ids, ok := Reasons("zeta")
	if !ok || !reflect.DeepEqual(ids, []string{"c", "a", "b"}) {
		t.Errorf("got %v %v", ids, ok)
	}
	if _, ok := Reasons("none"); ok {
		t.Error("undeclared hook reported as declared")
	}
	if got := DeclaredHooks(); !reflect.DeepEqual(got, []string{"alpha", "zeta"}) {
		t.Errorf("got %v", got)
	}
	if tag := (Reason{"h", "x"}).Tag(); tag != "[h/x]" {
		t.Errorf("tag %q", tag)
	}
}
