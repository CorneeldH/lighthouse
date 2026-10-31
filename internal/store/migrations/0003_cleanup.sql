CREATE TABLE tick_state_new (
  repo TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('fingerprint', 'deferred', 'run', 'cleanup')),
  key TEXT NOT NULL, value TEXT NOT NULL, updated_at TEXT NOT NULL,
  PRIMARY KEY (repo, kind, key)
);
INSERT INTO tick_state_new (repo, kind, key, value, updated_at)
  SELECT repo, kind, key, value, updated_at FROM tick_state;
DROP TABLE tick_state;
ALTER TABLE tick_state_new RENAME TO tick_state;
