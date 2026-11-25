CREATE TABLE setups_new (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  model TEXT, effort TEXT,
  harness TEXT, harness_version TEXT,
  superpowers_version TEXT, lighthouse_version TEXT,
  pack_version TEXT, skills_hash TEXT,
  lane TEXT CHECK (lane IN ('sdd', 'native')),
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  UNIQUE (run_id), UNIQUE (session_id),
  UNIQUE (source, source_id)
);

INSERT INTO setups_new (id, source, source_id, recorded_at, run_id, session_id,
    model, effort, harness, harness_version, superpowers_version, lighthouse_version, pack_version, skills_hash)
  SELECT id, source, source_id, recorded_at, run_id, session_id,
    NULLIF(model, ''), NULLIF(effort, ''), NULLIF(harness, ''), NULLIF(harness_version, ''),
    NULLIF(superpowers_version, ''), NULLIF(lighthouse_version, ''), NULLIF(pack_version, ''), NULLIF(skills_hash, '')
  FROM setups;

DROP TABLE setups;
ALTER TABLE setups_new RENAME TO setups;

ALTER TABLE trajectories ADD COLUMN plan_tasks INTEGER;
ALTER TABLE trajectories ADD COLUMN plan_path TEXT;
