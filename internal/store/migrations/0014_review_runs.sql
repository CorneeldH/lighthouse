CREATE TABLE review_runs (
  id INTEGER PRIMARY KEY,
  source TEXT NOT NULL, source_id TEXT NOT NULL, recorded_at TEXT NOT NULL,
  trajectory_id INTEGER REFERENCES trajectories(id),
  unlinked_reason TEXT CHECK (unlinked_reason IN ('no_trajectory')),
  component TEXT NOT NULL CHECK (component IN ('plan_review', 'spec_review')),
  record_path TEXT NOT NULL, record_sum TEXT NOT NULL,
  ran_at TEXT NOT NULL,                  -- written_at of the record
  outcome TEXT,                          -- outcome of the record: converged, round_limit, aborted
  finding_count INTEGER NOT NULL,
  CHECK ((trajectory_id IS NULL) = (unlinked_reason IS NOT NULL)),
  UNIQUE (source, source_id)
);
CREATE INDEX review_runs_trajectory ON review_runs(trajectory_id);

ALTER TABLE trajectories ADD COLUMN route TEXT;      -- gate from the triage marker: '0', '1', '2', '4a', '4b', '4c'
ALTER TABLE trajectories ADD COLUMN area TEXT;       -- the area label, without its prefix
ALTER TABLE recoveries ADD COLUMN code_lines INTEGER; -- roborev.CountCodeLines on the PR diff
