"""Read layer and state of `pack/core/lib/bash_writes.py` (#871, task 5+6).

Task 5: roots, git records (porcelain, diff-tree, reflog) and which commits
a post has to check. Task 6: signatures, the session directory, the
locked ledger, seeding a root, markers and `run_pre`. See
`docs/developer/planning/specs/2026-09-26-bash-write-gates-design.md`,
the sections on data flow, state, lock and pre, and the accompanying
task briefs under `.superpowers/sdd/2026-09-26-bash-write-gates/`. Dispatch
and `post` come in task 7.
"""

import json
import os
import stat
import subprocess
import threading
import time

import pytest

from tests.pack._testkit import (
    GIT_ENV,
    ORIGIN,
    forge_worktree,
    git,
    init_repo,
    load_hook_lib,
    past_this_second,
)

bw = load_hook_lib("bash_writes")
E = bw.ReflogEntry


@pytest.fixture(autouse=True)
def gate_root(tmp_path, monkeypatch):
    """A lighthouse root with a stub for every gate, so that `run_gate` finds one."""
    root = tmp_path / "lighthouse-root"
    hooks = root / ".claude" / "hooks" / "lighthouse"
    hooks.mkdir(parents=True)
    for gate in {g for gates in bw.GATES.values() for g in gates}:
        (hooks / gate).write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        (hooks / gate).chmod(0o755)
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(root))
    return root


# --- GATES --------------------------------------------------------------


def test_gates_maps_known_extensions():
    assert bw.GATES[".sh"] == ("shell-lint-check.sh", "code-language-check.sh")
    assert bw.GATES[".bash"] == ("shell-lint-check.sh", "code-language-check.sh")
    assert bw.GATES[".py"] == ("ruff-format-check.sh", "code-language-check.sh")
    assert bw.GATES[".go"] == ("gofmt-check.sh", "code-language-check.sh")
    assert bw.GATES[".md"] == ("md-link-check.sh",)
    assert bw.GATES[".markdown"] == ("md-link-check.sh",)


def test_rewriting_gate_runs_before_code_language():
    # code-language-check.sh must read the version after `ruff format`.
    for gates in bw.GATES.values():
        if "code-language-check.sh" in gates:
            last = gates.index("code-language-check.sh")
            assert not set(gates[last + 1 :]) & bw._SIGN_AFTER_GATE


# --- git() / GitError -----------------------------------------------------


def test_git_raises_giterror_on_nonzero_exit(tmp_path):
    outside = tmp_path / "not-a-repo"
    outside.mkdir()
    with pytest.raises(bw.GitError) as exc_info:
        bw.git(str(outside), "rev-parse", "--git-dir", deadline=time.time() + 5)
    assert exc_info.value.root == str(outside)
    assert exc_info.value.returncode not in (0, None)


def test_git_raises_giterror_on_timeout(tmp_path, monkeypatch):
    def fake_run(*args, **kwargs):
        raise subprocess.TimeoutExpired(cmd=args[0], timeout=kwargs.get("timeout", 0))

    monkeypatch.setattr(bw.subprocess, "run", fake_run)
    with pytest.raises(bw.GitError) as exc_info:
        bw.git(str(tmp_path), "status", deadline=time.time() + 5)
    assert exc_info.value.returncode is None
    assert "git read past the deadline" in str(exc_info.value)


# --- parse_porcelain / parse_diff_tree ------------------------------------


def test_porcelain_reports_the_old_path_of_a_rename_as_removed():
    # The source of a rename is gone (net filter: deleted, not unchecked);
    # the source of a copy still exists and is skipped. Neither is
    # ever a candidate.
    assert bw.parse_porcelain(
        b" M a.py\0R  new.sh\0old.sh\0C  cp.sh\0src.sh\0 D gone.sh\0?? sp ace.sh\0"
    ) == (
        {"a.py", "new.sh", "cp.sh", "sp ace.sh"},
        {"gone.sh", "old.sh"},
    )


def test_porcelain_keeps_newlines_and_non_ascii_in_names():
    assert bw.parse_porcelain("?? a\nb.sh\0?? ü.sh\0".encode()) == (
        {"a\nb.sh", "ü.sh"},
        set(),
    )


def test_diff_tree_combined_status_with_a_d_is_deleted():
    assert bw.parse_diff_tree(b"MM\0c.py\0DM\0x.sh\0A\0n.md\0") == (
        {"c.py", "n.md"},
        {"x.sh"},
    )


# --- parse_reflog / commits_to_check --------------------------------------


def test_reflog_stops_at_the_ledger_head_and_reports_a_checkout():
    e = [
        E("c3", 30, "commit: c"),
        E("b2", 20, "checkout: moving from x to main"),
        E("a1", 10, "commit (initial): a"),
    ]
    assert bw.commits_to_check(e, "b2", None) == ([("c3", False)], [])
    shas, notes = bw.commits_to_check(
        [E("b2", 20, "pull: Fast-forward"), *e[2:]], "a1", None
    )
    assert shas == [] and "without a commit" in notes[0]


def test_reflog_empty_subject_does_not_hide_a_later_movement():
    # The first entry of a new worktree has an empty subject: it must not
    # suppress the notice of a real movement behind it.
    e = [
        E("c3", 30, "commit: y"),
        E("e2", 20, ""),
        E("b1", 15, "checkout: moving from a to b"),
        E("s0", 10, "commit: base"),
    ]
    shas, notes = bw.commits_to_check(e, "s0", None)
    assert shas == [("c3", False)]
    assert len(notes) == 1 and "checkout: moving from a to b" in notes[0]
    assert bw.commits_to_check(
        [E("e2", 20, ""), E("s0", 10, "commit: base")], "s0", None
    ) == ([], [])


def test_reflog_without_the_ledger_head_is_skipped_with_a_note():
    entries = [E("c3", 30, "commit: c"), E("b2", 20, "commit: b")]
    shas, notes = bw.commits_to_check(entries, "gone", None)
    assert shas == []
    assert len(notes) == 1
    assert "gone" in notes[0]


def test_reflog_with_a_start_time_also_walks_past_a_return_to_the_stop():
    # Commit on `feat`, then back to main: the newest entry already has the
    # sha of the ledger `head`, the stop walk sees nothing. The walk
    # since the start does find c3; checkout lines do not count.
    e = [
        E("b2", 30, "checkout: moving from feat to main"),
        E("c3", 30, "commit: x"),
        E("b2", 29, "checkout: moving from main to feat"),
        E("a1", 10, "commit (initial): a"),
    ]
    assert bw.commits_to_check(e, "b2", None) == ([], [])
    assert bw.commits_to_check(e, "b2", 29) == ([("c3", False)], [])


