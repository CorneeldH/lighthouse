"""Pins the shape of .github/workflows/release.yml and build.yml."""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
WORKFLOWS = ROOT / ".github" / "workflows"
RELEASE = WORKFLOWS / "release.yml"
BUILD = WORKFLOWS / "build.yml"
MACOS = WORKFLOWS / "macos.yml"


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
    for path in (RELEASE, BUILD, MACOS):
        uses = re.findall(r"^\s*(?:- )?uses:\s*(.+)$", _text(path), re.MULTILINE)
        assert uses, path.name
        for value in uses:
            if re.fullmatch(r"\./\.github/workflows/[\w-]+\.yml", value.strip()):
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
    for path in (RELEASE, BUILD, MACOS):
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
    for path in (RELEASE, BUILD, MACOS):
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
    for path in (RELEASE, BUILD, MACOS):
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


def _steps(job: str) -> list[str]:
    """Cut a job per step: each block starting at a `      - ` line (six spaces) under `steps:`."""
    _, _, body = job.partition("\n    steps:\n")
    steps: list[str] = []
    for line in body.splitlines():
        if line.startswith("      - "):
            steps.append(line)
        elif steps:
            steps[-1] += "\n" + line
    return steps


def _step_index(steps: list[str], name: str) -> int:
    """Index of the step named `name`; fails the test when there is none."""
    for i, step in enumerate(steps):
        if re.match(rf"^      - name: {re.escape(name)}\s*$", step.splitlines()[0]):
            return i
    raise AssertionError(f"no step named {name!r}")


def test_build_embeds_real_spa():
    """Fails when the binaries stop embedding the real SPA built by the `web` job."""
    jobs = _jobs(BUILD)
    assert "web" in jobs, "build.yml has no `web` job"
    web = jobs["web"]
    assert "node-version-file: web/.nvmrc" in web
    assert "npm ci" in web
    assert "npm run build" in web
    assert web.index("npm ci") < web.index("npm run build")
    wsteps = _steps(web)
    for command in ("npm ci", "npm run build"):
        pattern = rf"^      - run: {command}\s*$"
        step = next((x for x in wsteps if re.match(pattern, x.splitlines()[0])), None)
        assert step is not None, f"web has no step `run: {command}`"
        assert re.search(r"^\s+working-directory: web\s*$", step, re.MULTILINE), command
    upload = next((x for x in wsteps if "actions/upload-artifact@" in x), None)
    assert upload is not None, "web has no upload-artifact step"
    assert re.search(r"^\s+if-no-files-found: error\s*$", upload, re.MULTILINE)
    assert "name: spa-dist" in web
    assert "internal/web/dist" in web
    assert "contents: read" in web
    assert "contents: write" not in web

    build = jobs["build"]
    assert re.search(r"^    needs: \[resolve, web\]\s*$", build, re.MULTILINE)
    steps = _steps(build)
    download = next(
        (
            i
            for i, step in enumerate(steps)
            if "actions/download-artifact@" in step
            and "name: spa-dist" in step
            and re.search(r"^\s+path: internal/web/dist\s*$", step, re.MULTILINE)
        ),
        None,
    )
    assert download is not None, "build has no download of spa-dist into internal/web/dist"
    assert download < _step_index(steps, "Build")
    # checkout runs `git clean -ffdx`, which would delete the git-ignored internal/web/dist.
    checkout = next(i for i, step in enumerate(steps) if "actions/checkout@" in step)
    assert checkout < download


def test_build_runs_spa_smoke():
    """Fails when the linux/amd64 binary is no longer smoke-tested after `Check version`."""
    steps = _steps(_jobs(BUILD)["build"])
    version = _step_index(steps, "Check version")
    smoke = _step_index(steps, "Check SPA")
    assert version < smoke
    condition = "if: matrix.goos == 'linux' && matrix.goarch == 'amd64'"
    assert condition in steps[version]
    assert re.search(r"^\s+" + re.escape(condition) + r"\s*$", steps[smoke], re.MULTILINE)
    assert 'ci/spa-smoke.sh "$RUNNER_TEMP/dist/lighthouse_linux_amd64"' in steps[smoke]


