// Package pack carries the lighthouse packages inside the binary (spec, besluit 2).
package pack

import "embed"

// Files holds every package directory. `all:` keeps files that start with `_`,
// such as `__init__.py`; the installer skips caches like `__pycache__`.
//
//go:embed all:core
var Files embed.FS