def test_reflog_with_a_start_time_dedups_and_survives_a_gap():
    e = [E("c3", 30, "commit: c"), E("b2", 20, "commit: b"), E("a1", 5, "commit: a")]
    # c3 is in both walks: once.
    assert bw.commits_to_check(e, "b2", 10) == ([("c3", False), ("b2", False)], [])
    # Reflog gap: the notice stays, the walk since the start runs anyway.
    shas, notes = bw.commits_to_check(e, "gone", 10)
    assert shas == [("c3", False), ("b2", False)]
    assert len(notes) == 1 and "gone" in notes[0]


def test_reflog_from_unborn_counts_every_commit():
    entries = [
        E("c3", 30, "commit: c"),
        E("b2", 20, "checkout: moving from x to main"),
        E("a1", 10, "commit (initial): a"),
    ]
    shas, notes = bw.commits_to_check(entries, "unborn", None)
    assert shas == [("c3", False), ("a1", False)]
    assert notes == []


def test_reflog_fallback_counts_commits_since_the_start_time():
    entries = [
        E("c3", 30, "commit: c"),
        E("b2", 20, "checkout: moving from x to main"),
        E("a1", 10, "commit (initial): a"),
    ]
    shas, notes = bw.commits_to_check(entries, None, 15)
    assert shas == [("c3", False)]
    assert notes == []


def test_reflog_fallback_marks_a_merge_commit():
    entries = [E("m1", 30, "commit (merge): Merge branch 'x'")]
    shas, notes = bw.commits_to_check(entries, None, 0)
    assert shas == [("m1", True)]
    assert notes == []


def test_parse_reflog_reads_the_epoch_from_gd():
    text = "aaa\x1fHEAD@{100}\x1fcommit: a\nbbb\x1fHEAD@{50}\x1fcommit: b\n"
    assert bw.parse_reflog(text) == [
        E("aaa", 100, "commit: a"),
        E("bbb", 50, "commit: b"),
    ]


def test_parse_reflog_keeps_exotic_control_chars_in_the_subject():
    # `str.splitlines()` also breaks on \x1c-\x1e, \x0c, \x85, U+2028/2029 — a
    # subject with such a character must not unravel the record. `.split("\n")`
    # does not.
    subject = "a\x1cb   c\x0cd"
    text = f"aaa\x1fHEAD@{{5}}\x1f{subject}\n"
    assert bw.parse_reflog(text) == [E("aaa", 5, subject)]


# --- head_of ---------------------------------------------------------------


def test_head_of_unborn_and_broken(tmp_path):
    empty = tmp_path / "empty"
    empty.mkdir()
    git(empty, "init", "-q", "-b", "main")
    deadline = time.time() + 5
    assert bw.head_of(str(empty), deadline=deadline) == "unborn"

    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "HEAD").write_text("garbage\n", encoding="utf-8")
    with pytest.raises(bw.GitError):
        bw.head_of(str(broken), deadline=deadline)


def test_head_of_born_repo_returns_the_sha(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    deadline = time.time() + 5
    sha = bw.head_of(str(repo), deadline=deadline)
    assert len(sha) == 40 and all(ch in "0123456789abcdef" for ch in sha)


# --- diff_commit -------------------------------------------------------


def test_diff_commit_on_the_root_commit(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")
    (repo / "a.txt").write_text("hello\n", encoding="utf-8")
    git(repo, "add", "a.txt")
    git(repo, "commit", "-qm", "root")

    deadline = time.time() + 5
    sha = bw.head_of(str(repo), deadline=deadline)
    assert bw.diff_commit(str(repo), sha, False, deadline=deadline) == (
        {"a.txt"},
        set(),
    )


def test_diff_commit_on_a_real_conflict_merge_names_only_the_resolution(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "c.txt").write_text("main\n", encoding="utf-8")
    git(repo, "add", "c.txt")
    git(repo, "commit", "-qm", "c on main")

    git(repo, "checkout", "-qb", "side")
    (repo / "s.sh").write_text("echo side\n", encoding="utf-8")
    (repo / "c.txt").write_text("side\n", encoding="utf-8")
    git(repo, "add", "s.sh", "c.txt")
    git(repo, "commit", "-qm", "side work")

    git(repo, "checkout", "-q", "main")
    (repo / "c.txt").write_text("main again\n", encoding="utf-8")
    git(repo, "add", "c.txt")
    git(repo, "commit", "-qm", "main work")

    # Deliberate conflict merge: the testkit `git()` uses `check=True` and would
    # break here on the non-zero exit of a real conflict.
    subprocess.run(
        ["git", "-C", str(repo), "merge", "-q", "--no-ff", "side"],
        env=dict(os.environ, **GIT_ENV),
        check=False,
    )
    (repo / "c.txt").write_text("resolved\n", encoding="utf-8")
    git(repo, "add", "c.txt")
    git(repo, "commit", "-qm", "merge")

    deadline = time.time() + 5
    sha = bw.head_of(str(repo), deadline=deadline)
    assert bw.diff_commit(str(repo), sha, True, deadline=deadline) == ({"c.txt"}, set())


# --- derive_roots ------------------------------------------------------


def test_derive_roots_covers_cwd_cd_git_dash_c_and_literal_paths(tmp_path, monkeypatch):
    monkeypatch.setenv("HOME", str(tmp_path))
    a = tmp_path / "a"
    b = a / "b"
    c = a / "c"
    d = a / "d"
    e = tmp_path / "e"
    for repo in (a, b, c, d, e):
        init_repo(repo)

    command = (
        "(cd b && true); git -C c status; "
        "cat > d/x.sh <<'EOF'\necho x\nEOF\n"
        "cat > ~/e/z.sh <<'EOF'\necho x\nEOF"
    )
    roots = bw.derive_roots(command, str(a))
    assert roots == {os.path.realpath(str(p)) for p in (a, b, c, d, e)}


def test_derive_roots_finds_a_relative_gitignored_nested_worktree(tmp_path):
    main = tmp_path / "main"
    init_repo(main)
    (main / ".gitignore").write_text("wt/\n", encoding="utf-8")
    git(main, "add", ".gitignore")
    git(main, "commit", "-qm", "gitignore")
    git(main, "worktree", "add", "-q", "wt", "-b", "wt-branch")

    roots = bw.derive_roots("cat > wt/x.sh <<'EOF'\necho x\nEOF", str(main))
    assert os.path.realpath(str(main / "wt")) in roots


def test_derive_roots_on_an_open_quote_still_finds_an_absolute_path(tmp_path):
    a = tmp_path / "a"
    b = tmp_path / "b"
    init_repo(a)
    init_repo(b)

    command = f"echo 'open; cat > {b}/x.sh"
    roots = bw.derive_roots(command, str(a))
    assert os.path.realpath(str(b)) in roots


# Regression: `Step.dirs` is the LIVE directory state of `walk` (outside a
# sub-shell all Steps share the same dict object). A `list(walk(...))`
# that reads `step.dirs["here"]` only afterwards sees for every Step the
# FINAL state of the whole walk — here always `other` — instead of the
# state at the moment of that Step. Every test therefore ends with a `cd`
# to a directory outside all repos, so that an implementation that does not
# read per Step loses the earlier root.


def test_derive_roots_reads_the_dir_state_per_step_for_cd(tmp_path):
    a = tmp_path / "a"
    b = a / "b"
    other = tmp_path / "other"
    init_repo(a)
    init_repo(b)
    other.mkdir()

    command = f"cd b && echo hi > x.sh; cd {other}"
    roots = bw.derive_roots(command, str(a))
    assert os.path.realpath(str(b)) in roots


def test_derive_roots_reads_the_dir_state_per_step_for_a_relative_word(tmp_path):
    a = tmp_path / "a"
    d = a / "d"
    other = tmp_path / "other"
    init_repo(a)
    init_repo(d)
    other.mkdir()

    command = f"echo x > d/x.sh; cd {other}"
    roots = bw.derive_roots(command, str(a))
    assert os.path.realpath(str(d)) in roots


def test_derive_roots_reads_the_dir_state_per_step_for_git_dash_c(tmp_path):
    a = tmp_path / "a"
    d = a / "d"
    other = tmp_path / "other"
    init_repo(a)
    init_repo(d)
    other.mkdir()

    command = f"git -C d commit -m x; cd {other}"
    roots = bw.derive_roots(command, str(a))
    assert os.path.realpath(str(d)) in roots


# --- project_filter ------------------------------------------------------


def test_project_filter_drops_a_foreign_repo_and_flags_a_broken_one(tmp_path):
    main = tmp_path / "main"
    init_repo(main)
    git(main, "worktree", "add", "-q", "wt", "-b", "wt-branch")
    wt = main / "wt"

    foreign = tmp_path / "foreign"
    init_repo(foreign)

    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "HEAD").write_text("garbage\n", encoding="utf-8")

    roots = {
        os.path.realpath(str(main)),
        os.path.realpath(str(wt)),
        os.path.realpath(str(foreign)),
        os.path.realpath(str(broken)),
    }
    deadline = time.time() + 5
    kept, errors = bw.project_filter(roots, str(main), deadline=deadline)

    assert kept == {os.path.realpath(str(main)), os.path.realpath(str(wt))}
    assert len(errors) == 1
    assert errors[0].startswith(
        f"COULD NOT CHECK: {os.path.realpath(str(broken))}"
    )


def test_project_filter_keeps_a_forge_worktree_of_the_same_repo(tmp_path):
    """A worktree of a separate clone with the same `origin` belongs too (#36)."""
    main = tmp_path / "main"
    init_repo(main)
    git(main, "remote", "add", "origin", ORIGIN)
    work = os.path.realpath(str(forge_worktree(main, tmp_path)))

    kept, errors = bw.project_filter({work}, str(main), deadline=time.time() + 5)

    assert (kept, errors) == ({work}, [])


def test_project_filter_raises_when_project_dir_itself_is_broken(tmp_path):
    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "HEAD").write_text("garbage\n", encoding="utf-8")

    with pytest.raises(bw.GitError):
        bw.project_filter(set(), str(broken), deadline=time.time() + 5)


