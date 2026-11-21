CREATE TABLE recoveries (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER NOT NULL REFERENCES trajectories(id),
  pr INTEGER NOT NULL,
  merged_at TEXT NOT NULL,
  window_end TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('open', 'recovered', 'none')),
  fixpr_rounds INTEGER NOT NULL DEFAULT 0,
  UNIQUE (trajectory_id, pr),
  UNIQUE (source, source_id)
);

CREATE TABLE recovery_items (
  id INTEGER PRIMARY KEY,
  recovery_id INTEGER NOT NULL REFERENCES recoveries(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('fix', 'revert', 'bug_issue')),
  ref TEXT NOT NULL,
  at TEXT NOT NULL,
  UNIQUE (recovery_id, kind, ref)
);

CREATE INDEX recovery_items_recovery ON recovery_items(recovery_id);
