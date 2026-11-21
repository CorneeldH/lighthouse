# Data of the code-language check

The scorer in [`score.go`](../score.go) embeds these files with `go:embed`.

- `freq.tsv.gz`: word frequencies for English and Dutch, generated from wordfreq
  3.1.1 by `tools/gen_code_language_freq.py`, removed after commit `c49fd274` (the data is no
  longer updated upstream). To get it back, from the repo root:

  ```bash
  git checkout c49fd274 -- tools/gen_code_language_freq.py
  uv run --with wordfreq==3.1.1 python tools/gen_code_language_freq.py
  ```

  The script must sit in `tools/`: it finds the deny list and its output through
  `Path(__file__).resolve().parents[1]`. The data is licensed under CC BY-SA 4.0
  ([`LICENSES/CC-BY-SA-4.0.txt`](../../../LICENSES/CC-BY-SA-4.0.txt)); that license covers this
  file only, not the rest of lighthouse. The attribution and the changes made to the data are in
  [`NOTICE.txt`](NOTICE.txt), which travels with it.
- `jargon.txt`: English words that look Dutch in names and prose; the scorer judges them as
  English.
- `NOTICE.txt`: the attribution for `freq.tsv.gz` (wordfreq 3.1.1).
