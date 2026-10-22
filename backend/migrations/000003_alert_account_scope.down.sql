BEGIN;

ALTER TABLE alerts ALTER COLUMN domain_id SET NOT NULL;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS fk_alerts_registrar_account;
DROP INDEX IF EXISTS idx_alerts_registrar_account_id;
ALTER TABLE alerts DROP COLUMN IF EXISTS registrar_account_id;

COMMIT;
