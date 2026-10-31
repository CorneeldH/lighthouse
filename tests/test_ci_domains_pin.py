"""Pins the shape of .github/workflows/ci.yml: one job per domain plus a `binary` job."""

import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
CI = ROOT / ".github" / "workflows" / "ci.yml"


def _text() -> str:
    return CI.read_text(encoding="utf-8")


def _jobs() -> dict[str, str]:
    """Cut ci.yml per job: the lines under `jobs:` with two spaces of indentation."""
    _, _, body = _text().partition("\njobs:\n")
    jobs: dict[str, list[str]] = {}
    current = None
    for line in body.splitlines():
        match = re.match(r"^  ([a-z][a-z0-9_-]*):\s*$", line)
        if match:
            current = match.group(1)
            jobs[current] = []
        elif current is not None:
            jobs[current].append(line)
    return {name: "\n".join(lines) for name, lines in jobs.items()}


def test_no_paths_filter():
    """Fails when a `paths:` filter returns to the `on:` block."""
    on_block = _text().split("\npermissions:")[0]
    assert "paths" not in on_block


def test_domain_jobs_wait_for_changes():
    """Fails when a domain job runs without consulting the `changes` job."""
    jobs = _jobs()
    for name in ("python", "go", "web"):
        assert "needs: changes" in jobs[name], name
        assert f"needs.changes.outputs.{name} == 'true'" in jobs[name], name


def test_changes_job_classifies():
    """Fails when the `changes` job stops classifying the diff with ci/domains.py."""
    changes = _jobs()["changes"]
    for needle in ("fetch-depth: 0", "--no-renames", " -z ", "ci/domains.py classify"):
        assert needle in changes, needle
    # A step line of its own (not the comment): without it `git diff | classify` has no pipefail.
    assert re.search(r"^        shell: bash\s*$", changes, re.MULTILINE)


def test_web_job_runs_every_check():
    """Fails when the web job drops a check or the generated-client drift test."""
    web = _jobs()["web"]
    for needle in (
        "node-version-file: web/.nvmrc",
        "npm ci",
        "npm run lint",
        "npm run typecheck",
        "npm test",
        "npm run gen",
        "git status --porcelain -- src/api",
        "npm run build",
    ):
        assert needle in web, needle


def test_node_jobs_cache_npm():
    """Fails when web or binary stops caching the npm download for npm ci."""
    jobs = _jobs()
    for name in ("web", "binary"):
        steps = re.split(r"(?m)^      - ", jobs[name])[1:]
        setup = [s for s in steps if "actions/setup-node" in s]
        assert len(setup) == 1, (name, setup)
        assert re.search(r"^\s+cache: npm\s*$", setup[0], re.MULTILINE), name
        assert "cache-dependency-path: web/package-lock.json" in setup[0], name


def test_binary_runs_without_web():
    """Fails when binary waits for web again, or stops building the SPA, the binary or the smoke."""
    binary = _jobs()["binary"]
    assert re.search(r"^    needs: changes\s*$", binary, re.MULTILINE)
    assert "needs.web" not in binary
    for needle in (
        "outputs.web == 'true'",
        "outputs.go == 'true'",
        "CGO_ENABLED=0 go build ./cmd/lighthouse",
        "ci/spa-smoke.sh",
    ):
        assert needle in binary, needle
    steps = re.split(r"(?m)^      - ", binary)[1:]
    build = [s for s in steps if re.search(r"run: npm run build\s*$", s, re.MULTILINE)]
    assert len(build) == 1 and not re.search(r"^\s*if:", build[0], re.MULTILINE), build
    assert binary.index("run: npm ci") < binary.index("run: npm run build") < binary.index("go build ./cmd/lighthouse")
    assert "web-dist" not in _text()


def _go_jobs() -> dict[str, str]:
    return {name: body for name, body in _jobs().items() if name == "go" or name.startswith("go-")}


