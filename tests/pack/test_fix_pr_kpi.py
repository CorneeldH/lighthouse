"""Unit tests voor scripts/measure_fix_pr_kpi.py (issue #664).

De KPI van #664 is één getal: hoeveel tool-calls zitten er tussen "fix 596" en de eerste handeling
die de boom daadwerkelijk verandert. Dat getal is alleen iets waard als drie dingen kloppen, en
precies die drie worden hier gepind:

- de promptherkenning is smal genoeg (een genoemd PR-nummer is geen fixverzoek);
- "muterend" is breder dan Write/Edit — 6 van de 8 gemeten beurten muteerden via Bash;
- de scope-rij ziet het geval waarvoor hij bestaat: de skill die doorloopt in close-issue.
"""

from __future__ import annotations

import importlib.util
import sys
from pathlib import Path

_SPEC = importlib.util.spec_from_file_location(
    "measure_fix_pr_kpi",
    Path(__file__).resolve().parents[2] / "scripts" / "measure_fix_pr_kpi.py",
)
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


# --- promptherkenning -------------------------------------------------------------------------


def test_recognises_the_five_measured_prompt_forms() -> None:
    """Precies de vormen die in de transcripten stonden. Zonder deze vijf meet het script niets."""
    assert kpi.fix_pr_number("fix pr 585") == 585
    assert kpi.fix_pr_number("fix 595") == 595
    assert kpi.fix_pr_number("627 pr heeft errors /debug") == 627
    assert kpi.fix_pr_number("612 heeft conflicts, fix die") == 612
    assert kpi.fix_pr_number("kun je conflict 408 goed oplossen?") == 408


def test_ignores_a_number_that_is_merely_mentioned() -> None:
    """Een genoemd nummer is geen fixverzoek; anders telt elke PR-conversatie mee."""
    assert kpi.fix_pr_number("ja werk bij pr 639 is gemerged") is None
    assert kpi.fix_pr_number("Merge PR #715 na groene checks") is None
    assert kpi.fix_pr_number("fix de typo in de eerste alinea") is None


# --- muterend versus diagnostisch -------------------------------------------------------------


def test_bash_mutations_count_as_mutating() -> None:
    """6 van de 8 gemeten beurten muteerden via Bash. Alleen Write/Edit tellen is per
    constructie blind — dezelfde valkuil als `worktree-first` in de start-issue-meting."""
    assert kpi.is_mutating(
        "Bash", {"command": "git rebase origin/main 2>&1 | tail -20"}
    )
    assert kpi.is_mutating("Bash", {"command": "sed -i 's/a/b/' src/x.py"})
    assert kpi.is_mutating("Bash", {"command": "python3 - <<'PY'\nprint(1)\nPY"})
    assert kpi.is_mutating("Bash", {"command": "gh run rerun 32050303342 --failed"})
    assert kpi.is_mutating("Edit", {"file_path": "src/x.py"})


def test_git_branch_delete_rename_and_copy_count_as_mutating() -> None:
    """Bevinding 1: `git branch -D` etc. zaten vóór de fix in DIAGNOSTIC_BASH_RE -- een
    branch-delete gold dan als read-only inspectie en trok de diagnose-mediaan omhoog, precies
    de kant die de moduledocstring uitsluit."""
    assert kpi.is_mutating("Bash", {"command": "git branch -D feature"})
    assert kpi.is_mutating("Bash", {"command": "git branch -m old new"})
    assert kpi.is_mutating("Bash", {"command": "git branch --delete feature"})


def test_reset_switch_and_stash_apply_count_as_mutating() -> None:
    """Ontbraken in MUTATING_BASH_RE: alle drie veranderen de werkboom net zo hard als
    `checkout` of `restore`, die er al wel in stonden."""
    assert kpi.is_mutating("Bash", {"command": "git reset --hard origin/main"})
    assert kpi.is_mutating("Bash", {"command": "git switch feature-branch"})
    assert kpi.is_mutating("Bash", {"command": "git stash pop"})
    assert kpi.is_mutating("Bash", {"command": "git stash apply"})


