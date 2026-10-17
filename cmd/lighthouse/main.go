// Command lighthouse installs the lighthouse packages into a repository.
package main

import (
	"os"

	"github.com/CorneeldH/lighthouse/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
