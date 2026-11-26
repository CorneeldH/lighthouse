-- What actually ran in a run beyond model and harness: the prompt hashes of the reviews
-- (JSON object {"<skill>": "<sha256>"}) and the controller settings at collect time
-- (JSON: profile, spec_review, plan_review, at_round_limit). NULL is unknown.
ALTER TABLE setups ADD COLUMN prompt_hashes TEXT;
ALTER TABLE setups ADD COLUMN controller_settings TEXT;

-- A change of model or reviewer during a run, such as a fallback from Codex to Opus.
-- UNIQUE (run_id) on setups stays; a switch hangs off the one setup of its run.
CREATE TABLE setup_switches (
  id INTEGER PRIMARY KEY,
  setup_id INTEGER NOT NULL REFERENCES setups(id),
  at TEXT NOT NULL,
  field TEXT NOT NULL CHECK (field IN ('model', 'reviewer')),
  from_value TEXT NOT NULL,
  to_value TEXT NOT NULL,
  UNIQUE (setup_id, at, field)
);
