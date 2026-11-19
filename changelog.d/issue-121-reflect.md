---
type: added
---
Scheduled job `reflect` (#121): a `[[schedule]]` block with `job = "reflect"` has a skill read the prompts of the repo's sessions and write one document of skill candidates. A Go gate counts the sessions since the last round (interactive and controller, told apart by the run history; sessions of scheduled runs do not count) and skips a round below `min_sessions`; Go writes the prompts into one corpus file with the sections Interactief and Controller, within `max_corpus_bytes`, which the run reads instead of raw sessions. The run may write one dated document and `output_dir/index.md`, with no Bash; the corpus never reaches the branch. See `docs/tick.md`.
