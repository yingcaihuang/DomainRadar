# Implementation Plan: DomainRadar

## Overview

DomainRadar is a centralized domain asset management system built with Go (Gin) backend, React (TypeScript + Ant Design Pro) frontend, PostgreSQL, Redis, and who-dat for WHOIS lookups. Implementation proceeds from infrastructure through core services to frontend, with property-based tests validating correctness properties alongside implementation.

## Tasks

- [x] 1. Project scaffolding and Docker infrastructure
  - [x] 1.1 Initialize Go backend project structure
    - Create `backend/` directory with Go module (`go mod init domainradar`)
    - Set up directory layout: `cmd/server/`, `internal/` (sync, alert, monitor, whois, crypto, audit, auth, adapter, domain, notification, dashboard), `pkg/`, `migrations/`
    - Add initial `main.go` with Gin router placeholder and health endpoint (`GET /api/v1/system/health`)
    - Add `go.sum` dependencies: gin, gorm, redis, zap logger, rapid, testify
    - _Requirements: 14.1, 14.3_

  - [x] 1.2 Initialize React frontend project structure
    - Create `frontend/` directory with Vite + React + TypeScript scaffold
    - Install Ant Design Pro, TanStack Query, Zustand, React Router v6, Recharts, FullCalendar, Papa Parse, xlsx
    - Set up `src/` layout: `pages/`, `components/`, `hooks/`, `services/`, `store/`, `types/`
    - Configure TypeScript strict mode and ESLint
    - _Requirements: 14.4_

  - [x] 1.3 Create Docker configuration files
    - Create `docker-compose.yml` with all services: postgresql, redis, who-dat, backend, frontend
    - Configure internal network `domainradar-net`, named volumes (pg-data, redis-data, app-logs)
    - Set health checks for all containers (pg_isready, redis-cli ping, HTTP endpoints) with 30s interval and 60s start period
    - Configure dependency ordering: postgresql/redis → who-dat → backend → frontend
    - Set restart policies: `unless-stopped` for production
    - Expose only frontend (:443) and backend API (:8080) ports to host
    - Create `docker-compose.override.yml` for development (hot-reload, debug ports, volume-mounted source)
    - Create `.env.example` with all environment variables documented
    - _Requirements: 14.1, 14.2, 14.5, 14.6, 14.7, 14.8, 14.9, 14.10, 14.11, 14.12_

  - [x] 1.4 Create backend Dockerfile (multi-stage)
    - Build stage: golang base image, compile binary
    - Production stage: alpine base, copy binary, max 100MB uncompressed
    - Development stage: with hot-reload (air) and debug ports
    - _Requirements: 14.3_

  - [x] 1.5 Create frontend Dockerfile (multi-stage)
    - Build stage: node base, compile React app
    - Production stage: nginx:alpine, serve static assets, max 50MB uncompressed
    - Add nginx.conf with `/health` endpoint and `/api/*` proxy
    - _Requirements: 14.4_

  - [x] 1.6 Configure Redis container with persistence
    - Set AOF persistence, max memory 256MB (configurable via env), allkeys-lru eviction
    - Mount named volume for data persistence
    - _Requirements: 14.12_

- [x] 2. Database schema and migrations
  - [x] 2.1 Create PostgreSQL migration framework
    - Set up golang-migrate or goose for database migrations
    - Create initial migration with all tables: domains, registrar_configs, registrar_accounts, groups, tags, domain_tags, health_checks, certificate_checks, email_checks, alerts, notification_channels, notification_rules, notification_logs, users, user_roles, audit_logs, sync_logs
    - Define all indexes (domain_name unique, expiration_date, generated_at, etc.)
    - _Requirements: 1.4, 4.7, 9.1, 10.3_

  - [x] 2.2 Implement GORM models and database connection
    - Create Go structs for all database tables matching the ER diagram
    - Implement `NormalizedDomain`, `RegistrarAccount`, `RegistrarConfig`, `Group`, `Tag`, `Alert`, `NotificationChannel`, `NotificationRule`, `NotificationLog`, `User`, `UserRole`, `AuditLog`, `SyncLog`, `HealthCheck`, `CertificateCheck`, `EmailCheck`
    - Set up GORM database connection with connection pool configuration
    - Implement auto-migration for development environment
    - _Requirements: 1.4, 14.8_

