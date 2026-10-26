"""Usage-guard module `controller` (`[usage_guard] controller = true`): the wording and the
resume marker of a controller run, which the controller resumes after the reset.

The guard decides whether to speak; this module supplies what is said and written. In
`tool` and `stop` it parks the run: one marker per session, with `"controller": true`, and
no handoff and no wake-up, because the controller resumes the session itself. Without a
`session_id` in the payload the run id is the session id: it is the session uuid, and
without a marker the controller never resumes the run. `prompt` only warns. `pause` parks
the run for the controller's PAUSE flag. The lib writes the keys, never this module.

The syntax stays within Python 3.9.
"""

from __future__ import annotations

from datetime import datetime

from lighthouse_lib.usage_guard import (
    Context,
    block,
    headline,
    marker,
    prompt_note,
    write_marker,
)

PARK = (
    "commit the work in progress, then stop: do not start a new step, do not dispatch a "
    "subagent, do not schedule a wake-up and do not write a handoff"
)


def _clock(until: str) -> str:
    """`HH:MM` of an ISO time with offset, as written; the raw text when it does not parse."""
    try:
        return datetime.fromisoformat(until.replace("Z", "+00:00")).strftime("%H:%M")
    except ValueError:
        return until


def _park(ctx: Context, *, window: str, reset_at: str, source: str, **flags: bool) -> None:
    fields = marker(
        ctx,
        session_id=ctx.session_id or ctx.run_id,
        window=window,
        reset_at=reset_at,
        source=source,
    )
    write_marker(dict(fields, controller=True, **flags))


def speak(ctx: Context, stand: str) -> dict:
    """`stand` is prompt, tool or stop."""
    assert ctx.alarm is not None
    if stand == "prompt":
        return prompt_note(
            f"{headline(ctx)} This controller run parks at the next tool call: finish only "
            "the current step and commit the work in progress."
        )
    _park(ctx, window=ctx.alarm.kind, reset_at=ctx.alarm.reset_at, source=stand)
    return block(
        f"{headline(ctx)} Now {PARK}. The controller resumes this session after "
        f"{ctx.alarm.reset_local}."
    )


def pause(ctx: Context) -> dict:
    """The controller's PAUSE flag is valid: park the run until the time in the flag."""
    _park(ctx, window="pause", reset_at=ctx.pause_until, source="tool", pause=True)
    return block(
        f"The controller has paused this run until {_clock(ctx.pause_until)}. Now {PARK}. "
        f"The controller resumes this session after {_clock(ctx.pause_until)}."
    )
