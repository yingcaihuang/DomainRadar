-- DomainRadar Initial Schema Rollback
-- Drops all tables in reverse dependency order

BEGIN;

DROP TABLE IF EXISTS sync_logs;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS notification_logs;
DROP TABLE IF EXISTS notification_rules;
DROP TABLE IF EXISTS notification_channels;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS email_checks;
DROP TABLE IF EXISTS certificate_checks;
DROP TABLE IF EXISTS health_checks;
DROP TABLE IF EXISTS domain_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS domains;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS registrar_accounts;
DROP TABLE IF EXISTS registrar_configs;

COMMIT;
