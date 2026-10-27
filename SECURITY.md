# Security Policy

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability. Report it privately by email
to corneel@cefero.nl.

The repository is private today, so GitHub's private vulnerability reporting is not
available. Once the repository is public it will be switched on and become the
preferred route; email stays as the fallback, and this document will say so.

Please include what you found, how to reproduce it, the output of `lighthouse version`,
and your operating system and harness (Claude Code, Codex or OpenCode).

Expect a first response within seven days. There is no bug bounty and no SLA beyond
that: one maintainer, best effort.

## Scope

In scope:

- the `lighthouse` binary: `init`, `usage` and `serve`;
- what `lighthouse init` installs into a repository: hooks, skills, agents, helper
  scripts and the managed block in `AGENTS.md`;
- the CI workflows of this repository.

The hooks run shell and Python inside your repository with your own permissions, on
every tool call of your coding agent. A finding where a hook runs something it should
not, writes outside the repository, or can be steered by file content it reads is in
scope.

## Supported versions

There is no release policy yet. Only the newest tag and the tip of `main` receive
fixes.

## What is deliberately not hardened

`lighthouse serve` listens on `127.0.0.1:8127` and has no authentication layer. That is
a design choice for a single user on their own machine or private network. Binding it to
a public interface with `--addr` is out of scope for this policy.