# --- signature -------------------------------------------------------------


def test_signature_is_none_for_a_missing_path(tmp_path):
    assert bw.signature(str(tmp_path / "nope.sh")) is None


def test_signature_is_four_ints_for_an_existing_file(tmp_path):
    f = tmp_path / "a.sh"
    f.write_text("echo hi\n", encoding="utf-8")
    sig = bw.signature(str(f))
    assert sig is not None
    assert len(sig) == 4
    assert all(isinstance(n, int) for n in sig)


# --- state_dir ---------------------------------------------------------


def test_state_dir_creates_the_dir_with_mode_0700(tmp_path, monkeypatch):
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    path = bw.state_dir("abc123")
    assert path.is_dir()
    assert stat.S_IMODE(path.stat().st_mode) == 0o700


def test_state_dir_rejects_a_traversal_session_id(tmp_path, monkeypatch):
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    with pytest.raises(ValueError):
        bw.state_dir("../evil")
    with pytest.raises(ValueError):
        bw.state_dir("..")


# --- marker_key --------------------------------------------------------


def test_marker_key_uses_the_tool_use_id_when_present():
    payload = {
        "tool_use_id": "toolu_abc",
        "session_id": "s",
        "tool_input": {"command": "ls"},
    }
    assert bw.marker_key(payload) == "toolu_abc"


def test_marker_key_falls_back_to_a_command_hash():
    assert bw.marker_key(
        {"session_id": "s", "tool_input": {"command": "ls"}}
    ).startswith("s-")


# --- write_marker / read_marker / prune_markers -----------------------------


