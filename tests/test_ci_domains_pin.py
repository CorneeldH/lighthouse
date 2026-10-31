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


def test_binary_builds_after_web():
    """Fails when the binary job stops waiting for web or stops building and smoking."""
    binary = _jobs()["binary"]
    needs = re.search(r"^    needs: (.*)$", binary, re.MULTILINE)
    assert needs and "web" in needs.group(1), needs
    for needle in (
        "!cancelled()",
        "outputs.go == 'true'",
        "needs.web.result != 'failure'",
        "CGO_ENABLED=0 go build ./cmd/lighthouse",
        "ci/spa-smoke.sh",
    ):
        assert needle in binary, needle


def test_actions_pinned_by_sha():
    """Fails when an action is referenced by tag, or loses its version comment."""
    uses = re.findall(r"^\s*(?:- )?uses: (\S+)(.*)$", _text(), re.MULTILINE)
    assert len(uses) == 16, uses
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
