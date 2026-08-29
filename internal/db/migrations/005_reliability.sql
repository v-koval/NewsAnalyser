ALTER TABLE digest_runs
    ADD COLUMN IF NOT EXISTS mail_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mail_error TEXT NOT NULL DEFAULT '';

ALTER TABLE settings
    ADD COLUMN IF NOT EXISTS keep_runs_days INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_digest_runs_processed ON digest_runs(processed_at DESC);
CREATE INDEX IF NOT EXISTS idx_digest_runs_status ON digest_runs(status, processed_at DESC);
