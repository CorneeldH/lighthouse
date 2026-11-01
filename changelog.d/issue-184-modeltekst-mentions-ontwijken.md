---
type: fixed
---
The triage comment of `lighthouse triage` no longer turns `@name`, `#N` or `owner/repo#N` in the model's free text into a mention or a cross-reference: a zero-width joiner follows the `@` or `#`. The gate-0 blockers and the `#N` of a possible duplicate, which Go writes itself, stay clickable.