- [x] 3. Checkpoint - Verify project builds and Docker starts
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Core infrastructure services
  - [x] 4.1 Implement Crypto Service (AES-256-GCM)
    - Create `internal/crypto/service.go` with `Encrypt()`, `Decrypt()`, and `MaskCredential()` methods
    - Load master key from `DOMAINRADAR_MASTER_KEY` environment variable at startup
    - Implement unique nonce generation per encryption operation
    - Implement `MaskCredential()`: show last 4 chars, mask remainder with asterisks; return "****" for strings ≤ 4 chars
    - _Requirements: 11.2, 11.3_

  - [x]* 4.2 Write property tests for Crypto Service
    - **Property 7: Credential encryption round-trip** — For any random UTF-8 string, Encrypt then Decrypt returns the original; ciphertext never contains plaintext as substring
    - **Property 8: Credential masking format** — For any string of length N>4, mask produces (N-4) asterisks + last 4 chars; for N≤4, produces "****"
    - **Validates: Requirements 11.2, 11.3**

  - [x] 4.3 Implement Audit Logger service
    - Create `internal/audit/service.go` with `RecordAction()` method
    - Record user_id, action_type (CREATE/UPDATE/DELETE), resource_type, resource_id, changed_fields (JSON diff with credentials masked), timestamp
    - Implement 365-day retention policy awareness
    - _Requirements: 10.3_

  - [x]* 4.4 Write property test for Audit Logger
    - **Property 30: Audit log completeness** — For any CUD operation with authenticated user, the audit entry contains all required fields with credentials masked
    - **Validates: Requirements 10.3**

  - [x] 4.5 Implement structured error handling
    - Create `internal/errors/` package with error codes, consistent JSON error format
    - Implement error response helper: `{"error": {"code": "...", "message": "...", "details": {...}, "request_id": "..."}}`
    - Create circuit breaker utility (`internal/circuitbreaker/`) with CLOSED/OPEN/HALF_OPEN states
    - Configure per-service thresholds: who-dat (3 failures, 90s cooldown), registrar APIs (5 failures, 5min cooldown), notification channels (3 failures, 60s cooldown)
    - _Requirements: 1.7, 1.9, 2.9_

  - [x]* 4.6 Write property test for circuit breaker
    - **Property 20: who-dat circuit breaker state transitions** — Pauses after exactly 3 consecutive failures; resumes on first success after being paused
    - **Validates: Requirements 2.9**

  - [x] 4.7 Implement Cache Service
    - Create `internal/cache/service.go` wrapping Redis client
    - Implement get/set/delete with TTL support
    - Implement graceful degradation when Redis is unavailable (fallback to no-cache)
    - _Requirements: 14.12_

- [x] 5. Authentication and authorization
  - [x] 5.1 Implement OIDC authentication with Authentik
    - Create `internal/auth/oidc.go` with OIDC client configuration
    - Implement `/api/v1/auth/login` (initiate OIDC flow), `/api/v1/auth/callback` (exchange code for tokens), `/api/v1/auth/logout`, `/api/v1/auth/me`
    - Implement session management with 60-second invalidation on Authentik revocation
    - Map Authentik groups/claims to internal roles (Viewer, Operator, Admin)
    - _Requirements: 10.1, 10.6_

  - [x] 5.2 Implement RBAC middleware
    - Create `internal/auth/rbac.go` with permission matrix (Viewer/Operator/Admin)
    - Implement Gin middleware that checks role permissions per endpoint
    - Return HTTP 403 for unauthorized access attempts
    - Enforce permissions: view domains (all), manage domains (Operator+), configure integrations (Admin), manage users (Admin), view/manage alerts (all/Operator+)
    - _Requirements: 10.2, 10.4_

  - [x]* 5.3 Write property test for RBAC enforcement
    - **Property 23: RBAC enforcement** — For any (role, endpoint) combination, return 403 iff role lacks required permission; decisions consistent with permission matrix
    - **Validates: Requirements 10.4**

  - [x] 5.4 Implement user management endpoints
    - Create `GET /api/v1/users` (list users with roles, last login, activity)
    - Create `PUT /api/v1/users/:id/roles` (update user roles)
    - Store user records from OIDC claims on first login
    - _Requirements: 10.5_

- [x] 6. Checkpoint - Verify auth and core infrastructure
  - Ensure all tests pass, ask the user if questions arise.

