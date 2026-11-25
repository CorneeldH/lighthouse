CREATE TABLE trajectory_events (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER NOT NULL REFERENCES trajectories(id),
  run_id INTEGER REFERENCES runs(id),
  session_id INTEGER REFERENCES sessions(id),
  kind TEXT NOT NULL CHECK (kind IN ('question', 'answer', 'done')),
  ref TEXT NOT NULL,
  at TEXT NOT NULL,
  CHECK ((run_id IS NULL) <> (session_id IS NULL)),
  CHECK (kind <> 'done' OR run_id IS NOT NULL),
  UNIQUE (source, source_id),
  UNIQUE (trajectory_id, kind, ref)
);

CREATE INDEX trajectory_events_trajectory ON trajectory_events(trajectory_id);
