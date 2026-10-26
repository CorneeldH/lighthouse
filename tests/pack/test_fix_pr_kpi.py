"""Unit tests for `.lighthouse/scripts/measure_fix_pr_kpi.py`, taken from paia.

The KPI of #664 is one number: how many tool calls sit between "fix 596" and the first action
that actually changes the tree. That number is only worth something if three things hold, and
exactly those three are pinned here:

- the prompt recognition is narrow enough (a mentioned PR number is not a fix request);
- "mutating" is broader than Write/Edit — 6 of the 8 measured turns mutated via Bash;
- the scope row sees the case it exists for: the skill that runs on into close-issue.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

from tests.pack._testkit import INSTALL_ROOT

SCRIPT = INSTALL_ROOT / ".lighthouse" / "scripts" / "measure_fix_pr_kpi.py"
_SPEC = importlib.util.spec_from_file_location("measure_fix_pr_kpi", SCRIPT)
assert _SPEC and _SPEC.loader
kpi = importlib.util.module_from_spec(_SPEC)
sys.modules[_SPEC.name] = kpi
_SPEC.loader.exec_module(kpi)


def user(text: str) -> dict:
    return {
        "type": "user",
        "isSidechain": False,
        "userType": "external",
        "message": {"content": [{"type": "text", "text": text}]},
    }


def assistant(*blocks: dict) -> dict:
    return {
        "type": "assistant",
        "isSidechain": False,
        "message": {"content": list(blocks)},
    }


def bash(command: str) -> dict:
    return {"type": "tool_use", "name": "Bash", "input": {"command": command}}


def skill(name: str) -> dict:
    return {"type": "tool_use", "name": "Skill", "input": {"skill": name}}


def turn(prompt: str, *records: dict) -> object:
    return kpi.tx.Turn(prompt, tuple(records))


# --- prompt recognition -----------------------------------------------------------------------


def test_recognises_the_five_measured_prompt_forms() -> None:
    """Exactly the shapes that were in the transcripts. Without these five the script measures nothing."""
    assert kpi.fix_pr_number("fix pr 585") == 585
    assert kpi.fix_pr_number("fix 595") == 595
    assert kpi.fix_pr_number("627 pr heeft errors /debug") == 627
    assert kpi.fix_pr_number("612 heeft conflicts, fix die") == 612
    assert kpi.fix_pr_number("kun je conflict 408 goed oplossen?") == 408


def test_ignores_a_number_that_is_merely_mentioned() -> None:
    """A mentioned number is not a fix request; otherwise every PR conversation counts."""
    assert kpi.fix_pr_number("ja werk bij pr 639 is gemerged") is None
    assert kpi.fix_pr_number("Merge PR #715 na groene checks") is None
    assert kpi.fix_pr_number("fix de typo in de eerste alinea") is None


# --- mutating versus diagnostic ---------------------------------------------------------------


def test_bash_mutations_count_as_mutating() -> None:
    """6 of the 8 measured turns mutated via Bash. Counting only Write/Edit is blind by
    construction — the same pitfall as `worktree-first` in the start-issue measurement."""
    assert kpi.is_mutating(
        "Bash", {"command": "git rebase origin/main 2>&1 | tail -20"}
    )
    assert kpi.is_mutating("Bash", {"command": "sed -i 's/a/b/' src/x.py"})
    assert kpi.is_mutating("Bash", {"command": "python3 - <<'PY'\nprint(1)\nPY"})
    assert kpi.is_mutating("Bash", {"command": "gh run rerun 32050303342 --failed"})
    assert kpi.is_mutating("Edit", {"file_path": "src/x.py"})


def test_git_branch_delete_rename_and_copy_count_as_mutating() -> None:
    """Finding 1: `git branch -D` etc. sat in DIAGNOSTIC_BASH_RE before the fix -- a
    branch delete then counted as read-only inspection and pulled the diagnostic median up, exactly
    the side the module docstring excludes."""
    assert kpi.is_mutating("Bash", {"command": "git branch -D feature"})
    assert kpi.is_mutating("Bash", {"command": "git branch -m old new"})
    assert kpi.is_mutating("Bash", {"command": "git branch --delete feature"})


def test_reset_switch_and_stash_apply_count_as_mutating() -> None:
    """Were missing from MUTATING_BASH_RE: all three change the worktree just as much as
    `checkout` or `restore`, which were already in it."""
    assert kpi.is_mutating("Bash", {"command": "git reset --hard origin/main"})
    assert kpi.is_mutating("Bash", {"command": "git switch feature-branch"})
    assert kpi.is_mutating("Bash", {"command": "git stash pop"})
    assert kpi.is_mutating("Bash", {"command": "git stash apply"})


def test_heredoc_recognises_quoted_and_dash_indented_delimiters() -> None:
    """`<<\\s*'?(EOF|PY)` caught only those two literal names. A fix via `<<'PATCH'` or a
    dash-indented `<<-EOF` (commonly used in heredoc Python inside Bash) silently slipped through
    and counted as "no mutation"."""
    assert kpi.is_mutating("Bash", {"command": "python3 - <<'PATCH'\nprint(1)\nPATCH"})
    assert kpi.is_mutating("Bash", {"command": 'cat <<"SCRIPT"\necho hi\nSCRIPT'})
    assert kpi.is_mutating("Bash", {"command": "cat <<-EOF\nhi\nEOF"})


def test_heredoc_regex_does_not_swallow_here_strings_or_bit_shifts() -> None:
    """Regression on the fix above: `<<-?\\s*['\\"]?[A-Z_]...` also matched lower-case letters under `re.IGNORECASE`,
    so a here-string (`<<<`) and a bit shift on lower-case variables
    silently counted as a mutation. A false mutation stops `count_leadin`/`count_diagnostics` too
    early and so UNDERCOUNTS both -- the side the module docstring excludes. The fixed
    combination `(?!<)` plus `(?-i:...)` must keep this out of MUTATING_BASH_RE again, while the
    real heredoc shapes keep matching."""
    assert not kpi.is_mutating("Bash", {"command": "cat <<< 'hello world'"})
    assert not kpi.is_mutating("Bash", {"command": "echo $((x << y))"})
    assert kpi.is_mutating("Bash", {"command": "python3 - <<'PATCH'\nprint(1)\nPATCH"})
    assert kpi.is_mutating("Bash", {"command": "cat <<-EOF\nhi\nEOF"})
    assert kpi.is_mutating("Bash", {"command": 'cat <<"SCRIPT"\necho hi\nSCRIPT'})


def test_read_only_inspection_is_not_mutating() -> None:
    assert not kpi.is_mutating("Bash", {"command": "gh pr view 596 --json state"})
    assert not kpi.is_mutating("Bash", {"command": "git worktree list"})
    assert not kpi.is_mutating("Read", {"file_path": "src/x.py"})


def test_read_only_branch_and_stash_forms_stay_out_of_mutating() -> None:
    """The flip side of findings 1 and 2: the broader regex must not drag in read forms.
    `git branch --merged main` has no flag from the delete/rename/copy set and `git stash list`
    changes nothing -- both must stay outside MUTATING_BASH_RE."""
    assert not kpi.is_mutating("Bash", {"command": "git branch -a"})
    assert not kpi.is_mutating("Bash", {"command": "git branch --merged main"})
    assert not kpi.is_mutating("Bash", {"command": "git stash list"})


def test_diagnostic_recognises_gh_and_git_reads() -> None:
    assert kpi.is_diagnostic("Bash", {"command": "gh pr checks 596 | head -20"})
    assert kpi.is_diagnostic("Bash", {"command": "git worktree list && git branch -a"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --merged main"})
    assert not kpi.is_diagnostic("Bash", {"command": "node -v"})


def test_diagnostic_covers_the_full_stay_diagnostic_set() -> None:
    """Finding C: the previous coverage was too thin to catch finding B (`git stash pop|apply` is
    mutating, but no companion in DIAGNOSTIC_BASH_RE). This test pins all
    read forms that must stay diagnostic after the two regex extensions, including the
    new `git stash list`/`show`."""
    assert kpi.is_diagnostic("Bash", {"command": "git branch -a"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --merged main"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --show-current"})
    assert kpi.is_diagnostic("Bash", {"command": "git log --oneline"})
    assert kpi.is_diagnostic("Bash", {"command": "git status"})
    assert kpi.is_diagnostic("Bash", {"command": "git stash list"})
    assert kpi.is_diagnostic("Bash", {"command": "git stash show -p"})


# --- count per turn ---------------------------------------------------------------------------


def test_leadin_counts_calls_before_the_first_mutation_exclusive() -> None:
    """Exclusive: the mutating call itself is the fix, not the lead-in to it."""
    t = turn(
        "fix pr 585",
        assistant(bash("gh pr view 585 --json state")),
        assistant(bash("gh pr checks 585")),
        assistant(bash("git rebase origin/main")),
    )
    assert kpi.count_leadin(t) == 2


def test_leadin_is_none_when_the_turn_never_mutates() -> None:
    """PR 627 in the baseline measurement: eight diagnostic calls and no mutation at all."""
    t = turn("627 pr heeft errors", assistant(bash("gh pr checks 627")))
    assert kpi.count_leadin(t) is None


def test_diagnostics_are_counted_up_to_the_mutation() -> None:
    t = turn(
        "fix 596",
        assistant(bash("gh pr view 596 --json state"), bash("node -v")),
        assistant(bash("git worktree list")),
        assistant(bash("git rebase origin/main"), bash("git status")),
    )
    assert kpi.count_diagnostics(t) == 2


def test_scope_sees_the_skill_running_past_green() -> None:
    """AC 7 as a number: the skill that runs on into close-issue is exactly the overlap that is not allowed."""
    t = turn(
        "fix pr 585",
        assistant(skill(kpi.SKILL_NAME)),
        assistant(skill("lighthouse-close-issue")),
    )
    assert kpi.overruns_scope(t)

    within = turn(
        "fix pr 585", assistant(skill(kpi.SKILL_NAME)), assistant(bash("git rebase"))
    )
    assert not kpi.overruns_scope(within)


def test_skill_names_are_lighthouse() -> None:
    assert kpi.SKILL_NAME == "lighthouse-fix-pr"
    assert "lighthouse-close-issue" in kpi.SCOPE_SKILLS


def test_loads_start_issue_kpi_as_its_neighbour() -> None:
    """The transcript layer comes from the file next to this script, not from a repo root."""
    assert Path(kpi.tx.__file__).resolve().parent == SCRIPT.resolve().parent


def test_runs_as_installed_file(tmp_path) -> None:
    import subprocess

    result = subprocess.run(
        [sys.executable, str(SCRIPT), "scope", "--dir", str(tmp_path)],
        capture_output=True,
        text=True,
        check=False,
    )
    assert result.returncode == 0, result.stderr
    assert '"past_the_boundary": 0' in result.stdout


def test_help_is_english(capsys) -> None:
    import pytest

    with pytest.raises(SystemExit):
        kpi.main(["--help"])
    out = capsys.readouterr().out
    assert "Review measurement for the skill lighthouse-fix-pr" in out
    assert "KPI-meting" not in out
