package lintcheck

import (
	"errors"
	"reflect"
	"testing"
)

const hookDir = ".claude/hooks/lighthouse/"

func TestFloorSubtractsNonLinterRules(t *testing.T) {
	root := writeRepo(t, map[string]string{
		hookDir + "h.sh": "#!/bin/sh\nBLOCKING_RULES=\"a,b,typecheck\"\nNON_LINTER_RULES=\"typecheck\"\n",
	})
	got, err := Floor(root, "h.sh")
	if err != nil || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestFloorMissingHookIsUnchecked(t *testing.T) {
	root := writeRepo(t, map[string]string{"x": ""})
	_, err := Floor(root, "h.sh")
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}

func TestFloorWithoutBlockingRulesIsUnchecked(t *testing.T) {
	root := writeRepo(t, map[string]string{hookDir + "h.sh": "#!/bin/sh\necho hi\n"})
	_, err := Floor(root, "h.sh")
	var u UncheckedError
	if !errors.As(err, &u) {
		t.Fatalf("want UncheckedError, got %v", err)
	}
}
