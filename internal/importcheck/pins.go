package importcheck

// DeptryVersion is the pinned deptry, the one place of the pin; the documentation names the tool
// without a version. Chosen 2026-11-20 as the latest stable release (0.25.1, uploaded to PyPI
// 2026-03-18) that is at least two weeks old. License: MIT, checked on the PyPI metadata of this
// release (license_expression MIT).
const DeptryVersion = "0.25.1"

// KnipVersion is the pinned knip, the one place of the pin; the documentation names the tool
// without a version. Chosen 2026-11-20 as the latest stable release (6.44.1, published to npm
// 2026-10-31) that is at least two weeks old. License: ISC, checked on the npm registry metadata
// of this release (`npm view knip license`).
const KnipVersion = "6.44.1" // lighthouse: allow-language — name of the tool
