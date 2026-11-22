// Package gitenvscrub removes the git variables that point a git command at another repository.
//
// A test package imports it for its side effect (`import _ ".../internal/gitenvscrub"`, in a
// `gitenv_test.go`). git sets GIT_DIR for a hook, so a `go test` that runs from a git hook inherits
// it; with GIT_DIR set, `git -C <fixture> commit` ignores the fixture and commits in the real
// repository. The init below runs before any test of the importing package, so no fixture can
// reach the real repository, whichever way the test was started.
//
// It is for tests only.
package gitenvscrub

import "os"

// Vars lists the variables that redirect git to a repository, an index, a work tree or an object
// store other than the one of the directory git runs in.
var Vars = []string{
	"GIT_DIR",
	"GIT_INDEX_FILE",
	"GIT_WORK_TREE",
	"GIT_COMMON_DIR",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_NAMESPACE",
	"GIT_PREFIX",
}

func init() { Scrub() }

// Scrub unsets every variable of Vars in the process environment.
func Scrub() {
	for _, name := range Vars {
		_ = os.Unsetenv(name)
	}
}
