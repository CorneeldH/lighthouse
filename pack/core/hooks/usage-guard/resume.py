"""Usage-guard module `resume` (`[usage_guard] resume = true`): the wording of an ordinary
session's alarm, plus the resume marker and the wake-up step.

The guard decides whether to speak; this module supplies what is said and written. In
`tool` and `stop` it writes one marker per session (only with a `session_id`). A
`venster` alarm also asks for a one-shot wake-up via `CronCreate`; a `week` alarm never.
`prompt` names the same steps but parks nothing, so it writes no marker.

The syntax stays within Python 3.9.
"""

from __future__ import annotations

from datetime import datetime, timedelta, tzinfo
from typing import Optional

from lighthouse_lib.usage_guard import (
    Context,
    block,
    handoff_path,
    marker,
    prompt_note,
    write_marker,
    _headline,
)

WAKE_DELAY = timedelta(minutes=2)


def wake_cron(reset_at: str, tz: Optional[tzinfo] = None) -> str:
    """`reset_at` plus 2 minutes in `tz` (None: local) as `M H D Mon *`, no leading zeros."""
    at = datetime.fromisoformat(reset_at.replace("Z", "+00:00")) + WAKE_DELAY
    at = at.astimezone(tz)
    return f"{at.minute} {at.hour} {at.day} {at.month} *"


def _wake_step(ctx: Context) -> str:
    assert ctx.alarm is not None
    cron = wake_cron(ctx.alarm.reset_at)
    return (
        f'Schedule one wake-up with CronCreate: cron "{cron}", recurring false, prompt = '
        "the first step from the handoff. If you already set a wake-up in this window, "
        "check with CronList and take over that job ID instead of setting a second one."
    )


def speak(ctx: Context, stand: str) -> dict:
    """`stand` is prompt, tool or stop."""
    assert ctx.alarm is not None
    venster = ctx.alarm.kind == "venster"
    head = _headline(ctx)
    handoff = f"`/lighthouse-handoff {handoff_path(ctx)}`"
    wake = f" {_wake_step(ctx)}" if venster else ""
    job = " Name the wake-up's job ID in the handoff." if venster else ""

    if stand == "prompt":
        return prompt_note(
            f"{head} Wrap up: finish only the current step, commit the work in progress,"
            f"{wake} run {handoff}, and do not start a new step.{job}"
        )

    if stand == "stop":
        reason = (
            f"{head} Commit nothing more.{wake} Run {handoff} and note in it what is "
            f"uncommitted and when to resume (after {ctx.alarm.reset_local}).{job} Then stop."
        )
    else:
        reason = (
            f"{head} Before anything else: commit the work in progress,{wake} run "
            f"{handoff}, and report where the work stands (worktree, branch, what is "
            f"still open).{job} Do not start a new step and do not dispatch a subagent."
        )

    if ctx.session_id:
        fields = marker(
            ctx,
            session_id=ctx.session_id,
            window=ctx.alarm.kind,
            reset_at=ctx.alarm.reset_at,
            source=stand,
        )
        write_marker(dict(fields, handoff_path=str(handoff_path(ctx))))
    return block(reason)