- [x] 7. Registrar adapter framework
  - [x] 7.1 Define RegistrarAdapter interface and registry
    - Create `internal/adapter/interface.go` with `RegistrarAdapter` interface: `ListDomains()`, `GetDomainDetail()`, `TestConnection()`, `RegistrarType()`
    - Create `AdapterRegistry` with `Register()` and `Get()` methods
    - Define `RegistrarCredential` struct for decrypted credential passing
    - _Requirements: 1.1_

  - [x] 7.2 Implement GoDaddy adapter
    - Create `internal/adapter/godaddy/adapter.go` implementing `RegistrarAdapter`
    - Support API Key + Secret and PAT authentication modes
    - Map GoDaddy API responses to `NormalizedDomain` format
    - Implement `TestConnection()` with 30-second timeout
    - _Requirements: 1.1, 1.2, 11.1_

  - [x] 7.3 Implement Cloudflare adapter
    - Create `internal/adapter/cloudflare/adapter.go` implementing `RegistrarAdapter`
    - Support API Token authentication
    - Map Cloudflare API responses to `NormalizedDomain` format
    - _Requirements: 1.1, 11.1_

  - [x] 7.4 Implement Alibaba Cloud adapter
    - Create `internal/adapter/alibaba/adapter.go` implementing `RegistrarAdapter`
    - Support AccessKey ID + Secret authentication
    - Map Alibaba Cloud domain API responses to `NormalizedDomain` format
    - _Requirements: 1.1, 11.1_

  - [x] 7.5 Implement Tencent Cloud adapter
    - Create `internal/adapter/tencent/adapter.go` implementing `RegistrarAdapter`
    - Support SecretId + SecretKey authentication
    - _Requirements: 1.1, 11.1_

  - [x] 7.6 Implement Namecheap adapter
    - Create `internal/adapter/namecheap/adapter.go` implementing `RegistrarAdapter`
    - Support API Key + Username + IP whitelist authentication
    - _Requirements: 1.1, 11.1_

  - [x] 7.7 Implement registrar configuration management endpoints
    - Create CRUD endpoints: `GET/POST/PUT/DELETE /api/v1/registrars`, `POST /api/v1/registrars/:id/test`
    - Implement credential encryption on save, masking on read (last 4 chars visible)
    - Implement connectivity test with 30-second timeout, persist "connected"/"disconnected" status
    - Validate required fields non-empty, credentials ≤ 512 chars, max 20 accounts per registrar type
    - Create registrar status page endpoint: `GET /api/v1/registrars/:id/status` (sync status, last sync, domain count, last 50 errors)
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6, 11.7, 11.8_

  - [x]* 7.8 Write property tests for registrar configuration
    - **Property 29: Registrar configuration validation** — Accept iff all required fields non-empty AND credentials ≤ 512 chars; enforce max 20 accounts per type
    - **Validates: Requirements 11.4, 11.8**

- [x] 8. Sync Scheduler
  - [x] 8.1 Implement Sync Scheduler core
    - Create `internal/sync/scheduler.go` with `RunSyncCycle()`, `CalculateSyncInterval()`, `ClampInterval()`
    - Implement smart frequency: >90 days = weekly, 30-90 = daily, <30 = every 12h
    - Implement override clamping: min 1 hour, max 30 days
    - Implement 10-minute max cycle duration with abort on timeout
    - Start sync scheduler as background goroutine with per-account scheduling
    - _Requirements: 1.3, 1.6, 12.1, 12.2, 12.3, 12.4_

  - [x] 8.2 Implement domain data merge logic
    - Create merge function that updates API-sourced fields while preserving user-defined fields (tags, notes, group)
    - Implement "unverified-removed" status after 2 consecutive absences (track absence count per domain)
    - Implement sync frequency recalculation within 5 minutes of detected expiration date change
    - Log all sync operations: start time, end time, domains synced/updated, errors
    - _Requirements: 1.5, 1.7, 1.8, 12.5, 12.6_

  - [x]* 8.3 Write property tests for Sync Scheduler
    - **Property 1: Sync frequency tier assignment** — Correct interval for all expiration distances
    - **Property 2: Sync interval override clamping** — Values clamped to [1h, 30d]
    - **Property 3: Domain data merge preserves user-defined fields** — Tags/notes/groups unchanged after merge
    - **Property 4: Domain removal requires two consecutive absences** — Status changes only after 2 consecutive absences
    - **Property 31: Sync error resilience** — All records unchanged after failed sync cycle
    - **Validates: Requirements 1.5, 1.6, 1.7, 1.8, 2.5, 12.1, 12.2, 12.3, 12.4, 13.4**

  - [x] 8.4 Implement manual sync trigger endpoint
    - Create `POST /api/v1/registrars/:id/sync` to trigger immediate sync for a registrar account
    - _Requirements: 1.3_

