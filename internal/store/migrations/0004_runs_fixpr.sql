CREATE TABLE runs_new (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL, issue INTEGER NOT NULL,
  session_id TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('start', 'resume')),
  trigger TEXT NOT NULL CHECK (trigger IN ('handoff', 'answer', 'usage', 'closecheck', 'manual', 'fixpr')),
  trigger_key TEXT NOT NULL,
  attempt INTEGER NOT NULL CHECK (attempt IN (1, 2)),
  command TEXT NOT NULL DEFAULT '', prompt TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '', workspace_id TEXT NOT NULL DEFAULT '',
  workplace_session TEXT NOT NULL DEFAULT '', profile TEXT NOT NULL DEFAULT '',
  started_at TEXT NOT NULL, ended_at TEXT,
  outcome TEXT CHECK (outcome IN ('start_failed', 'handoff', 'question', 'parked', 'done', 'returned')),
  reason TEXT NOT NULL DEFAULT '',
  CHECK ((ended_at IS NULL) = (outcome IS NULL))
);
INSERT INTO runs_new (id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
    branch, workspace_id, workplace_session, profile, started_at, ended_at, outcome, reason)
  SELECT id, repo, issue, session_id, mode, trigger, trigger_key, attempt, command, prompt,
    branch, workspace_id, workplace_session, profile, started_at, ended_at, outcome, reason FROM runs;
DROP TABLE runs;
ALTER TABLE runs_new RENAME TO runs;
CREATE UNIQUE INDEX runs_open_issue ON runs(repo, issue) WHERE ended_at IS NULL;
CREATE INDEX runs_trigger ON runs(repo, trigger_key);
