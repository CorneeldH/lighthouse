CREATE TABLE runs_new (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL, issue INTEGER, schedule TEXT,
  session_id TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('start', 'resume')),
  trigger TEXT NOT NULL CHECK (trigger IN ('handoff', 'answer', 'usage', 'closecheck', 'manual', 'fixpr', 'scheduled')),
  trigger_key TEXT NOT NULL,
  attempt INTEGER NOT NULL CHECK (attempt IN (1, 2)),
  command TEXT NOT NULL DEFAULT '', prompt TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL DEFAULT '',
  workplace_session TEXT NOT NULL DEFAULT '', profile TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT,
  outcome TEXT CHECK (outcome IN ('start_failed', 'handoff', 'question', 'parked', 'done', 'returned', 'failed', 'retry')),
  reason TEXT NOT NULL DEFAULT '',
  workspace_removed_at TEXT,
  merge_sha TEXT NOT NULL DEFAULT '',
  merge_error TEXT NOT NULL DEFAULT '',
  node TEXT NOT NULL DEFAULT '',
  start_sha TEXT NOT NULL DEFAULT '',
  verdict TEXT NOT NULL DEFAULT '',
  pr_at TEXT,
  CHECK ((ended_at IS NULL) = (outcome IS NULL)),
  CHECK ((issue IS NULL) <> (schedule IS NULL))
);
INSERT INTO runs_new (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
    branch, workspace_id, workplace_session, profile, started_at, ended_at, outcome, reason,
    workspace_removed_at, merge_sha, merge_error, node)
  SELECT id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
    branch, workspace_id, workplace_session, profile, started_at, ended_at, outcome, reason,
    workspace_removed_at, merge_sha, merge_error, node FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE UNIQUE INDEX runs_open_issue ON runs(repo, issue) WHERE ended_at IS NULL AND issue IS NOT NULL;
CREATE UNIQUE INDEX runs_open_schedule ON runs(repo, schedule) WHERE ended_at IS NULL AND schedule IS NOT NULL;
CREATE INDEX runs_trigger ON runs(repo, trigger_key);
CREATE TABLE schedule_slots (
  repo TEXT NOT NULL, schedule TEXT NOT NULL, slot TEXT NOT NULL,
  outcome TEXT NOT NULL CHECK (outcome = 'skipped'),
  reason TEXT NOT NULL DEFAULT '', at TEXT NOT NULL,
  PRIMARY KEY (repo, schedule, slot)
);