- [x] 9. WHOIS Worker with who-dat integration
  - [x] 9.1 Implement WHOIS Worker core
    - Create `internal/whois/worker.go` with Redis queue processing, rate limiter (2 req/sec)
    - Implement `QueryDomain()` calling who-dat REST API (`GET /<domain>`)
    - Parse who-dat JSON response into NormalizedDomain format (expiration date, registrar, creation date, nameservers)
    - Implement smart query frequency: >90d = weekly, 30-90d = daily, <30d = every 12h
    - _Requirements: 2.1, 2.2, 2.4, 2.5_

  - [x] 9.2 Implement WHOIS retry and circuit breaker logic
    - Implement exponential backoff: 2s, 4s, 8s with max 3 retries on 429/5xx/network errors
    - Implement who-dat health check on startup (log warning if unreachable, defer queries)
    - Implement circuit breaker: pause after 3 consecutive failed health checks (30s intervals), resume on success
    - _Requirements: 2.3, 2.7, 2.8, 2.9_

  - [x] 9.3 Implement WHOIS discrepancy detection
    - Compare WHOIS expiration date against manual entries, flag if difference > 24 hours
    - Display discrepancy in domain detail view
    - _Requirements: 2.6_

  - [x]* 9.4 Write property tests for WHOIS Worker
    - **Property 9: WHOIS exponential backoff timing** — Delay is 2^(N+1) seconds for retry N; marked failed after 3 retries
    - **Property 19: WHOIS expiration discrepancy detection** — Flagged iff absolute date difference > 24 hours
    - **Validates: Requirements 2.3, 2.6**

- [x] 10. Checkpoint - Verify sync and WHOIS workers
  - Ensure all tests pass, ask the user if questions arise.

- [x] 11. Alert Engine
  - [x] 11.1 Implement Alert Engine core
    - Create `internal/alert/engine.go` with `RunExpirationCheck()`, `CalculateSeverity()`
    - Evaluate all domain expiration dates once per 24-hour cycle, complete within 10 minutes
    - Assign severity: informational (90-31d), warning (30-8d), critical (7-0d), expired (past due)
    - Escalate severity by one level if auto-renew disabled and within 30 days
    - Generate alert records with: domain name, registrar, expiration date, days remaining, severity
    - Implement configurable thresholds (default: 90, 30, 14, 7, 3, 1 day)
    - _Requirements: 4.1, 4.2, 4.3, 4.4_

  - [x] 11.2 Implement alert delivery and retry logic
    - Deliver notifications to all users assigned to the domain via configured channels
    - Retry delivery up to 3 times with 5-minute intervals on failure
    - Mark delivery status as failed after all retries exhausted
    - Maintain alert history log with timestamps, recipients, delivery status (365-day retention)
    - _Requirements: 4.5, 4.7_

  - [x]* 11.3 Write property tests for Alert Engine
    - **Property 5: Alert severity assignment** — Correct severity for all (days_remaining, auto_renew) combinations
    - **Property 6: Alert threshold evaluation completeness** — Generated alerts contain all required fields
    - **Validates: Requirements 4.1, 4.2, 4.3, 4.4**

  - [x] 11.4 Implement alert API endpoints
    - Create `GET /api/v1/alerts` (list, filterable by severity/type/date), `GET /api/v1/alerts/:id`, `PUT /api/v1/alerts/:id/acknowledge`
    - _Requirements: 4.6, 4.7_

