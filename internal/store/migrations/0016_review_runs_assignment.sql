-- What a review run did and which experiment arm its trajectory was in. review_mode and reviewers
-- stay NULL for a record that does not carry them; assignment follows trajectories.assignment.
ALTER TABLE review_runs ADD COLUMN review_mode TEXT;
ALTER TABLE review_runs ADD COLUMN reviewers TEXT;       -- JSON array, e.g. ["codex","opus"]
ALTER TABLE review_runs ADD COLUMN assignment TEXT NOT NULL DEFAULT 'standard'
  CHECK (assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*');
ALTER TABLE review_runs ADD COLUMN assignment_applied INTEGER CHECK (assignment_applied IS NULL OR assignment_applied IN (0, 1));