def test_write_marker_then_read_marker_round_trips(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    bw.write_marker(state, "toolu_x", 123, ["/a", "/b"])
    assert bw.read_marker(state, "toolu_x") == {"start_ns": 123, "roots": ["/a", "/b"]}


def test_read_marker_missing_is_none(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    assert bw.read_marker(state, "nope") is None


def test_prune_removes_only_markers_older_than_an_hour(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    old = state / "old.json"
    fresh = state / "fresh.json"
    ledger_path = state / "ledger.json"
    lock_path = state / "ledger.lock"
    old.write_text("{}", encoding="utf-8")
    fresh.write_text("{}", encoding="utf-8")
    ledger_path.write_text("{}", encoding="utf-8")
    lock_path.write_text("", encoding="utf-8")

    now = time.time()
    old_time = now - 3700
    os.utime(old, (old_time, old_time))

    bw.prune_markers(state, now)

    assert not old.exists()
    assert fresh.exists()
    assert ledger_path.exists()
    assert lock_path.exists()


def test_prune_keeps_the_ledger_reset_sentinel(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    sentinel = state / "ledger.reset"
    sentinel.write_text("", encoding="utf-8")
    now = time.time()
    os.utime(sentinel, (now - 3700, now - 3700))
    bw.prune_markers(state, now)
    assert sentinel.exists()


# --- locked_ledger -----------------------------------------------------


def test_unreadable_ledger_is_flagged_not_fatal(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    (state / "ledger.json").write_text("{not valid json", encoding="utf-8")

    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        assert ledger.roots == {}


def test_missing_ledger_is_empty_and_readable(tmp_path):
    state = tmp_path / "state"
    state.mkdir()

    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is False
        assert ledger.roots == {}


def test_locked_ledger_persists_changes_across_calls(tmp_path):
    state = tmp_path / "state"
    state.mkdir()

    with bw.locked_ledger(state) as ledger:
        ledger.roots["/r"] = {"seen": {}, "head": "unborn", "pending": [], "claims": {}}

    with bw.locked_ledger(state) as ledger:
        assert ledger.roots == {
            "/r": {"seen": {}, "head": "unborn", "pending": [], "claims": {}}
        }


def test_locked_ledger_does_not_write_when_the_body_raises(tmp_path):
    state = tmp_path / "state"
    state.mkdir()

    def boom() -> None:
        with bw.locked_ledger(state) as ledger:
            ledger.roots["/r"] = {"seen": {}, "head": "x", "pending": [], "claims": {}}
            raise RuntimeError("boom")

    with pytest.raises(RuntimeError):
        boom()

    assert not (state / "ledger.json").exists()


def test_locked_ledger_does_not_write_back_an_unreadable_ledger(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    ledger_path = state / "ledger.json"
    original = b"{not valid json"
    ledger_path.write_bytes(original)

    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        # A caller that does nothing about `unreadable` must not silently replace the broken
        # ledger by filling only `roots`.
        ledger.roots["/r"] = {"seen": {}, "head": "x", "pending": [], "claims": {}}

    assert ledger_path.read_bytes() == original
    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        assert ledger.roots == {}


def test_locked_ledger_writes_when_unreadable_is_cleared(tmp_path):
    state = tmp_path / "state"
    state.mkdir()
    (state / "ledger.json").write_bytes(b"{not valid json")

    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        ledger.unreadable = False
        ledger.roots["/r"] = {"seen": {}, "head": "x", "pending": [], "claims": {}}

    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is False
        assert ledger.roots == {
            "/r": {"seen": {}, "head": "x", "pending": [], "claims": {}}
        }


def test_ledger_survives_parallel_writers(tmp_path):
    state = tmp_path / "state"
    state.mkdir()

    def worker(idx: int) -> None:
        key = f"w{idx}"
        for _ in range(20):
            with bw.locked_ledger(state) as ledger:
                bucket = ledger.roots.setdefault(key, {"count": 0})
                bucket["count"] += 1

    threads = [threading.Thread(target=worker, args=(i,)) for i in range(8)]
    for t in threads:
        t.start()
    for t in threads:
        t.join()

    with bw.locked_ledger(state) as ledger:
        assert len(ledger.roots) == 8
        assert all(v["count"] == 20 for v in ledger.roots.values())


# --- seed_root -----------------------------------------------------------


def test_seed_excludes_files_changed_at_or_after_the_pre_start(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "a.sh").write_text("echo a\n", encoding="utf-8")
    t0 = time.time_ns()
    (repo / "b.sh").write_text("echo b\n", encoding="utf-8")

    record = bw.seed_root(str(repo), t0, deadline=time.time() + 5)
    assert "a.sh" in record["seen"]
    assert "b.sh" not in record["seen"]
    assert record["pending"] == []
    assert record["claims"] == {}


def test_seed_ignores_extensions_outside_the_gates(tmp_path):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "a.sh").write_text("echo a\n", encoding="utf-8")
    (repo / "a.txt").write_text("plain\n", encoding="utf-8")
    t0 = time.time_ns()

    record = bw.seed_root(str(repo), t0, deadline=time.time() + 5)
    assert "a.sh" in record["seen"]
    assert "a.txt" not in record["seen"]


def test_seed_of_an_unborn_repo_has_head_unborn(tmp_path):
    repo = tmp_path / "repo"
    repo.mkdir()
    git(repo, "init", "-q", "-b", "main")

    record = bw.seed_root(str(repo), time.time_ns(), deadline=time.time() + 5)
    assert record == {"seen": {}, "head": "unborn", "pending": [], "claims": {}}


def test_seed_never_overwrites_an_existing_root(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "dirty.sh").write_text("echo dirty\n", encoding="utf-8")
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))

    root = os.path.realpath(str(repo))
    session_id = "sess-existing"
    state = bw.state_dir(session_id)
    sentinel = {
        "seen": {"sentinel.sh": [1, 2, 3, 4]},
        "head": "deadbeef",
        "pending": [],
        "claims": {},
    }
    with bw.locked_ledger(state) as ledger:
        ledger.roots[root] = sentinel

    payload = {
        "session_id": session_id,
        "tool_use_id": "toolu_1",
        "cwd": str(repo),
        "tool_input": {"command": "echo hi > x.sh"},
    }
    assert bw.run_pre(payload) == 0

    with bw.locked_ledger(state) as ledger:
        assert ledger.roots[root] == sentinel


# --- run_pre -------------------------------------------------------------


def test_run_pre_returns_zero_without_a_project_dir(tmp_path, monkeypatch):
    xdg = tmp_path / "xdg"
    monkeypatch.delenv("CLAUDE_PROJECT_DIR", raising=False)
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(xdg))
    payload = {
        "session_id": "s",
        "tool_use_id": "t1",
        "cwd": str(tmp_path),
        "tool_input": {"command": "echo hi"},
    }
    assert bw.run_pre(payload) == 0
    # Without `CLAUDE_PROJECT_DIR` `_run_pre` returns before `state_dir`
    # is ever called: there is no session directory, so no marker either.
    assert not (xdg / "lighthouse-bash-gates" / "s").exists()


def test_run_pre_returns_zero_on_a_broken_repo(tmp_path, monkeypatch):
    broken = tmp_path / "broken"
    init_repo(broken)
    (broken / ".git" / "HEAD").write_text("garbage\n", encoding="utf-8")

    xdg = tmp_path / "xdg"
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(broken))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(xdg))

    payload = {
        "session_id": "sess-broken",
        "tool_use_id": "toolu_broken",
        "cwd": str(broken),
        "tool_input": {"command": "echo hi > x.sh"},
    }
    assert bw.run_pre(payload) == 0
    # `project_filter` raises a `GitError` on the broken `project_dir` itself,
    # before `_run_pre` calls `state_dir`: no session directory, no marker.
    assert not (xdg / "lighthouse-bash-gates" / "sess-broken").exists()


def test_run_pre_does_not_overwrite_an_unreadable_ledger(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "dirty.sh").write_text("echo dirty\n", encoding="utf-8")
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))

    session_id = "sess-corrupt"
    state = bw.state_dir(session_id)
    ledger_path = state / "ledger.json"
    original = b"{corrupt"
    ledger_path.write_bytes(original)

    payload = {
        "session_id": session_id,
        "tool_use_id": "toolu_corrupt",
        "cwd": str(repo),
        "tool_input": {"command": "echo hi > x.sh"},
    }
    assert bw.run_pre(payload) == 0

    # The broken bytes stay unchanged: pre must not make `dirty.sh` unseen
    # baseline by silently replacing the ledger — that is
    # exactly what the "Ledger onleesbaar" fallback of `post` (task 7) has to see.
    assert ledger_path.read_bytes() == original
    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        assert ledger.roots == {}


