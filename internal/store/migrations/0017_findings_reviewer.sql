-- Which reviewer found a finding and how severe each one rated it. findings.source stays the
-- telemetry origin; all three columns stay NULL for a finding without a reviewer source
-- (a verification round) and severity_* stay NULL for a reviewer that did not rate it.
ALTER TABLE findings ADD COLUMN reviewer_source TEXT CHECK (reviewer_source IS NULL OR reviewer_source IN ('codex', 'fallback', 'both'));
ALTER TABLE findings ADD COLUMN severity_codex TEXT;
ALTER TABLE findings ADD COLUMN severity_fallback TEXT;
