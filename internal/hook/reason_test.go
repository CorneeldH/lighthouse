package hook

import (
	"reflect"
	"strings"
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

func mustPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
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
	mustPanic(t, func() { BlockTagged("[h/a] one\n", rs...) })
}

func TestCouldNotCheckRejectsAMessageWithoutTheToken(t *testing.T) {
	got := CouldNotCheck("trace: COULD NOT CHECK the thing\n")
	if got.Code != 2 || got.Stderr != "trace: COULD NOT CHECK the thing\n" {
		t.Errorf("got code %d stderr %q", got.Code, got.Stderr)
	}
	mustPanic(t, func() { CouldNotCheck("could not check, lower case\n") })
}

func TestBlockRejectsUndeclaredReason(t *testing.T) {
	isolateReasons(t)
	Declare("h", "x")
	mustPanic(t, func() { Block(Reason{"h", "y"}, "msg\n") })
	mustPanic(t, func() { Block(Reason{"other", "x"}, "msg\n") })
}

func TestDeclareRejectsDuplicates(t *testing.T) {
	cases := []struct {
		name string
		ids  []string
		pre  bool
	}{
		{"empty id", []string{"a", ""}, false},
		{"duplicate id", []string{"a", "a"}, false},
		{"second declare for the same hook", []string{"a"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateReasons(t)
			if c.pre {
				Declare("h", "first")
			}
			mustPanic(t, func() { Declare("h", c.ids...) })
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
	if tag := (Reason{"h", "x"}).Tag(); !strings.EqualFold(tag, "[h/x]") {
		t.Errorf("tag %q", tag)
	}
}
