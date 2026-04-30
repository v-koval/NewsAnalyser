ALTER TABLE digests
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'news';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.table_constraints
        WHERE table_name = 'digests' AND constraint_name = 'digests_kind_check'
    ) THEN
        ALTER TABLE digests
            ADD CONSTRAINT digests_kind_check CHECK (kind IN ('news', 'facts'));
    END IF;
END$$;