def test_run_pre_does_not_seed_past_a_replaced_ledger(tmp_path, monkeypatch):
    # `ledger.reset`: post once replaced an unreadable ledger. A root that is
    # missing from it may have earlier session writes; pre must not seed it
    # as baseline, post falls back for it.
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "dirty.sh").write_text("echo dirty\n", encoding="utf-8")
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    session_id = "sess-reset"
    state = bw.state_dir(session_id)
    (state / "ledger.json").write_text("{}", encoding="utf-8")
    (state / "ledger.reset").write_text("", encoding="utf-8")
    payload = {
        "session_id": session_id,
        "tool_use_id": "toolu_reset",
        "cwd": str(repo),
        "tool_input": {"command": "true"},
    }
    assert bw.run_pre(payload) == 0
    with bw.locked_ledger(state) as ledger:
        assert os.path.realpath(repo) not in ledger.roots
    marker = bw.read_marker(state, "toolu_reset")
    assert marker is not None
    assert marker["roots"] == [os.path.realpath(repo)]


def test_run_pre_seeds_a_fresh_root_and_writes_a_marker(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))

    payload = {
        "session_id": "sess-fresh",
        "tool_use_id": "toolu_fresh",
        "cwd": str(repo),
        "tool_input": {"command": "echo hi > x.sh"},
    }
    assert bw.run_pre(payload) == 0

    state = bw.state_dir("sess-fresh")
    root = os.path.realpath(str(repo))
    with bw.locked_ledger(state) as ledger:
        assert root in ledger.roots
        assert ledger.roots[root]["head"] != ""

    marker = bw.read_marker(state, "toolu_fresh")
    assert marker is not None
    assert marker["roots"] == [root]
    assert isinstance(marker["start_ns"], int)


# --- classify (task 7) ---------------------------------------------------


@pytest.mark.parametrize(
    ("rc", "output", "verdict"),
    [
        (0, b"", "ok"),
        # One token for every gate, only on the first line.
        (
            2,
            b"shellcheck-gate: COULD NOT CHECK \xe2\x80\x94 shellcheck-repo.sh gave rc=4\n",
            "unchecked",
        ),
        (2, b"ruff-format-check: COULD NOT CHECK \xe2\x80\x94 ruff is missing\n", "unchecked"),
        (2, b"Linkcheck: COULD NOT CHECK \xe2\x80\x94 jq\n", "unchecked"),
        (2, b"Linkcheck: markdown-link bestaat niet: x.md\n", "finding"),
        # ruff-format-check.sh: pass 1 comes before `Lint-gate:`, never an unchecked form.
        (2, b"1 file left unchanged\nLint-gate: los dit op\n", "finding"),
        (124, b"shellcheck-gate: halverwege afgebroken\n", "unchecked"),
        (1, b"Linkcheck: markdown-link bestaat niet: x.md\n", "unchecked"),
    ],
)
def test_classify(rc, output, verdict):
    assert bw.classify(rc, output) == verdict


# --- run_post (task 7) ---------------------------------------------------


def test_deadline_makes_progress_over_many_files(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    past_this_second()
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_DEADLINE", raising=False)
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_START_NS", raising=False)

    judged: list[tuple[str, str]] = []
    fired = {"flag": False}
    start = time.time()

    def fake_gate(root, rel, gate, deadline):
        judged.append((rel, gate))
        fired["flag"] = True
        return "finding", f"SC2034 {rel}".encode()

    def fake_now():
        # Before the first gate of this post: time left. After it: far past the
        # deadline. The deadline is checked per file, so each post judges
        # exactly one file, with all of its gates.
        return start + 10**6 if fired["flag"] else start

    monkeypatch.setattr(bw, "run_gate", fake_gate)
    monkeypatch.setattr(bw, "_now", fake_now)

    def payload(n: int) -> dict:
        return {
            "session_id": "sess-many",
            "tool_use_id": f"toolu_many_{n}",
            "hook_event_name": "PostToolUse",
            "cwd": str(repo),
            "tool_input": {"command": "true"},
        }

    assert bw.run_pre(payload(0)) == 0
    for name in ("c.sh", "a.sh", "b.sh"):
        (repo / name).write_text("echo kapot\n", encoding="utf-8")

    results = []
    for n in range(4):
        fired["flag"] = False
        if n:
            assert bw.run_pre(payload(n)) == 0
        results.append(bw.run_post(payload(n)))

    assert judged == [
        ("a.sh", "shell-lint-check.sh"),
        ("a.sh", "code-language-check.sh"),
        ("b.sh", "shell-lint-check.sh"),
        ("b.sh", "code-language-check.sh"),
        ("c.sh", "shell-lint-check.sh"),
        ("c.sh", "code-language-check.sh"),
    ]
    codes = [code for code, _out, _err in results]
    assert codes == [2, 2, 2, 0], results
    first_err = results[0][2]
    assert "COULD NOT CHECK" in first_err
    assert "b.sh" in first_err and "c.sh" in first_err
    assert results[3][1:] == ("", "")


# --- run_post source 2 (task 8) ---------------------------------------------

BAD_NAME = os.fsdecode(b"bad\xff.sh")


def _post_env(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_DEADLINE", raising=False)
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_START_NS", raising=False)
    for name, value in GIT_ENV.items():
        monkeypatch.setenv(name, value)
    past_this_second()
    return repo


def _payload(repo, n: int) -> dict:
    return {
        "session_id": "sess-utf8",
        "tool_use_id": f"toolu_utf8_{n}",
        "hook_event_name": "PostToolUse",
        "cwd": str(repo),
        "tool_input": {"command": "true"},
    }


def _write_bad_name(repo) -> None:
    try:
        (repo / BAD_NAME).write_text("echo hoi\n", encoding="utf-8")
    except OSError as exc:  # APFS refuses non-UTF-8 names (EILSEQ)
        pytest.skip(f"bestandssysteem weigert een niet-UTF-8-naam: {exc}")


def _ledger_root(tmp_path, repo) -> dict:
    ledger = tmp_path / "xdg" / "lighthouse-bash-gates" / "sess-utf8" / "ledger.json"

    return json.loads(ledger.read_text(encoding="utf-8"))[os.path.realpath(repo)]


def test_non_utf8_name_is_unchecked_not_dispatched(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    dispatched: list[str] = []
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: dispatched.append(rel) or ("ok", b""),
    )
    assert bw.run_pre(_payload(repo, 0)) == 0
    _write_bad_name(repo)
    for n in (0, 1):
        if n:
            assert bw.run_pre(_payload(repo, n)) == 0
        code, out, err = bw.run_post(_payload(repo, n))
        assert code == 2, (out, err)
        assert "COULD NOT CHECK" in err
        assert "bad\\xff.sh" in err
        assert "COULD NOT CHECK" in out
    assert dispatched == []
    assert BAD_NAME not in _ledger_root(tmp_path, repo)["seen"]


def test_non_utf8_name_from_a_commit_stays_pending(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    dispatched: list[str] = []
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: dispatched.append(rel) or ("ok", b""),
    )
    assert bw.run_pre(_payload(repo, 0)) == 0
    _write_bad_name(repo)
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "bad name")
    code, _out, err = bw.run_post(_payload(repo, 0))
    assert code == 2, err
    assert "bad\\xff.sh" in err
    assert dispatched == []
    record = _ledger_root(tmp_path, repo)
    assert BAD_NAME in record["pending"]
    assert BAD_NAME not in record["seen"]


def test_failing_diff_tree_in_a_born_repo_is_unchecked(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)

    def broken_diff(root, sha, is_merge, *, deadline):
        raise bw.GitError(root, 128, "fatal: kapot")

    monkeypatch.setattr(bw, "diff_commit", broken_diff)
    assert bw.run_pre(_payload(repo, 0)) == 0
    (repo / "x.sh").write_text("echo hoi\n", encoding="utf-8")
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "x")
    code, _out, err = bw.run_post(_payload(repo, 0))
    assert code == 2, err
    assert f"COULD NOT CHECK: {os.path.realpath(repo)}" in err
    assert "fatal: kapot" in err


