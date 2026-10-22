BEGIN;

ALTER TABLE alerts
    ADD COLUMN IF NOT EXISTS registrar_account_id BIGINT,
    ALTER COLUMN domain_id DROP NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'alerts'::regclass
          AND conname = 'fk_alerts_registrar_account'
    ) THEN
        ALTER TABLE alerts
            ADD CONSTRAINT fk_alerts_registrar_account
            FOREIGN KEY (registrar_account_id) REFERENCES registrar_accounts(id) ON DELETE CASCADE;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_alerts_registrar_account_id ON alerts(registrar_account_id);

COMMIT;