def test_build_checkouts_pin_the_tag():
    """Fails when a checkout in build.yml does not check out `refs/tags/<resolved tag>`."""
    ref = "ref: refs/tags/${{ needs.resolve.outputs.tag }}"
    found = 0
    for name, job in _jobs(BUILD).items():
        for step in _steps(job):
            if "actions/checkout@" in step:
                found += 1
                assert re.search(r"^\s+" + re.escape(ref) + r"\s*$", step, re.MULTILINE), name
    assert found == 3


def test_macos_gate_triggers():
    """Fails when macos.yml runs on a PR, a push or a schedule, or can no longer be called or started by hand."""
    # Only the `on:` block: the header comment may name a push or a PR.
    on_block = _text(MACOS).split("\non:\n", 1)[1].split("\npermissions:")[0]
    assert "workflow_call" in on_block and "workflow_dispatch" in on_block
    for trigger in ("pull_request", "push", "schedule"):
        assert trigger not in on_block, trigger


def test_macos_gate_runs_the_suites_on_macos():
    """Fails when a suite drops out of the macOS gate or stops running on macos-latest."""
    jobs = _jobs(MACOS)
    assert sorted(jobs) == ["go-e2e", "go-test", "python"]
    for name, job in jobs.items():
        assert re.search(r"^    runs-on: macos-latest\s*$", job, re.MULTILINE), name
        assert "timeout-minutes:" in job, name
    python = jobs["python"]
    assert re.search(r"^        shard: \[1, 2, 3\]\s*$", python, re.MULTILINE)
    assert "LIGHTHOUSE_PYTEST_SHARD: ${{ matrix.shard }}/3" in python
    assert "uv run pytest -n auto" in python
    assert "actions/setup-go@" in python  # the lighthouse_bin fixture builds cmd/lighthouse
    assert "- run: go test -race -shuffle=on ./..." in jobs["go-test"]
    assert "- run: go test -race -shuffle=on -tags e2e ./e2e/ -v" in jobs["go-e2e"]
    assert "astral-sh/setup-uv@" in jobs["go-e2e"]
    assert "ci/literal-grep.sh" not in _text(MACOS)


def test_release_waits_for_macos():
    """Fails when the release can start without a green macOS run (#337)."""
    jobs = _jobs(RELEASE)
    assert "uses: ./.github/workflows/macos.yml" in jobs["macos"]
    assert re.search(r"^    needs: macos\s*$", jobs["release"], re.MULTILINE)


def _tested_commit_check(release: str) -> str:
    """The `if` block in the release loop that compares HEAD with the commit macOS tested."""
    head = '            if [ "$(git rev-parse HEAD)" != "$TESTED" ]; then\n'
    assert head in release, "no tested-commit check"
    start = release.index(head)
    end = release.index("\n            fi\n", start)
    return release[start:end]


def test_release_only_releases_the_tested_commit():
    """Fails when release can tag a commit macOS did not test, or turns red when main moved on."""
    release = _jobs(RELEASE)["release"]
    assert "TESTED: ${{ github.sha }}" in release
    block = _tested_commit_check(release)
    check = release.index(block)
    assert release.index("for attempt in 1 2 3") < check
    assert release.index("git reset --hard origin/main") < check
    assert check < release.index("ci/changelog.py release")
    # A push or cron run that was overtaken ends green: the run of the newer commit releases.
    assert re.search(r'^              echo "release: [^"]+"; exit 0$', block, re.MULTILINE), block


def test_overtaken_dispatch_or_rerun_fails():
    """Fails when a run started by hand, or a re-run, ends green without releasing because main
    moved on: a re-run can replace the pending run of the newer commit, so nobody would release."""
    release = _jobs(RELEASE)["release"]
    assert "ATTEMPT: ${{ github.run_attempt }}" in release
    block = _tested_commit_check(release)
    line = re.search(
        r'^              if \[ "\$EVENT" = workflow_dispatch \] \|\| \[ "\$ATTEMPT" != 1 \]; then echo "release: [^"]+" >&2; exit 1; fi$',
        block,
        re.MULTILINE,
    )
    assert line, block
    assert line.start() < block.index("exit 0")