def test_missed_deadline_advances_head_and_keeps_pending(tmp_path, monkeypatch):
    # Only the clock of the dispatch jumps past the deadline; the
    # git read actions (`time.time`) keep their full 90 s, so source 2 really runs.
    repo = _post_env(tmp_path, monkeypatch)
    verdicts = {"x.sh": ("finding", b"SC2034 x.sh")}
    monkeypatch.setattr(bw, "run_gate", lambda root, rel, gate, deadline: verdicts[rel])
    assert bw.run_pre(_payload(repo, 0)) == 0
    (repo / "x.sh").write_text("echo kapot\n", encoding="utf-8")
    git(repo, "add", "x.sh")
    git(repo, "commit", "-qm", "x")
    head = subprocess.run(
        ["git", "-C", str(repo), "rev-parse", "HEAD"],
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()

    start = time.time()
    calls = {"n": 0}

    def late_now():
        calls["n"] += 1
        return start if calls["n"] == 1 else start + 10**6

    monkeypatch.setattr(bw, "_now", late_now)
    code, _out, err = bw.run_post(_payload(repo, 0))
    assert code == 2, err
    assert "COULD NOT CHECK" in err
    assert "SC2034" not in err
    record = _ledger_root(tmp_path, repo)
    assert record["head"] == head
    assert record["pending"] == ["x.sh"]

    monkeypatch.setattr(bw, "_now", time.time)
    assert bw.run_pre(_payload(repo, 1)) == 0
    code, _out, err = bw.run_post(_payload(repo, 1))
    assert code == 2, err
    assert "SC2034" in err
    assert _ledger_root(tmp_path, repo)["pending"] == []
    assert bw.run_pre(_payload(repo, 2)) == 0
    assert bw.run_post(_payload(repo, 2)) == (0, "", "")


def _pool(start_head, head, keep=(), settled=()):
    view = bw._RootView("/r", {}, False, head, start_head, [])
    return bw._Pool(view, [], [], list(keep), list(settled), [])


def test_advance_is_a_compare_and_swap_on_head():
    record = {"seen": {}, "head": "H0", "pending": [], "claims": {}}
    bw._advance(record, _pool("H0", "H1"))
    assert record["head"] == "H1"
    # A parallel post already moved on to H2: this post (began at H0) loses.
    record["head"] = "H2"
    bw._advance(record, _pool("H0", "H1"))
    assert record["head"] == "H2"


def test_advance_merges_pending_and_keeps_what_another_post_added():
    record = {"seen": {}, "head": "H0", "pending": ["b.sh", "gone.sh"], "claims": {}}
    bw._advance(record, _pool("H0", "H1", keep=["a.sh"], settled=["gone.sh"]))
    assert record["pending"] == ["a.sh", "b.sh"]


def test_pending_rename_source_is_not_unchecked(tmp_path, monkeypatch):
    # a.sh gets into `pending` through a missed deadline, and is then staged
    # renamed: the source is deleted, not `COULD NOT CHECK`.
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setattr(bw, "run_gate", lambda root, rel, gate, deadline: ("ok", b""))
    assert bw.run_pre(_payload(repo, 0)) == 0
    (repo / "a.sh").write_text("echo a\n", encoding="utf-8")
    git(repo, "add", "a.sh")
    git(repo, "commit", "-qm", "a")
    start = time.time()
    calls = {"n": 0}

    def late_now():
        calls["n"] += 1
        return start if calls["n"] == 1 else start + 10**6

    monkeypatch.setattr(bw, "_now", late_now)
    code, _out, err = bw.run_post(_payload(repo, 0))
    assert code == 2, err
    assert _ledger_root(tmp_path, repo)["pending"] == ["a.sh"]

    monkeypatch.setattr(bw, "_now", time.time)
    assert bw.run_pre(_payload(repo, 1)) == 0
    git(repo, "mv", "a.sh", "b.sh")
    code, out, err = bw.run_post(_payload(repo, 1))
    assert code == 0, err
    assert "COULD NOT" not in err and "COULD NOT" not in out
    assert _ledger_root(tmp_path, repo)["pending"] == []


# --- dispatch: vanished candidate, signal; pre: marker before the seeding ----


def _dirty_calls(repo, monkeypatch, names):
    """Pre, then write `names` dirty: the next post has them as a candidate."""
    assert bw.run_pre(_payload(repo, 0)) == 0
    for name in names:
        (repo / name).write_text("echo hoi\n", encoding="utf-8")


def test_vanished_candidate_is_not_dispatched_and_not_ok(tmp_path, monkeypatch):
    # x.sh is in HEAD and vanishes between pool and gate: the gate would see `[ -f ]`
    # fail and give exit 0. That is no verdict.
    repo = _post_env(tmp_path, monkeypatch)
    (repo / "x.sh").write_text("echo a\n", encoding="utf-8")
    git(repo, "add", "x.sh")
    git(repo, "commit", "-qm", "x")
    past_this_second()
    _dirty_calls(repo, monkeypatch, ["x.sh", "y.sh"])
    dispatched: list[str] = []
    real_claim = bw._claim

    def claim_then_vanish(state, key, pools):
        result = real_claim(state, key, pools)
        (repo / "x.sh").unlink()
        (repo / "y.sh").unlink()
        return result

    monkeypatch.setattr(bw, "_claim", claim_then_vanish)
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: dispatched.append(rel) or ("ok", b""),
    )
    code, out, err = bw.run_post(_payload(repo, 0))
    assert dispatched == []
    assert code == 2, (out, err)
    assert "COULD NOT CHECK" in err and "x.sh" in err
    # y.sh was never in HEAD: deleted, no notice.
    assert "y.sh" not in err
    record = _ledger_root(tmp_path, repo)
    assert "x.sh" not in record["seen"]
    assert record["claims"] == {}


