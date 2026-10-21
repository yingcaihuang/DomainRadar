ALTER TABLE registrar_accounts
    DROP COLUMN IF EXISTS full_import_enabled,
    DROP COLUMN IF EXISTS auto_sync_enabled;
