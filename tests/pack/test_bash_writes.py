"""Leeslaag en staat van `pack/core/lib/bash_writes.py` (#871, taak 5+6).

Taak 5: wortels, git-records (porcelain, diff-tree, reflog) en welke commits
een post moet controleren. Taak 6: handtekeningen, de sessiemap, de
gelockte ledger, het zaaien van een wortel, markers en `run_pre`. Zie
`docs/developer/planning/specs/2026-09-26-bash-write-gates-design.md`,
secties *Datastroom*, *Staat*, *Lock* en *pre*, en de bijbehorende
task-briefs onder `.superpowers/sdd/2026-09-26-bash-write-gates/`. Dispatch
en `post` komen in taak 7.
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
    """Een lighthouse-root met een stub voor elke gate, zodat `run_gate` er een vindt."""
    root = tmp_path / "lighthouse-root"
    hooks = root / ".claude" / "hooks" / "lighthouse"
    hooks.mkdir(parents=True)
    for gate in set(bw.GATES.values()):
        (hooks / gate).write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
        (hooks / gate).chmod(0o755)
    monkeypatch.setenv("LIGHTHOUSE_REPO_ROOT", str(root))
    return root


# --- GATES --------------------------------------------------------------


def test_gates_maps_known_extensions():
    assert bw.GATES[".sh"] == "shellcheck.sh"
    assert bw.GATES[".bash"] == "shellcheck.sh"
    assert bw.GATES[".py"] == "ruff-format.sh"
    assert bw.GATES[".md"] == "worktree-linkcheck.sh"
    assert bw.GATES[".markdown"] == "worktree-linkcheck.sh"


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
    # De bron van een rename is weg (nettofilter: verwijderd, niet ongecontroleerd);
    # de bron van een copy bestaat nog en wordt overgeslagen. Geen van beide is
    # ooit kandidaat.
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
    # De eerste entry van een nieuwe worktree heeft een leeg subject: die mag
    # de melding van een echte beweging erachter niet onderdrukken.
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
    # Commit op `feat`, dan terug naar main: de nieuwste entry heeft al de
    # sha van de ledger-`head`, de stop-wandeling ziet niets. De wandeling
    # sinds de start vindt c3 wel; checkout-regels tellen niet mee.
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
    # c3 zit in beide wandelingen: één keer.
    assert bw.commits_to_check(e, "b2", 10) == ([("c3", False), ("b2", False)], [])
    # Reflog-gat: de melding blijft, de wandeling sinds de start loopt toch.
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
    # `str.splitlines()` breekt ook op \x1c-\x1e, \x0c, \x85, U+2028/2029 — een
    # subject met zo'n teken mag het record niet uiteenrafelen. `.split("\n")`
    # wel.
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

    # Bewuste conflictmerge: de testkit-`git()` gebruikt `check=True` en zou
    # hier stuklopen op de niet-nul exit van een echt conflict.
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


# Regressie: `Step.dirs` is de LEVENDE mapstand van `walk` (buiten een
# sub-shell delen alle Steps hetzelfde dict-object). Een `list(walk(...))`
# die pas ná afloop `step.dirs["here"]` leest, ziet voor elke Step de
# EINDstand van de hele wandeling — hier steeds `other` — in plaats van de
# stand op het moment van die Step. Elke test eindigt daarom met een `cd`
# naar een map buiten alle repo's, zodat een niet-per-Step-lezende
# implementatie de eerdere wortel kwijtraakt.


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
    """Een worktree van een aparte clone met dezelfde `origin` hoort erbij (#36)."""
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
        # Een aanroeper die niets aan `unreadable` doet, mag de kapotte
        # ledger niet stilzwijgend vervangen door alleen `roots` te vullen.
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
    # Zonder `CLAUDE_PROJECT_DIR` keert `_run_pre` terug vóór `state_dir`
    # ooit wordt aangeroepen: er is geen sessiemap, dus ook geen marker.
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
    # `project_filter` gooit een `GitError` op de kapotte `project_dir` zelf,
    # vóórdat `_run_pre` `state_dir` aanroept: geen sessiemap, geen marker.
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

    # De kapotte bytes blijven ongewijzigd: pre mag `dirty.sh` niet ongezien
    # baseline maken door de ledger stilzwijgend te vervangen — dat is
    # precies wat `post`'s "Ledger onleesbaar"-terugval (taak 7) moet zien.
    assert ledger_path.read_bytes() == original
    with bw.locked_ledger(state) as ledger:
        assert ledger.unreadable is True
        assert ledger.roots == {}


def test_run_pre_does_not_seed_past_a_replaced_ledger(tmp_path, monkeypatch):
    # `ledger.reset`: post verving ooit een onleesbare ledger. Een wortel die
    # daarin ontbreekt, kan eerdere sessie-writes hebben; pre mag hem niet
    # als nullijn seeden, post valt voor hem terug.
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


# --- classify (taak 7) ---------------------------------------------------


@pytest.mark.parametrize(
    ("rc", "output", "verdict"),
    [
        (0, b"", "ok"),
        # Eén token voor elke gate, alleen op de eerste regel.
        (
            2,
            b"shellcheck-gate: COULD NOT CHECK \xe2\x80\x94 shellcheck-repo.sh gave rc=4\n",
            "unchecked",
        ),
        (2, b"ruff-format: COULD NOT CHECK \xe2\x80\x94 ruff is missing\n", "unchecked"),
        (2, b"Linkcheck: COULD NOT CHECK \xe2\x80\x94 jq\n", "unchecked"),
        (2, b"Linkcheck: markdown-link bestaat niet: x.md\n", "finding"),
        # ruff-format.sh: pass 1 staat vóór `Lint-gate:`, nooit een unchecked-vorm.
        (2, b"1 file left unchanged\nLint-gate: los dit op\n", "finding"),
        (124, b"shellcheck-gate: halverwege afgebroken\n", "unchecked"),
        (1, b"Linkcheck: markdown-link bestaat niet: x.md\n", "unchecked"),
    ],
)
def test_classify(rc, output, verdict):
    assert bw.classify(rc, output) == verdict


# --- run_post (taak 7) ---------------------------------------------------


def test_deadline_makes_progress_over_many_files(tmp_path, monkeypatch):
    repo = tmp_path / "repo"
    init_repo(repo)
    past_this_second()
    monkeypatch.setenv("CLAUDE_PROJECT_DIR", str(repo))
    monkeypatch.setenv("XDG_RUNTIME_DIR", str(tmp_path / "xdg"))
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_DEADLINE", raising=False)
    monkeypatch.delenv("LIGHTHOUSE_BASH_GATES_START_NS", raising=False)

    judged: list[str] = []
    fired = {"flag": False}
    start = time.time()

    def fake_gate(root, rel, deadline):
        judged.append(rel)
        fired["flag"] = True
        return "finding", f"SC2034 {rel}".encode()

    def fake_now():
        # Vóór de eerste gate van deze post: nog tijd. Erna: ver voorbij de
        # deadline. Zo laat elke post precies één gate toe.
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

    assert judged == ["a.sh", "b.sh", "c.sh"]
    codes = [code for code, _out, _err in results]
    assert codes == [2, 2, 2, 0], results
    first_err = results[0][2]
    assert "COULD NOT CHECK" in first_err
    assert "b.sh" in first_err and "c.sh" in first_err
    assert results[3][1:] == ("", "")


# --- run_post bron 2 (taak 8) ---------------------------------------------

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
    except OSError as exc:  # APFS weigert niet-UTF-8-namen (EILSEQ)
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
        lambda root, rel, deadline: dispatched.append(rel) or ("ok", b""),
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
        lambda root, rel, deadline: dispatched.append(rel) or ("ok", b""),
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
    # Alleen de klok van de dispatch springt voorbij de deadline; de
    # git-leesacties (`time.time`) houden hun volle 90 s, dus bron 2 loopt echt.
    repo = _post_env(tmp_path, monkeypatch)
    verdicts = {"x.sh": ("finding", b"SC2034 x.sh")}
    monkeypatch.setattr(bw, "run_gate", lambda root, rel, deadline: verdicts[rel])
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
    # Een parallelle post schoof al door naar H2: deze post (begon op H0) verliest.
    record["head"] = "H2"
    bw._advance(record, _pool("H0", "H1"))
    assert record["head"] == "H2"


def test_advance_merges_pending_and_keeps_what_another_post_added():
    record = {"seen": {}, "head": "H0", "pending": ["b.sh", "gone.sh"], "claims": {}}
    bw._advance(record, _pool("H0", "H1", keep=["a.sh"], settled=["gone.sh"]))
    assert record["pending"] == ["a.sh", "b.sh"]


def test_pending_rename_source_is_not_unchecked(tmp_path, monkeypatch):
    # a.sh komt via een gemiste deadline in `pending`, en wordt daarna gestaged
    # hernoemd: de bron is verwijderd, niet `COULD NOT CHECK`.
    repo = _post_env(tmp_path, monkeypatch)
    monkeypatch.setattr(bw, "run_gate", lambda root, rel, deadline: ("ok", b""))
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


# --- dispatch: verdwenen kandidaat, signaal; pre: marker vóór het zaaien ----


def _dirty_calls(repo, monkeypatch, names):
    """Pre, dan `names` vuil schrijven: de volgende post heeft ze als kandidaat."""
    assert bw.run_pre(_payload(repo, 0)) == 0
    for name in names:
        (repo / name).write_text("echo hoi\n", encoding="utf-8")


def test_vanished_candidate_is_not_dispatched_and_not_ok(tmp_path, monkeypatch):
    # x.sh staat in HEAD en verdwijnt tussen pool en gate: de gate zou `[ -f ]`
    # zien falen en exit 0 geven. Dat is geen oordeel.
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
        lambda root, rel, deadline: dispatched.append(rel) or ("ok", b""),
    )
    code, out, err = bw.run_post(_payload(repo, 0))
    assert dispatched == []
    assert code == 2, (out, err)
    assert "COULD NOT CHECK" in err and "x.sh" in err
    # y.sh stond nooit in HEAD: verwijderd, geen melding.
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
    # De gate eindigt zelf met 128 + n (bv. een gekilde shellcheck onder
    # `set -e`); de post kreeg geen signaal. Dat is een gewone niet-oordeel-
    # exit voor dat ene bestand: de overige kandidaten gaan wel door de gate.
    repo = _post_env(tmp_path, monkeypatch)
    _dirty_calls(repo, monkeypatch, ["a.sh", "b.sh", "c.sh"])
    started: list[str] = []

    def exits(deadline, argv, grace=10.0, **kwargs):
        payload = json.loads(kwargs["stdin_bytes"])
        started.append(os.path.basename(payload["tool_input"]["file_path"]))
        return rc, b""

    monkeypatch.setattr(bw, "run_until", exits)
    code, out, err = bw.run_post(_payload(repo, 0))
    assert started == ["a.sh", "b.sh", "c.sh"]
    assert code == 2, (out, err)
    assert "COULD NOT CHECK" in err
    assert "signal" not in err