def test_go_jobs_wait_for_changes():
    """Fails when a job split off from go runs without the go output of the changes job."""
    jobs = _go_jobs()
    assert len(jobs) >= 2, sorted(jobs)
    for name, body in jobs.items():
        assert re.search(r"^    needs: changes\s*$", body, re.MULTILINE), name
        assert "if: needs.changes.outputs.go == 'true'" in body, name


def test_go_checks_stay_in_go_jobs():
    """Fails when a Go check drops out of every go job while the go jobs are split."""
    go = "\n".join(_go_jobs().values())
    for needle in (
        "golangci/golangci-lint-action@",
        "- run: CGO_ENABLED=0 go build ./...",
        "- run: GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...",
        "- run: GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./...",
        "- run: go test -race -shuffle=on ./...",
        "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v",
        "name: installed pack/core matches pack/",
    ):
        assert needle in go, needle


def test_e2e_runs_apart_from_race_tests():
    """Fails when the e2e moves back into the job of the race tests, which made that job the slowest."""
    jobs = _go_jobs()
    race = [n for n, b in jobs.items() if "- run: go test -race -shuffle=on ./..." in b]
    e2e = [n for n, b in jobs.items() if "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v" in b]
    assert len(race) == 1 and len(e2e) == 1 and race != e2e, (race, e2e)
    assert "astral-sh/setup-uv@" in jobs[e2e[0]] and "astral-sh/setup-uv@" not in jobs[race[0]]


def test_no_measurement_step():
    """Fails when the temporary #131 measurement step stays in CI."""
    assert "-run '^$'" not in _text()


def test_actions_pinned_by_sha():
    """Fails when an action is referenced by tag, or loses its version comment."""
    uses = re.findall(r"^\s*(?:- )?uses: (\S+)(.*)$", _text(), re.MULTILINE)
    assert len(uses) == 18, uses
    for ref, rest in uses:
        assert re.search(r"@[0-9a-f]{40}$", ref), ref
        assert re.match(r"\s+# v\d+\.\d+\.\d+\s*$", rest), (ref, rest)


def test_changelog_job_runs_on_every_pr():
    """Fails when the fragment check starts depending on the domains of the change."""
    job = _jobs()["changelog"]
    assert "needs:" not in job and "if:" not in job
    for needle in ("runs-on: ubuntu-latest", "fetch-depth: 0", "python3 ci/changelog.py check --base \"$BASE\" --head \"$HEAD\""):
        assert needle in job, needle


def test_binary_runs_playwright():
    """Fails when binary stops running Playwright after the smoke, or Node/npm ci can be skipped."""
    binary = _jobs()["binary"]
    install = "npx playwright install --with-deps chromium"
    assert install in binary
    run = re.search(r"LIGHTHOUSE_BIN=\S+ npm run e2e", binary)
    assert run, "no LIGHTHOUSE_BIN=... npm run e2e step"
    assert binary.index("ci/spa-smoke.sh") < binary.index(install) < run.start()
    # Cut the steps: each starts with a `      - ` line.
    steps = re.split(r"(?m)^      - ", binary)[1:]
    setup = [s for s in steps if "actions/setup-node" in s]
    ci = [s for s in steps if re.search(r"run: npm ci\s*$", s, re.MULTILINE)]
    assert len(setup) == 1 and len(ci) == 1, (setup, ci)
    for step in setup + ci:
        assert not re.search(r"^\s*(- )?if:", step, re.MULTILINE), step
    assert binary.index("actions/setup-node") < binary.index("run: npm ci") < run.start()


def test_smoke_script_executable():
    """Fails when git stops tracking ci/spa-smoke.sh as executable."""
    out = subprocess.run(
        ["git", "ls-files", "-s", "ci/spa-smoke.sh"],
        cwd=ROOT, capture_output=True, text=True, check=True,
    ).stdout
    assert out.startswith("100755"), out
