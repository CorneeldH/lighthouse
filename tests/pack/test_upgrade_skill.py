"""Pin tests of the skill lighthouse-upgrade (spec #46)."""

import re

import pytest

from tests.pack.python._testkit import CORE

VARIANTS = {v: (CORE / "skills" / "upgrade" / v / "SKILL.md").read_text(encoding="utf-8")
            for v in ("claude", "agents")}


@pytest.mark.parametrize("v", VARIANTS)
def test_frontmatter(v):
    front = VARIANTS[v].split("---\n", 2)[1]
    assert "name: lighthouse-upgrade\n" in front
    assert "origin: own" in front


@pytest.mark.parametrize("v", VARIANTS)
def test_reads_both_versions(v):
    assert ".lighthouse/manifest.toml" in VARIANTS[v]
    assert "lighthouse version" in VARIANTS[v]


@pytest.mark.parametrize("v", VARIANTS)
def test_versions_are_compared_without_the_v_prefix(v):
    assert "#v}" in VARIANTS[v] and "sort -V" in VARIANTS[v]


@pytest.mark.parametrize("v", VARIANTS)
def test_dev_build_stops(v):
    assert re.search(r"`dev`.*stop", VARIANTS[v], re.I)


@pytest.mark.parametrize("v", VARIANTS)
def test_older_binary_stops(v):
    assert re.search(r"older than the (repo|manifest).*stop", VARIANTS[v], re.I | re.S)


@pytest.mark.parametrize("v", VARIANTS)
def test_binary_only_through_self_update_after_consent(v):
    t = VARIANTS[v]
    assert "lighthouse self-update" in t and "go install" not in t
    assert "whole host" in t


@pytest.mark.parametrize("v", VARIANTS)
def test_declined_self_update_continues_only_when_binary_is_ahead(v):
    t = VARIANTS[v]
    assert "already current" in t
    assert re.search(r"ahead.*continue|continue.*ahead", t, re.I | re.S)


@pytest.mark.parametrize("v", VARIANTS)
def test_release_notes_come_from_the_lighthouse_repo(v):
    """Pins ci/literal-allowlist.txt: the one line that names the lighthouse repo."""
    lines = VARIANTS[v].splitlines()
    assert lines.count("L=CorneeldH/lighthouse") == 1
    assert sum("CorneeldH" in line for line in lines) == 1
    assert '--repo "$L"' in VARIANTS[v]


@pytest.mark.parametrize("v", VARIANTS)
def test_never_forces_a_conflict(v):
    t = VARIANTS[v]
    assert "refused:" in t
    assert all("never" in line.lower() for line in t.splitlines() if "--force" in line)


@pytest.mark.parametrize("v", VARIANTS)
def test_evidence_in_order(v):
    t = VARIANTS[v]
    t = t[t.index("## Workflow"):]
    keys = ["test_gate.command", "lighthouse check-instructions", "--print-mirror-exceptions", "No changes."]
    assert [t.index(k) for k in keys] == sorted(t.index(k) for k in keys)


@pytest.mark.parametrize("v", VARIANTS)
def test_commit_holds_only_what_init_wrote(v):
    t = VARIANTS[v]
    assert "git status" in t and "chore(lighthouse):" in t
    assert re.search(r"anything else.*stop", t, re.I | re.S)


@pytest.mark.parametrize("v", VARIANTS)
def test_init_applies_only_after_consent_with_yes(v):
    t = VARIANTS[v]
    assert t.index("</dev/null") < t.index("--upgrade --yes")
    assert "LIGHTHOUSE_GATE_TMP" in t


@pytest.mark.parametrize("v", VARIANTS)
def test_push_and_pr_after_consent_through_write_pr(v):
    assert "/lighthouse-write-pr" in VARIANTS[v]


def test_agents_md_lists_the_skill():
    assert "- `/lighthouse-upgrade`:" in (CORE / "instructions" / "AGENTS.md").read_text(encoding="utf-8")


@pytest.mark.parametrize("v", VARIANTS)
def test_network_steps_name_the_sandbox_and_report_a_failed_fetch(v):
    t = VARIANTS[v]
    step3 = t[t.index("### 3."):t.index("### 4.")]
    assert re.search(r"sandbox", step3, re.I)
    assert "could not fetch release" in step3
    step2 = t[t.index("### 2."):t.index("### 3.")]
    assert re.search(r"network", step2)


@pytest.mark.parametrize("v", VARIANTS)
def test_empty_version_stops_before_comparing(v):
    assert re.search(r"is empty.*stop, before any comparison", VARIANTS[v], re.S)


@pytest.mark.parametrize("v", VARIANTS)
def test_empty_test_gate_is_no_evidence(v):
    t = VARIANTS[v]
    assert re.search(r"prints nothing.*skip it.*no evidence", t, re.S)
    assert 'never count `bash -c ""` as green' in t
