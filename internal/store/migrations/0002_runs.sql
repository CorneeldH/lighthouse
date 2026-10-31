CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  repo TEXT NOT NULL, issue INTEGER NOT NULL,
  session_id TEXT NOT NULL,
  mode TEXT NOT NULL CHECK (mode IN ('start', 'resume')),
  trigger TEXT NOT NULL CHECK (trigger IN ('handoff', 'answer', 'usage', 'closecheck', 'manual')),
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
CREATE UNIQUE INDEX runs_open_issue ON runs(repo, issue) WHERE ended_at IS NULL;
CREATE INDEX runs_trigger ON runs(repo, trigger_key);
CREATE TABLE action_failures (
  action_key TEXT PRIMARY KEY, error TEXT NOT NULL,
  first_at TEXT NOT NULL, last_at TEXT NOT NULL
);
CREATE TABLE tick_state (
  repo TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('fingerprint', 'deferred', 'run')),
  key TEXT NOT NULL, value TEXT NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY (repo, kind, key)
);