def test_heredoc_recognises_quoted_and_dash_indented_delimiters() -> None:
    """`<<\\s*'?(EOF|PY)` ving alleen die twee letterlijke namen. Een fix via `<<'PATCH'` of een
    dash-ingesprongen `<<-EOF` (veelgebruikt in heredoc-Python binnen Bash) viel er stil doorheen
    en telde als "geen mutatie"."""
    assert kpi.is_mutating("Bash", {"command": "python3 - <<'PATCH'\nprint(1)\nPATCH"})
    assert kpi.is_mutating("Bash", {"command": 'cat <<"SCRIPT"\necho hi\nSCRIPT'})
    assert kpi.is_mutating("Bash", {"command": "cat <<-EOF\nhi\nEOF"})


def test_heredoc_regex_does_not_swallow_here_strings_or_bit_shifts() -> None:
    """Regressie op de fix hierboven: `<<-?\\s*['\\"]?[A-Z_]...` matchte onder `re.IGNORECASE`
    óók kleine letters, dus een here-string (`<<<`) en een bit-shift op kleine-letter-variabelen
    telden stiekem als mutatie mee. Een valse mutatie stopt `count_leadin`/`count_diagnostics` te
    vroeg en ONDERSCHAT dus allebei -- de kant die de moduledocstring juist uitsluit. De vaste
    combinatie `(?!<)` plus `(?-i:...)` moet dit weer buiten MUTATING_BASH_RE houden, terwijl de
    echte heredoc-vormen wel blijven matchen."""
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
    """De keerzijde van bevinding 1 en 2: de bredere regex mag leesvormen niet meesleuren.
    `git branch --merged main` heeft geen vlag uit de delete/rename/copy-set en `git stash list`
    verandert niets -- allebei moeten ze buiten MUTATING_BASH_RE blijven."""
    assert not kpi.is_mutating("Bash", {"command": "git branch -a"})
    assert not kpi.is_mutating("Bash", {"command": "git branch --merged main"})
    assert not kpi.is_mutating("Bash", {"command": "git stash list"})


def test_diagnostic_recognises_gh_and_git_reads() -> None:
    assert kpi.is_diagnostic("Bash", {"command": "gh pr checks 596 | head -20"})
    assert kpi.is_diagnostic("Bash", {"command": "git worktree list && git branch -a"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --merged main"})
    assert not kpi.is_diagnostic("Bash", {"command": "node -v"})


def test_diagnostic_covers_the_full_stay_diagnostic_set() -> None:
    """Bevinding C: de vorige dekking was te dun om bevinding B (`git stash pop|apply` wél
    muterend, maar geen companion in DIAGNOSTIC_BASH_RE) op te vangen. Deze test pint alle
    leesvormen die na de twee regex-uitbreidingen diagnostisch moeten blijven, inclusief de
    nieuwe `git stash list`/`show`."""
    assert kpi.is_diagnostic("Bash", {"command": "git branch -a"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --merged main"})
    assert kpi.is_diagnostic("Bash", {"command": "git branch --show-current"})
    assert kpi.is_diagnostic("Bash", {"command": "git log --oneline"})
    assert kpi.is_diagnostic("Bash", {"command": "git status"})
    assert kpi.is_diagnostic("Bash", {"command": "git stash list"})
    assert kpi.is_diagnostic("Bash", {"command": "git stash show -p"})


# --- telling per beurt ------------------------------------------------------------------------


def test_leadin_counts_calls_before_the_first_mutation_exclusive() -> None:
    """Exclusief: de muterende call zelf is de fix, niet de aanloop ernaartoe."""
    t = turn(
        "fix pr 585",
        assistant(bash("gh pr view 585 --json state")),
        assistant(bash("gh pr checks 585")),
        assistant(bash("git rebase origin/main")),
    )
    assert kpi.count_leadin(t) == 2


def test_leadin_is_none_when_the_turn_never_mutates() -> None:
    """PR 627 in de nulmeting: acht diagnosecalls en geen enkele mutatie."""
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
    """AC 7 als getal: de skill die doorloopt in close-issue is precies de overlap die niet mag."""
    t = turn(
        "fix pr 585",
        assistant(skill("paia-fix-pr")),
        assistant(skill("paia-close-issue")),
    )
    assert kpi.overruns_scope(t)

    binnen = turn(
        "fix pr 585", assistant(skill("paia-fix-pr")), assistant(bash("git rebase"))
    )
    assert not kpi.overruns_scope(binnen)
