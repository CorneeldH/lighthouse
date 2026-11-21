package issuestate

import (
	"go/build"
	"slices"
	"testing"
)

// The tick (internal/dispatch) calls Return and SetLabel directly, so issuestate must never
// import dispatch or what only dispatch-side callers need: that would be an import cycle.
func TestImportsNoDispatchOrStore(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		"github.com/CorneeldH/lighthouse/internal/dispatch",
		"github.com/CorneeldH/lighthouse/internal/store",
	} {
		if slices.Contains(pkg.Imports, p) {
			t.Errorf("issuestate imports %s", p)
		}
	}
}
