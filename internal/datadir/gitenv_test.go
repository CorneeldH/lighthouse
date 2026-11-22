package datadir

// A go test that runs from a git hook inherits GIT_DIR; without this import the fixture repositories
// of the tests commit in the real repository (#463).
import _ "github.com/CorneeldH/lighthouse/internal/gitenvscrub"