- [x] 12. Notification Dispatcher
  - [x] 12.1 Implement NotificationChannel interface and channel implementations
    - Create `internal/notification/dispatcher.go` with `NotificationChannel` interface (`Send()`, `TestConnection()`, `ChannelType()`)
    - Implement `EmailChannel` (SMTP), `WeChatWorkChannel` (bot token), `SMSChannel` (gateway), `WebhookChannel` (JSON payload)
    - Webhook payload: alert_severity, alert_type, triggered_at, domain_name, domain_url, message
    - _Requirements: 5.1, 5.6_

  - [x] 12.2 Implement notification routing and retry
    - Initiate delivery to all channels configured for alert's severity within 30 seconds
    - Implement exponential backoff: 5s, 10s, 20s with max 3 retries
    - Record failures with timestamp, channel, alert reference, error reason
    - Flag critical alerts as undelivered in dashboard if all channels fail; reattempt after 5 minutes
    - _Requirements: 5.2, 5.4, 5.7_

  - [x] 12.3 Implement notification channel configuration endpoints
    - Create CRUD for `/api/v1/notifications/channels` with credential masking
    - Implement connectivity test on save (result within 10 seconds)
    - Create CRUD for `/api/v1/notifications/rules` (map severity to channels, max 10 rules per domain)
    - _Requirements: 5.3, 5.5, 5.8_

  - [x]* 12.4 Write property tests for Notification Dispatcher
    - **Property 10: Notification delivery exponential backoff** — Delay is 5×2^N seconds for retry N
    - **Property 21: Notification severity-to-channel routing** — Deliver to exactly those channels configured for the severity
    - **Property 22: Webhook payload completeness** — JSON contains all required fields
    - **Validates: Requirements 5.2, 5.4, 5.5, 5.6**

- [x] 13. Health Monitor - Website availability
  - [x] 13.1 Implement website health checks
    - Create `internal/monitor/website.go` with `CheckWebsite()` method
    - Perform HTTP GET at configurable interval (1-60 min, default 5 min) with 30s timeout
    - Record HTTP status code, response time (ms), SSL validity, redirect chain (max 10 redirects)
    - Detect downtime after 3 consecutive failed checks (non-2xx or exceeded response time threshold)
    - Generate recovery notification with total downtime duration on state transition to available
    - Categorize failures: DNS resolution → "dns", connection timeout/network → "connectivity", non-2xx → "http_error"
    - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.6, 6.7_

  - [x] 13.2 Implement uptime calculation and monitoring endpoints
    - Calculate uptime percentage to two decimal places over selectable periods (24h, 7d, 30d)
    - Create `GET /api/v1/monitoring/websites/:domainId` and `GET /api/v1/monitoring/uptime/:domainId`
    - _Requirements: 6.5_

  - [x]* 13.3 Write property tests for website monitoring
    - **Property 11: Health check downtime detection** — Downtime alert iff 3+ consecutive failures
    - **Property 12: Uptime percentage calculation** — (successful/total) × 100, rounded to 2 decimal places
    - **Property 13: Health check failure categorization** — Mutually exclusive: dns, connectivity, http_error
    - **Property 14: Website downtime duration calculation** — Duration equals time from first failure to first success
    - **Validates: Requirements 6.3, 6.4, 6.5, 6.6, 6.7**

- [x] 14. Health Monitor - HTTPS certificates
  - [x] 14.1 Implement certificate monitoring
    - Create `internal/monitor/certificate.go` with `CheckCertificate()` method
    - Retrieve and parse SSL/TLS certificate at least once per day
    - Record issuer, subject, valid-from, valid-to, chain completeness, serial number
    - Generate expiration alerts at 30/14/7/3/1 days (same severity tiering as domain expiration)
    - Generate critical alert within 5 minutes for invalid chain, hostname mismatch, or revocation
    - Detect renewal by changed valid-to date or serial number; clear active certificate alerts on renewal
    - Log connection failures, retry next scheduled check without generating cert-specific alert
    - Create `GET /api/v1/monitoring/certificates/:domainId` endpoint
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7_

  - [x]* 14.2 Write property tests for certificate monitoring
    - **Property 24: Certificate renewal detection** — Renewal detected iff valid-to OR serial number changed; active alerts cleared
    - **Property 25: Certificate critical alert generation** — Critical alert for invalid chain/hostname mismatch/revocation regardless of expiry
    - **Validates: Requirements 7.4, 7.6**

