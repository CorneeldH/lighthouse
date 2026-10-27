# Contributing

lighthouse has one maintainer. Bug reports, questions and pull requests are welcome.
This document says where each one goes and what a pull request needs.

If you send a pull request, read [The CLA is required](#the-cla-is-required) first. It
is the one step that cannot be skipped.

## Where things go

| You have | Go to |
|---|---|
| a bug | a new issue, with the **Bug report** template |
| a question | a new issue, with the **Question** template |
| a suspected vulnerability | **not** an issue: see [SECURITY.md](SECURITY.md) |
| a change you want to make | an issue first for anything larger than a small fix, then a pull request |

## How the work in this repository is done

lighthouse is built with lighthouse. The maintainer's own work runs through the
development chain it ships: an issue, a git worktree on its own branch, a spec or plan
when the *how* is open, a test gate after every commit, and a pull request whose body
proves each acceptance criterion. The rules for the coding agents doing that work live in
[AGENTS.md](AGENTS.md) and [CLAUDE.md](CLAUDE.md), and the issues the chain writes itself
follow `/lighthouse-write-issue`, not the issue templates.

You do not have to work that way. The files for outside contributors — this one,
[SECURITY.md](SECURITY.md) and the issue templates — sit outside those chain rules. What a
pull request from outside does need is below.

## Sending a pull request

- One topic per branch. Open an issue first for anything larger than a small fix, so we
  can agree on the direction before you write the code.
- Run the test gate before you push; CI runs the same:

  ```bash
  uv run pytest -q && go test -race -shuffle=on ./...
  ```

- Go code is `gofmt`-formatted and passes `golangci-lint run` (the version is in
  [`.golangci-version`](.golangci-version)).
- A change under `pack/` also runs `go run ./cmd/lighthouse init --upgrade` and commits
  the installed copy; CI checks that the copy matches.
- Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat(scope): …`, `fix(scope): …`, `docs(scope): …`). Pull requests are
  squash-merged.
- Fill in the pull request template. `Related issues` names the issue
  (`Closes #N` or `Refs #N`), or says `No issue: <reason>`.

## The CLA is required

**Every contributor must sign the [Contributor License Agreement](CLA.md) before their
pull request can be merged.** There are no exceptions, including for one-line changes.

The reason is narrow and specific. lighthouse is licensed under the FSL-1.1-ALv2, and a
commercial license is available for uses the FSL does not permit. Without a CLA, what the
project receives from you is a licence under the repository's own terms — nothing more.
It carries no right to sublicense or to relicense. From the first outside merge onward,
every licence change and every commercial licence would need permission from every past
contributor, and a single unreachable contributor would block it permanently. The CLA
grants that right up front, while you keep your copyright.

Signing takes one commit: add your entry to
[`.github/cla-signatures.json`](.github/cla-signatures.json). The exact format and the two
ways to do it are in [section 11 of the CLA](CLA.md#11-how-to-sign). You may sign in the
same pull request as your change. A `CLA` check runs on every pull request; if an
identity in your commits is not covered, the check fails and its job summary names the
missing address.

## There is no DCO

You do not need `Signed-off-by:` trailers, and `git commit -s` is not required. The
CLA's own representations — that the work is your original creation, that you are
entitled to grant the licence, and the employer clause — cover what a Developer
Certificate of Origin certifies.

## Language

Files for outside readers are in English: `README.md`, this file, `SECURITY.md`, the
issue templates and the CLA. The maintainer's working artefacts — issues, pull requests,
specs and plans under `docs/developer/` — are in Dutch. An issue or pull request from you
may be in English.

## Licence

lighthouse is licensed under the [Functional Source License 1.1 with an Apache 2.0
future licence](LICENSE) (FSL-1.1-ALv2). It permits self-hosting, internal use, research
and teaching, and bans only competing use; each release converts to Apache-2.0 two years
after it ships. A contribution you make is licensed to the project under the terms of
the CLA.
