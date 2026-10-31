"""Pins the shape of .github/workflows/release.yml and build.yml."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github" / "workflows"
RELEASE = WORKFLOWS / "release.yml"
BUILD = WORKFLOWS / "build.yml"


def _text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def _jobs(path: Path) -> dict[str, str]:
    """Cut a workflow per job: the lines under `jobs:` with two spaces of indentation."""
    _, _, body = _text(path).partition("\njobs:\n")
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


def _run_blocks(text: str) -> list[str]:
    """Every `run:` value in a workflow: the one-liner or the indented block under `run: |`."""
    blocks: list[str] = []
    lines = text.splitlines()
    for i, line in enumerate(lines):
        match = re.match(r"^(\s*)(?:- )?run:\s*(.*)$", line)
        if not match:
            continue
        indent = len(match.group(1)) + (2 if line.lstrip().startswith("- ") else 0)
        value = match.group(2)
        if value not in ("|", ">", "|-", ">-"):
            blocks.append(value)
            continue
        body = []
        for nxt in lines[i + 1 :]:
            if nxt.strip() and len(nxt) - len(nxt.lstrip()) <= indent:
                break
            body.append(nxt)
        blocks.append("\n".join(body))
    return blocks


def test_actions_pinned_by_sha():
    """Fails when a `uses:` is not pinned to a 40-hex SHA with a `# vX.Y.Z` comment."""
    for path in (RELEASE, BUILD):
        uses = re.findall(r"^\s*(?:- )?uses:\s*(.+)$", _text(path), re.MULTILINE)
        assert uses, path.name
        for value in uses:
            if value.strip() == "./.github/workflows/build.yml":
                continue
            assert re.fullmatch(r"[\w./-]+@[0-9a-f]{40} # v\d+\.\d+\.\d+", value.strip()), (
                path.name,
                value,
            )


def test_release_triggers_and_concurrency():
    """Fails when release.yml loses a trigger or lets runs cancel each other."""
    text = _text(RELEASE)
    for needle in (
        "branches: [main]",
        'cron: "0 6 * * 1"',
        "workflow_dispatch",
        "group: release-${{ github.event_name }}",
        "cancel-in-progress: false",
    ):
        assert needle in text, needle


def test_release_pushes_atomically():
    """Fails when the commit and the tag no longer land in one atomic push, with retries."""
    release = _jobs(RELEASE)["release"]
    assert "git push --atomic origin HEAD:main" in release
    assert "for attempt in 1 2 3" in release


def test_only_release_job_persists_credentials():
    """Fails when a second job keeps the token in .git/config, or a checkout lacks `false`."""
    total = 0
    for path in (RELEASE, BUILD):
        text = _text(path)
        for name, job in _jobs(path).items():
            count = job.count("persist-credentials: true")
            total += count
            if count:
                assert (path.name, name) == ("release.yml", "release")
        checkouts = len(re.findall(r"uses:\s*actions/checkout@", text))
        # release.yml's one pushing checkout is the sole exception.
        exempt = 1 if path == RELEASE else 0
        assert text.count("persist-credentials: false") == checkouts - exempt, path.name
    assert total == 1


def test_write_permission_only_where_needed():
    """Fails when `contents: write` appears outside release.release, release.build, build.publish."""
    allowed = {("release.yml", "release"), ("release.yml", "build"), ("build.yml", "publish")}
    found = set()
    for path in (RELEASE, BUILD):
        assert "contents: write" not in _text(path).split("\njobs:\n")[0], path.name
        for name, job in _jobs(path).items():
            if "contents: write" in job:
                found.add((path.name, name))
    assert found == allowed


def test_build_targets_and_flags():
    """Fails when a target, -trimpath, CGO_ENABLED=0 or the out-of-checkout output dir goes."""
    build = _jobs(BUILD)["build"]
    pairs = re.findall(r"goos:\s*(\w+)\s*\n\s*goarch:\s*(\w+)", build)
    assert sorted(pairs) == [
        ("darwin", "amd64"),
        ("darwin", "arm64"),
        ("linux", "amd64"),
        ("linux", "arm64"),
        ("windows", "amd64"),
    ]
    for needle in ("-trimpath", "CGO_ENABLED=0", '"$RUNNER_TEMP/dist/', "lighthouse_", ".exe"):
        assert needle in build, needle


def test_build_checks_version_and_clean_tree():
    """Fails when the binary is built from a dirty tree or its version is not checked."""
    build = _jobs(BUILD)["build"]
    assert 'test "$out" = "lighthouse $TAG"' in build
    assert "git status --porcelain" in build
    assert build.index("git status --porcelain") < build.index("go build")


def test_publish_uses_notes_file_and_clobber():
    """Fails when publish loses the notes file, the re-upload or the checksums."""
    publish = _jobs(BUILD)["publish"]
    for needle in (
        "ci/changelog.py notes",
        "--notes-file",
        "--clobber",
        "SHA256SUMS",
        "PRERELEASE",
        "Pre-release build of $TAG for testing; not a release.",
    ):
        assert needle in publish, needle
    # A normal tag without a section must fail; only the prerelease branch writes the fixed line.
    assert re.search(
        r'if \[ "\$PRERELEASE" = true \]; then\s*\n\s*echo "Pre-release build of \$TAG[^\n]*\n'
        r'\s*else\s*\n\s*echo "publish: no CHANGELOG\.md section for \$TAG" >&2; exit 1\s*\n\s*fi',
        publish,
    )


def test_no_inputs_in_run_scripts():
    """Fails when input or the ref name is interpolated into a run script instead of env."""
    for path in (RELEASE, BUILD):
        for block in _run_blocks(_text(path)):
            assert "${{" not in block, (path.name, block)


def test_every_step_job_has_a_runner():
    """Fails when a job with steps lacks `runs-on: ubuntu-latest`, or a `uses:` job gets one."""
    for path in (RELEASE, BUILD):
        for name, job in _jobs(path).items():
            if "\n    steps:" in job:
                assert "    runs-on: ubuntu-latest" in job, (path.name, name)
                assert "timeout-minutes:" in job, (path.name, name)
            else:
                assert "runs-on" not in job, (path.name, name)
    assert "uses: ./.github/workflows/build.yml" in _jobs(RELEASE)["build"]


def test_release_recovers_a_push_that_landed():
    """Fails when a push error after a server-side landing drops the tag and skips the build."""
    release = _jobs(RELEASE)["release"]
    check = 'git ls-remote origin "refs/tags/$tag"'
    assert check in release
    assert release.index(check) < release.index('git tag -d "$tag"')
    # Only our own tag object counts as landed; a rival run's tag of the same name must not.
    assert 'git rev-parse "refs/tags/$tag"' in release
    assert "sleep $((attempt * 5))" in release
