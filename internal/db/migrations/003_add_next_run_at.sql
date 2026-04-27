ALTER TABLE digests
    ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ;

UPDATE digests
SET next_run_at = COALESCE(last_run_at, now()) + (frequency_hours * interval '1 hour')
WHERE next_run_at IS NULL;
