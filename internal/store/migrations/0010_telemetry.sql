CREATE TABLE trajectories (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL,
  issue INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE (repo, issue)
);

ALTER TABLE runs ADD COLUMN trajectory_id INTEGER REFERENCES trajectories(id);
ALTER TABLE runs ADD COLUMN assignment TEXT NOT NULL DEFAULT 'standard'
  CHECK (assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*');

CREATE TABLE sessions (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  repo TEXT NOT NULL,
  harness TEXT NOT NULL,
  session_key TEXT NOT NULL,
  transcript_path TEXT NOT NULL,
  branch TEXT NOT NULL DEFAULT '',
  trajectory_id INTEGER REFERENCES trajectories(id),
  link_basis TEXT CHECK (link_basis IN ('branch', 'transcript')),
  unlinked_reason TEXT CHECK (unlinked_reason IN ('main_only', 'no_issue_branch', 'no_issue_number')),
  assignment TEXT NOT NULL DEFAULT 'standard'
    CHECK (assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*'),
  started_at TEXT NOT NULL, ended_at TEXT,
  CHECK ((trajectory_id IS NULL) = (unlinked_reason IS NOT NULL)),
  CHECK ((trajectory_id IS NULL) = (link_basis IS NULL)),
  UNIQUE (harness, session_key),
  UNIQUE (source, source_id)
);

CREATE TABLE setups (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  model TEXT NOT NULL DEFAULT '', effort TEXT NOT NULL DEFAULT '',
  harness TEXT NOT NULL DEFAULT '', harness_version TEXT NOT NULL DEFAULT '',
  superpowers_version TEXT NOT NULL DEFAULT '', lighthouse_version TEXT NOT NULL DEFAULT '',
  pack_version TEXT NOT NULL DEFAULT '', skills_hash TEXT NOT NULL DEFAULT '',
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  UNIQUE (run_id), UNIQUE (session_id),
  UNIQUE (source, source_id)
);

CREATE TABLE usage (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  model TEXT NOT NULL,
  input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
  cache_read_tokens INTEGER NOT NULL DEFAULT 0, cache_write_tokens INTEGER NOT NULL DEFAULT 0,
  cost REAL,
  cost_kind TEXT CHECK (cost_kind IN ('estimate', 'quota', 'actual')),
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  CHECK ((cost IS NULL) = (cost_kind IS NULL)),
  UNIQUE (run_id, model), UNIQUE (session_id, model),
  UNIQUE (source, source_id)
);

CREATE TABLE findings (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER NOT NULL REFERENCES trajectories(id),
  run_id INTEGER REFERENCES runs(id), session_id INTEGER REFERENCES sessions(id),
  component TEXT NOT NULL,
  finding_key TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT '',
  decision TEXT CHECK (decision IN ('adopted', 'rejected', 'left_open')),
  record_path TEXT NOT NULL, record_sum TEXT NOT NULL,
  CHECK (run_id IS NULL OR session_id IS NULL),
  UNIQUE (source, source_id)
);

CREATE INDEX runs_trajectory ON runs(trajectory_id);
CREATE INDEX sessions_trajectory ON sessions(trajectory_id);
CREATE INDEX findings_trajectory ON findings(trajectory_id);

INSERT INTO trajectories (repo, issue, created_at)
  SELECT repo, issue, min(started_at) FROM runs WHERE issue IS NOT NULL GROUP BY repo, issue;
UPDATE runs SET trajectory_id =
  (SELECT t.id FROM trajectories t WHERE t.repo = runs.repo AND t.issue = runs.issue)
  WHERE issue IS NOT NULL;
