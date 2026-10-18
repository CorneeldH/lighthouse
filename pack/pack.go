// Package pack carries the lighthouse packages inside the binary (spec, besluit 2).
package pack

import "embed"

// Files holds every package directory: each directory under pack/ is a package. The
// pattern `*/*` takes what is inside those directories and leaves the files in pack/
// itself (this file, SOURCES.toml) out; a package directory that does not exist yet
// is no build error. `all:` keeps files that start with `_` or `.`, such as
// `__init__.py`; the installer skips caches like `__pycache__`.
//
//go:embed all:*/*
var Files embed.FS
