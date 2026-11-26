-- The explore/exploit assignment of a trajectory, fixed at its first claim and copied into every run.
-- NULL until then; the CHECK has the same form as runs.assignment.
ALTER TABLE trajectories ADD COLUMN assignment TEXT
  CHECK (assignment IS NULL OR assignment = 'standard' OR assignment GLOB 'shadow:?*' OR assignment GLOB 'variant:?*=?*');
ALTER TABLE trajectories ADD COLUMN experiment_id TEXT;
UPDATE trajectories SET assignment = 'standard';
