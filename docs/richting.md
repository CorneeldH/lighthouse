# Direction documents

A repository can keep three short documents that say where it is going: a **strategy**, a set of
**tenets** and one file per **area**. They record direction, never status: no dates, no issue lists,
no "in progress". The skills read them to judge whether new work fits, and `lighthouse direction
check` checks their shape.

They are optional. A repository that sets none of them notices nothing: `init` seeds nothing,
the skills skip the step silently and `lighthouse direction check` prints nothing and exits 0.

## The three documents

- **Strategy**: a diagnosis of the problem the product answers, the audiences in order of priority,
  the guiding policy, what is deliberately not done (*deliberately not*) and one sentence on where the
  roadmap lives. A point in *deliberately not* has an ID in bold, `S1`, `S2`, ..., cited as `strategy/S1`.
- **Tenets**: beliefs that make a decision faster, each in the form "X over Y" with an opposite
  that someone could reasonably choose, and at least two decisions as evidence. The order in the
  file is the precedence: when two tenets conflict, the higher one wins. A hard rule (a hook, a gate,
  `AGENTS.md`) always wins over a tenet. Cited as `tenet A`.
- **Areas**: one file per area label, named after the label without its prefix. Each has a crux (the
  one problem of the area), a leading policy and its deliberate no's with IDs `N1`, `N2`, ..., cited
  as `<area>/N1`. A no names the neighbour area with an arrow (`→ <area>`) when the work belongs
  there.

## Turn them on

Either of these opts the repository in:

- `lighthouse init --direction` (with `--upgrade` on a repository that already has lighthouse), or
- set one of `strategy`, `tenets` or `areas` under `[paths]` in `lighthouse.toml` and run
  `lighthouse init --upgrade`.

`init` then proposes the missing `[paths]` keys and seeds the missing documents from the templates of
the core package, on the diff and under the same approval as the rest. Details:

- Setting one `[paths]` key is enough: `init` adds the other two keys it misses.
- Without keys, the defaults are `docs/developer/strategie.md`, `docs/developer/uitgangspunten.md`
  and `docs/developer/areas/`. Set the keys yourself to put them elsewhere. A path must be relative
  and stay inside the repository.
- The area files come from the label taxonomy (`[paths] labels`): one file per label with the
  `[labels] area_prefix`, named after the label without the prefix (`area/api` gives `api.md`).
  Without a taxonomy or without area labels, `init` makes only the areas folder, with a
  `.gitkeep`, and with `--direction` prints one line saying why no area files were seeded.
- The headings of the seeded files are in your `[project] language` (English when it is not set).
  The templates carry guidance as HTML comments in English.

## `init` only seeds what is missing

An existing file is never overwritten, and the seeded files are yours: they are not in the
manifest, so `--upgrade` never touches them or reports them as changed. Once a repository has opted
in, every `lighthouse init --upgrade` prints, on stderr, one line per document that already exists:

```text
direction: docs/developer/strategie.md exists; left as it is
```

That is expected, not a problem. A document you delete is seeded again at the next `--upgrade`.

## Fill them in

Replace the placeholder text in each template and keep its headings. For the strategy and the area
files the check wants exactly those headings, in that order, and no other second-level heading; the
tenets file has one heading per tenet instead.

- Strategy: write the diagnosis as one paragraph, the audiences as a numbered list, three to five
  bullets of policy, and under *deliberately not* one bullet per deliberate no, each starting with a bold
  unique ID (`**S1** No hosted service: the tool runs where the code is.`). A dropped point keeps its
  ID out of use.
- Tenets: one second-level heading per tenet, `## A. <X> over <Y>`, one sentence why, and an
  evidence line with at least two links to specs or milestones.
- Areas: a crux paragraph, three to five bullets of policy, and the no's with IDs `N1`... that name
  a neighbour area with an arrow when the work belongs there.

## What reads them

- `write-issue`: step 3f tests an issue that may go on the map against the tenets, the leading
  policy and the no's of its area and the strategy, and names the ID that carries the judgement.
  A milestone pitch names under *Problem / Opportunity* the crux of the areas it touches.
- `close-issue`: step 5 updates the near goal of an area only when its file has that heading. The
  seeded template has none and `lighthouse direction check` rejects it as an extra heading, so by
  default this step does nothing.
- The scheduled job `map-planned` reads the area files when it asks whether a planned issue belongs
  on the map.

Each skill skips the step silently when the key is not set.

## Check the shape

```text
lighthouse direction check
```

It prints each finding as `path:line: message`. It checks form only, never whether the content is
wise: the headings, the IDs (unique per file), the "X over Y" form and its evidence links, the
arrows to area labels and that every relative link exists. Exit codes:

| Exit | Meaning |
| ---- | ------- |
| 0 | No finding, or the repository opted out (none of the three keys is set) |
| 1 | At least one finding |
| 2 | `COULD NOT CHECK`: no repository root, or an unparsable `lighthouse.toml` |

Once at least one key is set, a missing or unreadable document of the other two is a finding.

## The headings in your language

The check accepts the heading in every language variant. The keys are `strategy_diagnosis`,
`strategy_audience`, `policy`, `not_now`, `strategy_roadmap`, `area_crux`,
`tenet_over` and `tenet_evidence`. To see what one is in your language:

```text
lighthouse headings not_now
```

See also [Getting started](getting-started.md).