- [x] 15. Health Monitor - Email services
  - [x] 15.1 Implement email service monitoring
    - Create `internal/monitor/email.go` with `CheckEmailService()` method
    - Query and validate MX records for email-enabled domains at least once per day
    - Check SPF, DKIM, DMARC DNS records for presence and syntactic validity (per RFC)
    - Generate compliance warning for missing SPF/DKIM/DMARC records
    - Detect MX record changes and generate warning alert
    - Generate critical alert if MX host doesn't respond on port 25 within 10 seconds (after 2 retries)
    - Calculate email compliance score: MX(25) + SPF(25) + DKIM(25) + DMARC(25) = 0-100
    - Create `GET /api/v1/monitoring/email/:domainId` endpoint
    - _Requirements: 8.1, 8.2, 8.3, 8.4, 8.5, 8.6_

  - [x]* 15.2 Write property tests for email monitoring
    - **Property 15: Email compliance score calculation** — Score = sum of (boolean × 25) for MX, SPF, DKIM, DMARC; range [0, 100]
    - **Property 26: MX record change detection** — Warning alert iff MX record sets differ between checks
    - **Validates: Requirements 8.4, 8.5**

- [x] 16. Health score calculation
  - [x] 16.1 Implement domain health score
    - Create `CalculateHealthScore()` in health monitor: expiration(30) + certificate(25) + uptime(25) + email(20) = 0-100
    - Update health score on each monitoring cycle
    - _Requirements: 9.3_

  - [x]* 16.2 Write property test for health score
    - **Property 16: Domain health score calculation** — Weighted sum in [0, 100] with correct weights
    - **Validates: Requirements 9.3**

- [x] 17. Checkpoint - Verify monitoring and alerting
  - Ensure all tests pass, ask the user if questions arise.

- [x] 18. Domain CRUD, import/export, and organization
  - [x] 18.1 Implement domain CRUD endpoints
    - Create `GET /api/v1/domains` (paginated, filterable by tags/groups/registrar/status/expiration range, response within 2s for 1000 domains)
    - Create `GET /api/v1/domains/:id`, `POST /api/v1/domains`, `PUT /api/v1/domains/:id`, `DELETE /api/v1/domains/:id`
    - Set data_source_type to "manual" for manually created domains; schedule WHOIS validation within 24 hours
    - _Requirements: 3.3, 3.4, 13.3_

  - [x] 18.2 Implement CSV/Excel import
    - Create `POST /api/v1/domains/import` accepting CSV and Excel files (max 10MB, max 5000 rows)
    - Validate required fields (domain name, expiration date) per row
    - Report row-level errors (row number, field name, reason) without rejecting entire file
    - Display import summary: total processed, created, updated, errors
    - _Requirements: 3.1, 3.2, 3.6_

  - [x] 18.3 Implement domain export
    - Create `GET /api/v1/domains/export` supporting CSV and Excel formats (max 10,000 rows)
    - Include tags and group membership in export output
    - _Requirements: 9.4, 13.6_

  - [x] 18.4 Implement bulk operations
    - Create `POST /api/v1/domains/bulk` for tag assignment, grouping, deletion on up to 500 domains
    - _Requirements: 3.5, 13.5_

  - [x] 18.5 Implement tags and groups management
    - Create CRUD for `/api/v1/tags` and `/api/v1/groups`
    - Enforce max 20 tags per domain, tag names 1-50 characters
    - Enforce hierarchical groups with max 3 levels of nesting
    - Preserve tags/groups during sync operations
    - _Requirements: 13.1, 13.2, 13.4_

  - [x]* 18.6 Write property tests for domain management
    - **Property 17: CSV/Excel import validation** — Valid rows create records, invalid rows report errors, sum equals total
    - **Property 18: Export data round-trip** — Exported CSV contains all fields including tags and groups
    - **Property 27: Tag and group constraint enforcement** — Rejects operations exceeding limits (20 tags, 50 char names, 3 levels)
    - **Property 28: Domain list filtering correctness** — Results include all and only domains matching ALL filter criteria
    - **Validates: Requirements 3.2, 3.6, 9.4, 13.1, 13.2, 13.3, 13.6**

- [x] 19. Dashboard and reporting APIs
  - [x] 19.1 Implement dashboard endpoint
    - Create `GET /api/v1/dashboard` returning: total domain count, domains expiring within 30 days, active alerts count, overall health score, domains by registrar
    - Render within 3 seconds for up to 1000 domains, data freshness within 5 minutes
    - Create `GET /api/v1/dashboard/health-scores` for per-domain scores
    - _Requirements: 9.1, 9.5_

  - [x] 19.2 Implement expiration calendar endpoint
    - Create `GET /api/v1/domains/calendar` returning domain and certificate expiration dates for monthly calendar view with severity indicators
    - _Requirements: 4.6, 9.2_

  - [x] 19.3 Implement cost statistics endpoint
    - Create `GET /api/v1/reports/costs` with renewal costs grouped by registrar, time period (monthly/quarterly/yearly), and custom tags
    - _Requirements: 9.6_

  - [x] 19.4 Implement audit log endpoint
    - Create `GET /api/v1/audit-logs` (admin only, paginated, filterable)
    - _Requirements: 10.3_

