-- DomainRadar Initial Schema Migration
-- Creates all core tables for domain asset management

BEGIN;

-- =============================================================================
-- registrar_configs: defines supported registrar types
-- =============================================================================
CREATE TABLE registrar_configs (
    id BIGSERIAL PRIMARY KEY,
    registrar_type VARCHAR(50) NOT NULL,
    display_name VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- registrar_accounts: holds encrypted credentials for registrar accounts
-- =============================================================================
CREATE TABLE registrar_accounts (
    id BIGSERIAL PRIMARY KEY,
    registrar_config_id BIGINT NOT NULL REFERENCES registrar_configs(id) ON DELETE CASCADE,
    account_name VARCHAR(100) NOT NULL,
    credentials_encrypted TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'disconnected',
    sync_interval_hours INT NOT NULL DEFAULT 24,
    last_sync_at TIMESTAMPTZ,
    domain_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_registrar_accounts_config_id ON registrar_accounts(registrar_config_id);

-- =============================================================================
-- groups: hierarchical domain grouping (max 3 levels)
-- =============================================================================
CREATE TABLE groups (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    parent_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    level INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_groups_parent_id ON groups(parent_id);

-- =============================================================================
-- domains: the core normalized domain records
-- =============================================================================
CREATE TABLE domains (
    id BIGSERIAL PRIMARY KEY,
    domain_name VARCHAR(253) NOT NULL,
    registrar_account_id BIGINT REFERENCES registrar_accounts(id) ON DELETE SET NULL,
    registrar_identifier VARCHAR(100),
    creation_date TIMESTAMPTZ,
    expiration_date TIMESTAMPTZ,
    auto_renew BOOLEAN NOT NULL DEFAULT FALSE,
    renewal_deadline TIMESTAMPTZ,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    nameservers TEXT[],
    privacy_protection BOOLEAN NOT NULL DEFAULT FALSE,
    lock_status BOOLEAN NOT NULL DEFAULT FALSE,
    data_source_type VARCHAR(20) NOT NULL DEFAULT 'manual',
    last_sync_at TIMESTAMPTZ,
    group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    notes TEXT,
    website_url VARCHAR(2048),
    email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    health_score INT NOT NULL DEFAULT 100,
    check_interval_minutes INT NOT NULL DEFAULT 5,
    response_time_threshold_ms INT NOT NULL DEFAULT 10000,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_domains_domain_name ON domains(domain_name);
CREATE INDEX idx_domains_expiration_date ON domains(expiration_date);
CREATE INDEX idx_domains_registrar_account_id ON domains(registrar_account_id);
CREATE INDEX idx_domains_group_id ON domains(group_id);
CREATE INDEX idx_domains_status ON domains(status);
CREATE INDEX idx_domains_data_source_type ON domains(data_source_type);

-- =============================================================================
-- tags: user-defined labels for domains
-- =============================================================================
CREATE TABLE tags (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_tags_name ON tags(name);

-- =============================================================================
-- domain_tags: many-to-many relationship between domains and tags
-- =============================================================================
CREATE TABLE domain_tags (
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (domain_id, tag_id)
);

CREATE INDEX idx_domain_tags_tag_id ON domain_tags(tag_id);

-- =============================================================================
-- health_checks: website availability check results
-- =============================================================================
CREATE TABLE health_checks (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    http_status_code INT,
    response_time_ms INT,
    ssl_valid BOOLEAN,
    ssl_expiry TIMESTAMPTZ,
    redirect_chain TEXT,
    check_type VARCHAR(50),
    failure_category VARCHAR(50),
    failure_detail TEXT,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_health_checks_domain_id ON health_checks(domain_id);
CREATE INDEX idx_health_checks_checked_at ON health_checks(checked_at);

-- =============================================================================
-- certificate_checks: SSL/TLS certificate monitoring results
-- =============================================================================
CREATE TABLE certificate_checks (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    issuer VARCHAR(255),
    subject VARCHAR(255),
    valid_from TIMESTAMPTZ,
    valid_to TIMESTAMPTZ,
    chain_complete BOOLEAN,
    serial_number VARCHAR(255),
    days_remaining INT,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_certificate_checks_domain_id ON certificate_checks(domain_id);
CREATE INDEX idx_certificate_checks_checked_at ON certificate_checks(checked_at);

-- =============================================================================
-- email_checks: email service compliance monitoring results
-- =============================================================================
CREATE TABLE email_checks (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    mx_records TEXT,
    spf_valid BOOLEAN,
    dkim_valid BOOLEAN,
    dmarc_valid BOOLEAN,
    compliance_score INT,
    mx_previous TEXT,
    mx_changed BOOLEAN,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_email_checks_domain_id ON email_checks(domain_id);
CREATE INDEX idx_email_checks_checked_at ON email_checks(checked_at);

-- =============================================================================
-- alerts: generated alert records for domain events
-- =============================================================================
CREATE TABLE alerts (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    alert_type VARCHAR(50) NOT NULL,
    severity VARCHAR(20) NOT NULL,
    message TEXT,
    days_remaining INT,
    acknowledged BOOLEAN NOT NULL DEFAULT FALSE,
    delivery_status VARCHAR(20) NOT NULL DEFAULT 'pending',
    generated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acknowledged_at TIMESTAMPTZ
);

CREATE INDEX idx_alerts_domain_id ON alerts(domain_id);
CREATE INDEX idx_alerts_generated_at ON alerts(generated_at);
CREATE INDEX idx_alerts_severity ON alerts(severity);
CREATE INDEX idx_alerts_acknowledged ON alerts(acknowledged);

-- =============================================================================
-- notification_channels: configured delivery channels (email, wechat, sms, webhook)
-- =============================================================================
CREATE TABLE notification_channels (
    id BIGSERIAL PRIMARY KEY,
    channel_type VARCHAR(50) NOT NULL,
    name VARCHAR(100) NOT NULL,
    config_encrypted TEXT NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'inactive',
    last_tested_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- =============================================================================
-- notification_rules: map alerts to notification channels by severity
-- =============================================================================
CREATE TABLE notification_rules (
    id BIGSERIAL PRIMARY KEY,
    domain_id BIGINT REFERENCES domains(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    severity_filter VARCHAR(100) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_rules_domain_id ON notification_rules(domain_id);
CREATE INDEX idx_notification_rules_channel_id ON notification_rules(channel_id);

-- =============================================================================
-- notification_logs: delivery attempt history
-- =============================================================================
CREATE TABLE notification_logs (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    channel_id BIGINT NOT NULL REFERENCES notification_channels(id) ON DELETE CASCADE,
    status VARCHAR(20) NOT NULL,
    error_reason TEXT,
    retry_count INT NOT NULL DEFAULT 0,
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_logs_alert_id ON notification_logs(alert_id);
CREATE INDEX idx_notification_logs_channel_id ON notification_logs(channel_id);

-- =============================================================================
-- users: user accounts synced from Authentik SSO
-- =============================================================================
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    external_id VARCHAR(255) NOT NULL,
    email VARCHAR(255),
    display_name VARCHAR(255),
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_users_external_id ON users(external_id);

-- =============================================================================
-- user_roles: role assignments for RBAC
-- =============================================================================
CREATE TABLE user_roles (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_user_roles_user_id ON user_roles(user_id);

-- =============================================================================
-- audit_logs: action audit trail (365-day retention)
-- =============================================================================
CREATE TABLE audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action_type VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    resource_id VARCHAR(100),
    changed_fields JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX idx_audit_logs_resource_type ON audit_logs(resource_type);

-- =============================================================================
-- sync_logs: registrar sync operation history
-- =============================================================================
CREATE TABLE sync_logs (
    id BIGSERIAL PRIMARY KEY,
    registrar_account_id BIGINT NOT NULL REFERENCES registrar_accounts(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    domains_synced INT NOT NULL DEFAULT 0,
    domains_updated INT NOT NULL DEFAULT 0,
    status VARCHAR(20) NOT NULL DEFAULT 'running',
    error_message TEXT
);

CREATE INDEX idx_sync_logs_registrar_account_id ON sync_logs(registrar_account_id);
CREATE INDEX idx_sync_logs_started_at ON sync_logs(started_at);

COMMIT;