def test_signal_during_a_gate_stops_the_dispatch_loudly(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    _dirty_calls(repo, monkeypatch, ["a.sh", "b.sh", "c.sh"])
    started: list[str] = []

    def killed(deadline, argv, grace=10.0, **kwargs):
        assert kwargs["raise_on_signal"] is True
        started.append(os.path.basename(kwargs["cwd"]))
        raise bw.Interrupted(15, b"")

    monkeypatch.setattr(bw, "run_until", killed)
    code, out, err = bw.run_post(_payload(repo, 0))
    assert len(started) == 1, started
    assert code == 2, (out, err)
    assert "COULD NOT CHECK" in err
    for name in ("a.sh", "b.sh", "c.sh"):
        assert name in err
    record = _ledger_root(tmp_path, repo)
    assert record["seen"] == {}
    assert record["claims"] == {}


@pytest.mark.parametrize("rc", [129, 130, 143])
def test_gate_exit_128_plus_n_does_not_stop_the_dispatch(tmp_path, monkeypatch, rc):
    # The gate itself ends with 128 + n (e.g. a killed shellcheck under
    # `set -e`); the post received no signal. That is an ordinary non-verdict
    # exit for that one file: the other candidates do go through the gate.
    repo = _post_env(tmp_path, monkeypatch)
    _dirty_calls(repo, monkeypatch, ["a.sh", "b.sh", "c.sh"])
    started: list[str] = []

    def exits(deadline, argv, grace=10.0, **kwargs):
        payload = json.loads(kwargs["stdin_bytes"])
        started.append(os.path.basename(payload["tool_input"]["file_path"]))
        return rc, b""

    monkeypatch.setattr(bw, "run_until", exits)
    code, out, err = bw.run_post(_payload(repo, 0))
    assert started == ["a.sh", "a.sh", "b.sh", "b.sh", "c.sh", "c.sh"]
    assert code == 2, (out, err)
    assert "COULD NOT CHECK" in err
    assert "signal" not in err


def test_pre_writes_the_marker_before_seeding(tmp_path, monkeypatch):
    # Pre is killed at its 5 s timeout during the seeding: the marker
    # (with the start time for source 2) must already be there then.
    repo = _post_env(tmp_path, monkeypatch)

    def killed_seed(root, start_ns, *, deadline):
        raise SystemExit(143)

    monkeypatch.setattr(bw, "seed_root", killed_seed)
    payload = _payload(repo, 0)
    with pytest.raises(SystemExit):
        bw.run_pre(payload)
    marker = bw.read_marker(bw.state_dir("sess-utf8"), bw.marker_key(payload))
    assert marker is not None
    assert marker["roots"] == [os.path.realpath(repo)]
    assert isinstance(marker["start_ns"], int)


def test_pre_stops_seeding_at_its_deadline(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    seeded: list[str] = []
    real_seed = bw.seed_root
    monkeypatch.setattr(
        bw,
        "seed_root",
        lambda root, start_ns, *, deadline: (
            seeded.append(root) or real_seed(root, start_ns, deadline=deadline)
        ),
    )
    monkeypatch.setattr(bw, "_PRE_DEADLINE_S", -1.0)
    payload = _payload(repo, 0)
    assert bw.run_pre(payload) == 0
    assert seeded == []
    assert bw.read_marker(bw.state_dir("sess-utf8"), bw.marker_key(payload))


# --- lighthouse: one token, missing language packs (decision 18, 19) --------


def test_classify_first_line_only():
    """A finding that quotes `COULD NOT CHECK` on a later line stays a finding."""
    output = b"SC2034: x appears unused\necho 'COULD NOT CHECK'\n"
    assert bw.classify(2, output) == "finding"


def test_gate_packs_name_every_gate():
    assert bw.GATE_PACKS == {
        "shell-lint-check.sh": "shell",
        "ruff-format-check.sh": "python",
        "gofmt-check.sh": "go",
        "md-link-check.sh": "core",
        "code-language-check.sh": "core",
    }
    assert set(bw.GATE_PACKS) == {g for gates in bw.GATES.values() for g in gates}


def test_hook_dir_from_repo_root(gate_root):
    hooks = gate_root / ".claude" / "hooks" / "lighthouse"
    assert bw.gate_path(str(gate_root), "shell-lint-check.sh") == hooks / "shell-lint-check.sh"
    assert bw.gate_path(str(gate_root), "code-language-check.sh") == hooks / "code-language-check.sh"


def test_gofmt_signs_after_gate():
    assert "gofmt-check.sh" in bw._SIGN_AFTER_GATE


def test_run_gate_finds_the_gate_under_the_repo_root(tmp_path, gate_root):
    hooks = gate_root / ".claude" / "hooks" / "lighthouse"
    (hooks / "shell-lint-check.sh").write_text("#!/bin/sh\necho stub-ran\nexit 2\n", encoding="utf-8")
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "a.sh").write_text("echo a\n", encoding="utf-8")
    verdict, output = bw.run_gate(str(repo), "a.sh", "shell-lint-check.sh", time.time() + 10)
    assert (verdict, output) == ("finding", b"stub-ran\n")


def test_missing_language_pack_is_reported_not_blocked(tmp_path, monkeypatch, gate_root):
    for gate in ("ruff-format-check.sh", "shell-lint-check.sh"):
        (gate_root / ".claude" / "hooks" / "lighthouse" / gate).unlink()
    repo = _post_env(tmp_path, monkeypatch)
    assert bw.run_gate(str(repo), "a.py", "ruff-format-check.sh", time.time() + 10) == (
        "missing",
        b"no python check installed",
    )
    _dirty_calls(repo, monkeypatch, ["a.py", "b.sh"])
    code, out, err = bw.run_post(_payload(repo, 0))
    assert code == 0, (out, err)
    assert err == ""
    context = json.loads(out)["hookSpecificOutput"]["additionalContext"]
    assert "a.py not checked — no python check installed" in context
    assert "b.sh not checked — no shell check installed" in context
    # One notice per change: the next call does not report unchanged files again.
    assert bw.run_pre(_payload(repo, 1)) == 0
    assert bw.run_post(_payload(repo, 1)) == (0, "", "")


# --- dispatch: several gates per extension (#26) -----------------------------


def test_dispatch_runs_every_gate_in_order(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setitem(bw.GATES, ".sh", ("first.sh", "second.sh"))
    ran: list[tuple[str, str]] = []

    def fake_gate(root, rel, gate, deadline):
        ran.append((rel, gate))
        return "finding", f"{gate} says no".encode()

    monkeypatch.setattr(bw, "run_gate", fake_gate)
    _dirty_calls(repo, monkeypatch, ["x.sh"])
    code, out, err = bw.run_post(_payload(repo, 0))
    assert ran == [("x.sh", "first.sh"), ("x.sh", "second.sh")]
    assert code == 2, (out, err)
    first = err.index("== x.sh (first.sh) ==\nfirst.sh says no")
    second = err.index("== x.sh (second.sh) ==\nsecond.sh says no")
    assert first < second


@pytest.mark.parametrize(
    ("verdicts", "judged"),
    [
        (("unchecked", "ok"), False),
        (("ok", "unchecked"), False),
        (("finding", "unchecked"), False),
        (("ok", "ok"), True),
        (("ok", "finding"), True),
        (("missing", "ok"), True),
        (("finding", "missing"), True),
    ],
)
def test_dispatch_combined_verdict(tmp_path, monkeypatch, verdicts, judged):
    # x.sh comes from a commit (source 2), so it is in `pending` until it has a
    # real verdict from every gate.
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setitem(bw.GATES, ".sh", ("first.sh", "second.sh"))
    by_gate = dict(zip(("first.sh", "second.sh"), verdicts, strict=True))
    monkeypatch.setattr(
        bw, "run_gate", lambda root, rel, gate, deadline: (by_gate[gate], b"")
    )
    assert bw.run_pre(_payload(repo, 0)) == 0
    (repo / "x.sh").write_text("echo x\n", encoding="utf-8")
    git(repo, "add", "x.sh")
    git(repo, "commit", "-qm", "x")
    bw.run_post(_payload(repo, 0))
    record = _ledger_root(tmp_path, repo)
    if judged:
        assert record["pending"] == []
        assert "x.sh" in record["seen"]
    else:
        assert record["pending"] == ["x.sh"]
        assert "x.sh" not in record["seen"]
    assert record["claims"] == {}


@pytest.mark.parametrize(
    ("gates", "signed_after"),
    [
        # The rewriting gate's verdict holds for the version it wrote; a write
        # during the read-only gate after it stays new.
        (("ruff-format-check.sh", "code-language-check.sh"), "ruff-format-check.sh"),
        # No rewriting gate: the version from just before the first gate.
        (("shell-lint-check.sh", "code-language-check.sh"), None),
    ],
)
def test_dispatch_signature_after_rewriting_gate(tmp_path, monkeypatch, gates, signed_after):
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setitem(bw.GATES, ".sh", gates)
    path = repo / "x.sh"
    sigs: dict[str, list[int]] = {}

    def rewriting_gate(root, rel, gate, deadline):
        # Every gate writes the file (a growing size, so each version has its
        # own signature), as ruff format does, or a concurrent write would.
        with path.open("a", encoding="utf-8") as handle:
            handle.write(f"# {gate}\n")
        sigs[gate] = list(bw.signature(str(path)))
        return "ok", b""

    monkeypatch.setattr(bw, "run_gate", rewriting_gate)
    _dirty_calls(repo, monkeypatch, ["x.sh"])
    before = list(bw.signature(str(path)))
    assert bw.run_post(_payload(repo, 0)) == (0, "", "")
    seen = _ledger_root(tmp_path, repo)["seen"]["x.sh"]
    assert seen == (sigs[signed_after] if signed_after else before)


# --- the code-language notice on the Bash path (#37, decision 19) -------------


def test_run_gate_sets_under_gates_env(tmp_path, monkeypatch, gate_root):
    monkeypatch.delenv("LIGHTHOUSE_UNDER_BASH_WRITE_CHECK", raising=False)
    gate = gate_root / ".claude" / "hooks" / "lighthouse" / "code-language-check.sh"
    gate.write_text('#!/bin/sh\necho "$LIGHTHOUSE_UNDER_BASH_WRITE_CHECK"\n', encoding="utf-8")
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "a.py").write_text("x = 1\n", encoding="utf-8")
    assert bw.run_gate(str(repo), "a.py", "code-language-check.sh", time.time() + 10) == ("ok", b"1\n")
    assert "LIGHTHOUSE_UNDER_BASH_WRITE_CHECK" not in os.environ


def _gate_output(monkeypatch, output: bytes) -> None:
    """`code-language-check.sh` ends with rc 0 and `output`; every other gate is silent."""
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: ("ok", output if gate == "code-language-check.sh" else b""),
    )