- [x] 20. Checkpoint - Verify all backend APIs
  - Ensure all tests pass, ask the user if questions arise.

- [x] 21. Frontend application - Core layout and auth
  - [x] 21.1 Implement app shell and routing
    - Set up React Router v6 with route structure: `/dashboard`, `/domains`, `/domains/:id`, `/calendar`, `/alerts`, `/settings/*`
    - Implement Ant Design Pro layout with sidebar navigation
    - Set up TanStack Query provider and Zustand store
    - _Requirements: 9.1, 10.1_

  - [x] 21.2 Implement authentication flow
    - Create Auth Provider with OIDC context
    - Implement login redirect to Authentik, callback handling, session management
    - Implement route guards based on user roles
    - _Requirements: 10.1, 10.4_

- [x] 22. Frontend - Domain management pages
  - [x] 22.1 Implement domain list page
    - Create domain table with pagination, sorting, and filtering (tags, groups, registrar, status, expiration range)
    - Implement bulk action bar (tag assignment, grouping, deletion) for up to 500 selections
    - Add health badge, severity tag, and status indicators
    - Implement filter panel component
    - _Requirements: 3.5, 13.3, 13.5_

  - [x] 22.2 Implement domain detail page
    - Display all NormalizedDomain fields, health score, website uptime, certificate status, email compliance
    - Show WHOIS discrepancy warnings when applicable
    - Display tags, group membership, and notes (editable)
    - _Requirements: 2.6, 7.5, 9.3_

  - [x] 22.3 Implement domain create/edit form
    - Create web form with all NormalizedDomain fields
    - Client-side validation for required fields
    - _Requirements: 3.3_

  - [x] 22.4 Implement CSV/Excel import modal
    - File upload (max 10MB), parsing with Papa Parse (CSV) and xlsx (Excel)
    - Display validation errors per row
    - Show import summary after completion
    - _Requirements: 3.1, 3.2, 3.6_

- [x] 23. Frontend - Monitoring and alerts pages
  - [x] 23.1 Implement dashboard page
    - Display summary statistics: total domains, expiring within 30d, active alerts, health score
    - Domains grouped by registrar (pie/bar chart via Recharts)
    - Cost statistics with time period selectors
    - Render within 3 seconds
    - _Requirements: 9.1, 9.5, 9.6_

  - [x] 23.2 Implement expiration calendar page
    - Use FullCalendar to display domain and certificate expiration dates
    - Color-code by severity level (informational/warning/critical/expired)
    - _Requirements: 4.6, 9.2_

  - [x] 23.3 Implement alerts page
    - List alerts with filtering by severity, type, date range
    - Acknowledge alerts from list or detail view
    - Show delivery status and notification history
    - _Requirements: 4.6, 4.7_

  - [x] 23.4 Implement uptime and response time charts
    - Display uptime percentage and response time trends using Recharts
    - Selectable time periods: 24h, 7d, 30d
    - _Requirements: 6.5_

- [x] 24. Frontend - Settings pages
  - [x] 24.1 Implement registrar configuration page
    - Configuration forms per registrar type with appropriate credential fields
    - Credential masking (show last 4 chars after save)
    - Connectivity test button with result display
    - Sync status, last sync time, domain count, recent errors per account
    - _Requirements: 11.1, 11.3, 11.5, 11.7_

  - [x] 24.2 Implement notification channel configuration page
    - Forms for email (SMTP), WeChat Work (bot token), SMS (gateway), Webhook (URLs)
    - Mask sensitive values after saving
    - Test connectivity button
    - Notification rules: map severity to channels (max 10 rules per domain)
    - _Requirements: 5.3, 5.5, 5.8_

  - [x] 24.3 Implement user management page
    - List users with roles, last login, activity summary
    - Role assignment interface (Viewer/Operator/Admin)
    - _Requirements: 10.2, 10.5_

  - [x] 24.4 Implement audit log page
    - Paginated audit log viewer with filters (user, action type, resource, date range)
    - _Requirements: 10.3_