def test_pre_writes_the_marker_before_seeding(tmp_path, monkeypatch):
    # Pre wordt op zijn timeout van 5 s gekild tijdens het zaaien: de marker
    # (met de starttijd voor bron 2) moet er dan al staan.
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


# --- lighthouse: één token, ontbrekende taalpakketten (besluit 18, 19) --------


def test_classify_first_line_only():
    """Een bevinding die op een latere regel `COULD NOT CHECK` citeert, blijft een bevinding."""
    output = b"SC2034: x appears unused\necho 'COULD NOT CHECK'\n"
    assert bw.classify(2, output) == "finding"


def test_gate_packs_name_every_gate():
    assert bw.GATE_PACKS == {
        "shellcheck.sh": "shell",
        "ruff-format.sh": "python",
        "gofmt.sh": "go",
        "worktree-linkcheck.sh": "core",
    }
    assert set(bw.GATE_PACKS) == set(bw.GATES.values())


def test_hook_dir_from_repo_root(gate_root):
    hooks = gate_root / ".claude" / "hooks" / "lighthouse"
    assert bw.gate_path(str(gate_root), ".sh") == hooks / "shellcheck.sh"
    assert bw.gate_path(str(gate_root), ".md") == hooks / "worktree-linkcheck.sh"
    assert bw.gate_path(str(gate_root), ".txt") is None