def test_notice_on_rc0_is_passed_on(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    _gate_output(monkeypatch, b"code-language-check: NOTICE \xe2\x80\x94 x\na.py:2: name: y")
    _dirty_calls(repo, monkeypatch, ["x.py"])
    code, out, err = bw.run_post(_payload(repo, 0))
    assert (code, err) == (0, "")
    data = json.loads(out)
    for text in (data["hookSpecificOutput"]["additionalContext"], data["systemMessage"]):
        assert "== x.py (code-language-check.sh) ==\ncode-language-check: NOTICE — x\na.py:2: name: y" in text


def test_notice_beside_a_block_also_reaches_the_user(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    verdicts = {
        "a.py": ("ok", b"code-language-check: NOTICE \xe2\x80\x94 x"),
        "b.sh": ("finding", b"SC2034 b.sh"),
    }
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: verdicts[rel] if gate == "code-language-check.sh" else ("ok", b""),
    )
    _dirty_calls(repo, monkeypatch, ["a.py", "b.sh"])
    code, out, err = bw.run_post(_payload(repo, 0))
    assert code == 2
    data = json.loads(out)
    assert "NOTICE — x" in data["systemMessage"]
    assert "SC2034" not in data["systemMessage"]
    assert "SC2034" in err and "NOTICE — x" in err


def test_plain_note_has_no_system_message(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setattr(
        bw,
        "run_gate",
        lambda root, rel, gate, deadline: (bw._MISSING, b"no python check installed")
        if gate == "ruff-format-check.sh"
        else ("ok", b""),
    )
    _dirty_calls(repo, monkeypatch, ["a.py"])
    code, out, _err = bw.run_post(_payload(repo, 0))
    assert code == 0
    data = json.loads(out)
    assert "a.py not checked — no python check installed" in data["hookSpecificOutput"]["additionalContext"]
    assert "systemMessage" not in data


def test_chatter_on_rc0_stays_silent(tmp_path, monkeypatch):
    repo = _post_env(tmp_path, monkeypatch)
    _gate_output(monkeypatch, b"formatted 1 file")
    _dirty_calls(repo, monkeypatch, ["x.py"])
    assert bw.run_post(_payload(repo, 0)) == (0, "", "")


def test_under_gates_env_matches():
    cl = load_hook_lib("code_language")
    assert bw.UNDER_GATES_ENV == cl.UNDER_GATES_ENV == "LIGHTHOUSE_UNDER_BASH_WRITE_CHECK"