- [x] 25. Checkpoint - Verify frontend application
  - Ensure all tests pass, ask the user if questions arise.

- [x] 26. Integration tests
  - [x] 26.1 Write backend integration tests
    - Set up testcontainers-go for PostgreSQL and Redis
    - Test full sync cycle with mocked registrar APIs
    - Test WHOIS query processing with mocked who-dat
    - Test notification delivery with mocked channels
    - Test OIDC authentication flow with mocked Authentik
    - Test database migrations and schema validation
    - Test end-to-end API flows (create registrar → sync → view domains → generate alerts)
    - _Requirements: 1.2, 1.3, 2.1, 5.2, 10.1_

  - [x] 26.2 Write frontend tests
    - Set up Vitest + React Testing Library for component tests
    - Test form validation and submission flows
    - Test filter and search interactions
    - Set up Playwright for E2E tests
    - Test key user journeys: login → view domains → acknowledge alert
    - _Requirements: 9.5, 10.1_

- [x] 27. Production Docker optimization
  - [x] 27.1 Optimize Docker builds for production
    - Verify backend image ≤ 100MB uncompressed (alpine base, minimal dependencies)
    - Verify frontend image ≤ 50MB uncompressed (nginx:alpine with static assets only)
    - Ensure `docker-compose up` reaches healthy state within 120 seconds
    - Test container restart policies and health check recovery
    - Verify dependent service restart behavior (max 3 restarts in 5-minute window)
    - _Requirements: 14.3, 14.4, 14.9, 14.10, 14.11_

- [x] 28. Final checkpoint - Full system verification
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate the 31 universal correctness properties from the design document
- Unit tests validate specific examples and edge cases
- The Go `rapid` library is used for property-based testing; `testify` for assertions
- Frontend uses Vitest + React Testing Library for unit/component tests and Playwright for E2E
- All registrar adapters follow the same interface pattern — additional registrars (e.g., Xinnet) can be added later using the same template
- The who-dat service is a third-party container (lissy93/who-dat) — no implementation needed, only integration

## Task Dependency Graph

```json
{
  "waves": [
    { "id": 0, "tasks": ["1.1", "1.2"] },
    { "id": 1, "tasks": ["1.3", "1.4", "1.5", "1.6"] },
    { "id": 2, "tasks": ["2.1", "2.2"] },
    { "id": 3, "tasks": ["4.1", "4.3", "4.5", "4.7"] },
    { "id": 4, "tasks": ["4.2", "4.4", "4.6"] },
    { "id": 5, "tasks": ["5.1", "5.2"] },
    { "id": 6, "tasks": ["5.3", "5.4"] },
    { "id": 7, "tasks": ["7.1"] },
    { "id": 8, "tasks": ["7.2", "7.3", "7.4", "7.5", "7.6"] },
    { "id": 9, "tasks": ["7.7", "7.8"] },
    { "id": 10, "tasks": ["8.1", "9.1"] },
    { "id": 11, "tasks": ["8.2", "8.4", "9.2", "9.3"] },
    { "id": 12, "tasks": ["8.3", "9.4"] },
    { "id": 13, "tasks": ["11.1", "12.1"] },
    { "id": 14, "tasks": ["11.2", "11.4", "12.2", "12.3"] },
    { "id": 15, "tasks": ["11.3", "12.4"] },
    { "id": 16, "tasks": ["13.1", "14.1", "15.1"] },
    { "id": 17, "tasks": ["13.2", "13.3", "14.2", "15.2", "16.1"] },
    { "id": 18, "tasks": ["16.2"] },
    { "id": 19, "tasks": ["18.1", "18.2", "18.3", "18.4", "18.5"] },
    { "id": 20, "tasks": ["18.6"] },
    { "id": 21, "tasks": ["19.1", "19.2", "19.3", "19.4"] },
    { "id": 22, "tasks": ["21.1", "21.2"] },
    { "id": 23, "tasks": ["22.1", "22.2", "22.3", "22.4"] },
    { "id": 24, "tasks": ["23.1", "23.2", "23.3", "23.4"] },
    { "id": 25, "tasks": ["24.1", "24.2", "24.3", "24.4"] },
    { "id": 26, "tasks": ["26.1", "26.2"] },
    { "id": 27, "tasks": ["27.1"] }
  ]
}
```
