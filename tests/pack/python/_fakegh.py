"""A fake `gh` that replays the scripted calls of a `scripts/*/cases.json` case.

Reads `FAKE_CALLS` (a JSON list of calls), `FAKE_COUNT` (a file with the number
of calls made) and `FAKE_LOG`. A call that is not the next one, or one after
the list ran out, writes the expected and the actual argv to `FAKE_LOG` and
exits 97. The Go twin is `internal/scriptcase.Replay`.
"""

import json
import os
import sys
import time
from pathlib import Path

DEVIATION = 97
# A call with `"timeout": true` outlasts any timeout of a command.
TIMEOUT_SLEEP = 3600


def main(argv: list[str]) -> int:
    calls = json.loads(Path(os.environ["FAKE_CALLS"]).read_text(encoding="utf-8"))
    count_file = Path(os.environ["FAKE_COUNT"])
    log = Path(os.environ["FAKE_LOG"])
    index = int(count_file.read_text(encoding="utf-8") or "0")
    actual = ["gh", *argv]

    def deviate(message: str) -> int:
        with log.open("a", encoding="utf-8") as handle:
            handle.write(message + "\n")
        sys.stderr.write(f"fake gh: {message}\n")
        return DEVIATION

    if index >= len(calls):
        return deviate(
            f"unexpected call {actual}: all {len(calls)} scripted calls were used"
        )
    call = calls[index]
    if call["argv"] != actual:
        return deviate(f"call {index + 1}: expected {call['argv']}, got {actual}")
    if call.get("stdin") is not None and sys.stdin.read() != call["stdin"]:
        return deviate(f"call {index + 1} {actual}: stdin differs")
    count_file.write_text(str(index + 1), encoding="utf-8")
    if call.get("timeout"):
        time.sleep(TIMEOUT_SLEEP)
    sys.stdout.write(call.get("stdout", ""))
    sys.stderr.write(call.get("stderr", ""))
    return int(call.get("code", 0))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