def test_go_gate_path(gate_root):
    hooks = gate_root / ".claude" / "hooks" / "lighthouse"
    assert bw.gate_path(str(gate_root), ".go") == hooks / "gofmt.sh"


def test_gofmt_signs_after_gate():
    assert "gofmt.sh" in bw._SIGN_AFTER_GATE


def test_run_gate_finds_the_gate_under_the_repo_root(tmp_path, gate_root):
    hooks = gate_root / ".claude" / "hooks" / "lighthouse"
    (hooks / "shellcheck.sh").write_text("#!/bin/sh\necho stub-ran\nexit 2\n", encoding="utf-8")
    repo = tmp_path / "repo"
    init_repo(repo)
    (repo / "a.sh").write_text("echo a\n", encoding="utf-8")
    verdict, output = bw.run_gate(str(repo), "a.sh", time.time() + 10)
    assert (verdict, output) == ("finding", b"stub-ran\n")


def test_missing_language_pack_is_reported_not_blocked(tmp_path, monkeypatch, gate_root):
    for gate in ("ruff-format.sh", "shellcheck.sh"):
        (gate_root / ".claude" / "hooks" / "lighthouse" / gate).unlink()
    repo = _post_env(tmp_path, monkeypatch)
    assert bw.run_gate(str(repo), "a.py", time.time() + 10) == (
        "missing",
        b"no python gate installed",
    )
    _dirty_calls(repo, monkeypatch, ["a.py", "b.sh"])
    code, out, err = bw.run_post(_payload(repo, 0))
    assert code == 0, (out, err)
    assert err == ""
    context = json.loads(out)["hookSpecificOutput"]["additionalContext"]
    assert "a.py not checked — no python gate installed" in context
    assert "b.sh not checked — no shell gate installed" in context
    # Eén melding per wijziging: de volgende call meldt ongewijzigde bestanden niet opnieuw.
    assert bw.run_pre(_payload(repo, 1)) == 0
    assert bw.run_post(_payload(repo, 1)) == (0, "", "")
