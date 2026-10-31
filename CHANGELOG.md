# Changelog

All notable changes to lighthouse. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); how versions are
chosen is in [docs/releasing.md](docs/releasing.md). The release workflow writes the sections below from the
fragments in `changelog.d/`; never edit a released section by hand.

## [v0.2.0] - 2026-10-31

### Added

- Every GitHub release now carries prebuilt binaries for Linux, macOS and Windows (amd64 and arm64 where available) with a `SHA256SUMS` file, so you can install lighthouse without Go.
- Releases are automatic: a merged fix ships as a patch release, everything else in the weekly minor release; the policy is in `docs/releasing.md`. `/lighthouse-verify-pr` writes a changelog fragment in repositories that opt in with `changelog.d/.lighthouse`.

### Changed

- Since v0.1.6: lighthouse's own code is in English and a code-language hook checks it, blocking Stop gates stay quiet while a valid usage alarm is set, and the test gate now runs after the commit and only on what the commit touches. `/lighthouse-write-issue` gained a path for updating an existing issue, the repository has a getting-started guide, contribution guidelines and a CLA check, and work started on a web UI that shows open questions, returned issues and the usage window (release binaries still serve a placeholder page).

### Fixed

- lighthouse builds on Windows again; `lighthouse serve` reports that its database lock is not supported there.

## Earlier versions

v0.1.0–v0.1.6: internal pre-releases.
