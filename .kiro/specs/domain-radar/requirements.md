# Requirements Document

## Introduction

DomainRadar is a domain asset management system designed to centrally manage hundreds of domains across multiple registrars (GoDaddy, Alibaba Cloud, Tencent Cloud, Cloudflare, Namecheap, Xinnet, etc.). The system provides unified monitoring for domain expiration, website availability, HTTPS certificates, and email services, with multi-channel alerting and role-based access control. The backend is built with Golang, the frontend with React, and authentication is handled via Authentik SSO integration.

## Glossary

- **DomainRadar**: The domain asset management system being specified
- **Registrar_Adapter**: A plugin component that implements the data synchronization interface for a specific domain registrar
- **Sync_Scheduler**: The component responsible for scheduling and executing domain data synchronization tasks across all registrars
- **WHOIS_Worker**: The queue-based worker that performs rate-limited WHOIS/RDAP queries via the who-dat service for registrars without API access
- **Who_Dat_Service**: The self-hosted Docker container running who-dat (https://github.com/Lissy93/who-dat) that provides a unified REST API for RDAP/WHOIS lookups
- **Container_Orchestrator**: The docker-compose configuration that defines and manages all DomainRadar service containers and their dependencies
- **Alert_Engine**: The component that evaluates alert rules against monitored asset states and dispatches notifications
- **Notification_Dispatcher**: The component responsible for sending alerts through configured channels (email, WeChat Work, SMS, Webhook)
- **Health_Monitor**: The component that performs periodic checks on websites, certificates, and email services
- **Admin_Panel**: The backend web interface for managing system configuration, integrar credentials, and user permissions
- **Normalized_Domain**: The unified data model representing a domain asset regardless of its source registrar
- **Data_Source_Priority**: The hierarchy determining which data source is authoritative for a given domain (API > WHOIS > Manual)
- **Authentik**: The external SSO identity provider used for user authentication

## Requirements

### Requirement 1: Multi-Registrar Domain Data Synchronization

**User Story:** As a domain administrator, I want the system to automatically synchronize domain data from multiple registrars through their APIs, so that I always have up-to-date information without manual data entry.

#### Acceptance Criteria

1. THE Registrar_Adapter SHALL implement a unified interface exposing `ListDomains()` and `GetDomainDetail(domain)` methods that return Normalized_Domain objects
2. WHEN a registrar API credential is configured via the Admin_Panel, THE Sync_Scheduler SHALL validate the credential by performing a test API call within 30 seconds and report success or failure to the administrator
3. WHEN the Sync_Scheduler executes a sync cycle, THE Registrar_Adapter SHALL retrieve all active domains from the configured registrar account and persist them as Normalized_Domain records within a maximum cycle duration of 10 minutes per registrar account
4. THE Normalized_Domain SHALL contain at minimum: domain name, registrar identifier, registrar account, creation date, expiration date, auto-renew status, renewal deadline, domain status, nameservers, privacy protection status, lock status, data source type, and last sync timestamp
5. WHEN a domain exists in both API data and local database, THE Sync_Scheduler SHALL update the local record with API data without overwriting user-defined fields (tags, notes, grouping)
6. THE Sync_Scheduler SHALL support configurable sync frequency per registrar account with a minimum interval of 1 hour, a maximum interval of 7 days, and a default of once per day
7. IF a registrar API returns an error or is unreachable during a sync cycle, THEN THE Sync_Scheduler SHALL retain the existing local domain records unchanged, log the failure with the registrar identifier and error reason, and retry the sync at the next scheduled interval
8. WHEN a domain present in the local database is no longer returned by the registrar API for two consecutive successful sync cycles, THE Sync_Scheduler SHALL mark the domain status as "unverified-removed" without deleting the record
9. IF a sync cycle exceeds the maximum cycle duration, THEN THE Sync_Scheduler SHALL abort the cycle, retain all previously synced records unchanged, and report a timeout failure to the administrator

### Requirement 2: WHOIS/RDAP Fallback Data Collection via who-dat

**User Story:** As a domain administrator, I want the system to query WHOIS/RDAP for domains hosted at registrars without API support, so that I can still track expiration dates automatically.

#### Acceptance Criteria

1. WHEN a domain is marked with data source type "whois", THE WHOIS_Worker SHALL query its expiration information by calling the self-hosted who-dat service REST API endpoint (`GET /<domain>`) which internally queries RDAP first and falls back to WHOIS
2. THE WHOIS_Worker SHALL process queries through a rate-limited queue at a maximum rate of 2 requests per second to avoid upstream rate limiting
3. IF a who-dat API call fails due to HTTP 429, 5xx response, or network error, THEN THE WHOIS_Worker SHALL retry the query using exponential backoff starting at 2 seconds (2s, 4s, 8s) with a maximum of 3 retries, after which the query is marked as failed and the domain retains its previous expiration data
4. THE WHOIS_Worker SHALL parse the structured JSON response from who-dat into the Normalized_Domain format, extracting at minimum: expiration date, registrar name, creation date, and name servers
5. WHILE a domain has more than 90 days until expiration, THE WHOIS_Worker SHALL query it once per week; WHILE a domain has 30 to 90 days until expiration, THE WHOIS_Worker SHALL query it once per day; WHILE a domain has fewer than 30 days until expiration, THE WHOIS_Worker SHALL query it every 12 hours
6. WHEN a manually entered domain has WHOIS data available, THE WHOIS_Worker SHALL compare the WHOIS expiration date against the manual entry and flag a discrepancy if the difference exceeds 24 hours, displaying the discrepancy in the domain detail view
7. THE WHOIS_Worker SHALL depend on the who-dat service (https://github.com/Lissy93/who-dat) deployed as a self-hosted Docker container accessible via internal container networking at a configurable base URL (default: `http://who-dat:8080`)
8. WHEN the DomainRadar application starts, THE WHOIS_Worker SHALL verify who-dat service availability by calling the who-dat health endpoint and SHALL log a startup warning if the service is unreachable, deferring WHOIS queries until the service becomes available
9. IF the who-dat service becomes unreachable during operation (3 consecutive failed health checks at 30-second intervals), THEN THE WHOIS_Worker SHALL pause all WHOIS query processing and resume automatically when the service health check succeeds again

### Requirement 3: Manual Domain Data Management

**User Story:** As a domain administrator, I want to manually import and manage domain records via CSV/Excel upload and web UI entry, so that I can track domains that cannot be synced automatically.

#### Acceptance Criteria

1. WHEN an administrator uploads a CSV or Excel file (maximum file size 10MB, maximum 5000 rows), THE DomainRadar SHALL parse the file and create Normalized_Domain records for each valid row
2. WHEN parsing an import file, THE DomainRadar SHALL validate required fields (domain name, expiration date) and report row-level errors (row number, field name, reason) for invalid entries without rejecting the entire file
3. THE Admin_Panel SHALL provide a web form for manually creating and editing individual domain records with all Normalized_Domain fields available
4. WHEN a domain record is created manually, THE DomainRadar SHALL set its data source type to "manual" and schedule WHOIS validation within 24 hours
5. THE DomainRadar SHALL support bulk operations (tag assignment, grouping, deletion) on up to 500 selected domain records from the domain list view in a single operation
6. WHEN a bulk import completes, THE DomainRadar SHALL display a summary showing total rows processed, records created, records updated (for existing domains), and rows with errors

### Requirement 4: Domain Expiration Monitoring and Alerting

**User Story:** As a domain administrator, I want to receive tiered alerts before domains expire, so that I can renew them in time and avoid losing ownership.

#### Acceptance Criteria

1. THE Alert_Engine SHALL evaluate all domain expiration dates once per 24-hour cycle and generate alerts at configurable thresholds (default: 90, 30, 14, 7, 3, and 1 day before expiration), completing the evaluation of all monitored domains within 10 minutes of cycle start
2. WHEN a domain reaches an alert threshold, THE Alert_Engine SHALL create an alert record containing domain name, registrar, expiration date, days remaining, and alert severity level, and SHALL deliver a notification to all users assigned to that domain via at least one configured channel
3. THE Alert_Engine SHALL assign severity levels based on proximity: informational (90-31 days), warning (30-8 days), critical (7-0 days), and expired (past due)
4. WHEN a domain has auto-renew disabled and is within 30 days of expiration, THE Alert_Engine SHALL escalate the alert severity by one level
5. IF alert notification delivery fails for a configured channel, THEN THE Alert_Engine SHALL retry delivery up to 3 times with a minimum interval of 5 minutes between attempts and SHALL mark the delivery status as failed after all retries are exhausted
6. THE DomainRadar SHALL display an expiration calendar view showing all domains with their expiration dates and visually distinct severity indicators corresponding to the four severity levels defined in criterion 3
7. THE DomainRadar SHALL maintain an alert history log with timestamps, recipients, and delivery status for each notification sent, retaining records for a minimum of 365 days

### Requirement 5: Multi-Channel Notification Dispatch

**User Story:** As a domain administrator, I want to receive alerts through multiple channels (email, WeChat Work, SMS, Webhook), so that I am notified in the most appropriate way based on urgency.

#### Acceptance Criteria

1. THE Notification_Dispatcher SHALL support sending notifications via email, WeChat Work (Enterprise WeChat), SMS, and Webhook channels
2. WHEN an alert is generated, THE Notification_Dispatcher SHALL initiate delivery to all channels configured for the alert's severity level within 30 seconds of alert generation
3. THE Admin_Panel SHALL provide configuration forms for each notification channel with credential fields (SMTP settings, WeChat Work bot token, SMS gateway credentials, Webhook URLs) that mask sensitive values after saving and allow updating or deleting stored credentials
4. WHEN a notification delivery fails, THE Notification_Dispatcher SHALL retry delivery up to 3 times with exponential backoff starting at 5 seconds (5s, 10s, 20s) and record the failure with timestamp, channel, alert reference, and error reason in the notification log visible to the administrator
5. THE Admin_Panel SHALL support notification rules that map alert severity levels (critical, warning, informational) to one or more specific channels, with a maximum of 10 rules per domain
6. WHERE a Webhook channel is configured, THE Notification_Dispatcher SHALL send a JSON payload conforming to a documented schema containing at minimum: alert severity, alert type, triggered timestamp, affected domain name, and a direct URL link to the domain in DomainRadar
7. IF delivery fails on all configured channels for a critical-severity alert after all retry attempts are exhausted, THEN THE Notification_Dispatcher SHALL flag the alert as undelivered in the Admin_Panel dashboard and reattempt delivery once after 5 minutes
8. WHEN an administrator saves channel credentials in the Admin_Panel, THE Notification_Dispatcher SHALL perform a connectivity test against the configured endpoint and display a success or failure result within 10 seconds

### Requirement 6: Website Availability Monitoring

**User Story:** As a domain administrator, I want to monitor the HTTP availability and response time of websites associated with my domains, so that I can detect and respond to outages promptly.

#### Acceptance Criteria

1. WHEN a domain has an associated website URL configured, THE Health_Monitor SHALL perform periodic HTTP GET health checks at a configurable interval between 1 minute and 60 minutes (default: 5 minutes) with a connection timeout of 30 seconds
2. WHEN a health check completes, THE Health_Monitor SHALL record HTTP status code, response time in milliseconds, SSL certificate validity status and expiry date, and redirect chain (up to a maximum of 10 redirects) for each check
3. WHEN a website returns a non-2xx HTTP status code or exceeds the configured response time threshold (configurable between 1 second and 60 seconds, default: 10 seconds) for 3 consecutive checks, THE Health_Monitor SHALL generate a downtime alert
4. WHEN a website transitions from unavailable to available state, THE Health_Monitor SHALL generate a recovery notification including the total downtime duration
5. THE DomainRadar SHALL display website uptime percentage calculated to two decimal places and response time trends over selectable time periods (24 hours, 7 days, 30 days)
6. IF a health check fails due to DNS resolution failure, THEN THE Health_Monitor SHALL categorize the issue as a DNS problem distinct from general downtime and include the failed resolution detail in the alert
7. IF a health check fails due to connection timeout or network error that is not a DNS resolution failure, THEN THE Health_Monitor SHALL categorize the issue as a connectivity problem and include the error type in the alert

### Requirement 7: HTTPS Certificate Monitoring

**User Story:** As a domain administrator, I want to monitor SSL/TLS certificate expiration and validity, so that I can renew certificates before they expire and avoid security warnings.

#### Acceptance Criteria

1. THE Health_Monitor SHALL retrieve and parse the SSL/TLS certificate for each domain with an associated website at least once per day
2. THE Health_Monitor SHALL record certificate issuer, subject, valid-from date, valid-to date, certificate chain completeness (all intermediate certificates present and valid), and serial number
3. WHEN a certificate has fewer than 30 days until expiration, THE Alert_Engine SHALL generate certificate expiration alerts following the same severity tiering as domain expiration (30/14/7/3/1 days)
4. IF a certificate has an invalid chain, hostname mismatch, or is revoked, THEN THE Health_Monitor SHALL generate a critical alert within 5 minutes of detection regardless of expiration date
5. THE DomainRadar SHALL display certificate status (issuer, valid-from, valid-to, days remaining, chain status) alongside domain information in the domain detail view
6. WHEN a certificate is renewed (detected by changed valid-to date or serial number), THE Health_Monitor SHALL log the renewal event and clear any active certificate alerts for that domain
7. IF the Health_Monitor cannot connect to a domain's HTTPS endpoint to retrieve the certificate, THEN it SHALL log the connection failure and retry at the next scheduled check without generating a certificate-specific alert

### Requirement 8: Email Service Monitoring

**User Story:** As a domain administrator, I want to monitor the email service configuration for my domains, so that I can ensure email deliverability and security compliance.

#### Acceptance Criteria

1. THE Health_Monitor SHALL query and validate MX records for domains with email service enabled at least once per day
2. THE Health_Monitor SHALL check for the presence and syntactic validity (per RFC specifications) of SPF, DKIM, and DMARC DNS records for each email-enabled domain
3. WHEN a domain is missing SPF, DKIM, or DMARC records, THE Health_Monitor SHALL generate a compliance warning indicating the specific missing record type
4. WHEN MX records differ from the previously recorded check result, THE Alert_Engine SHALL generate a warning alert and notify the administrator
5. THE DomainRadar SHALL display an email security compliance score per domain on a 0-100 numeric scale, with equal weighting across MX presence, SPF validity, DKIM validity, and DMARC validity (25 points each)
6. IF an MX record points to a host that does not respond to a TCP connection on port 25 within 10 seconds after 2 retry attempts, THEN THE Health_Monitor SHALL generate a critical alert indicating potential email service outage

### Requirement 9: Dashboard and Reporting

**User Story:** As a domain administrator, I want a unified dashboard showing the health and status of all my domain assets, so that I can quickly assess the overall state and identify issues.

#### Acceptance Criteria

1. THE DomainRadar SHALL display a dashboard containing: total domain count, domains expiring within 30 days, active alerts count, overall health score (0-100), and domains grouped by registrar
2. THE DomainRadar SHALL provide an expiration calendar view showing domain and certificate expiration dates on a monthly calendar
3. THE DomainRadar SHALL calculate a health score per domain on a 0-100 scale based on: expiration proximity (30 points), certificate validity (25 points), website uptime (25 points), and email compliance (20 points)
4. THE DomainRadar SHALL support exporting domain lists and reports in CSV and Excel formats with a maximum of 10,000 rows per export
5. WHEN a user accesses the dashboard, THE DomainRadar SHALL render all statistics within 3 seconds for a dataset of up to 1000 domains, with dashboard data refreshed at most 5 minutes prior to display
6. THE DomainRadar SHALL provide cost statistics showing renewal costs grouped by registrar, time period (monthly, quarterly, yearly), and custom tags

### Requirement 10: User Authentication and Role-Based Access Control

**User Story:** As a system administrator, I want to manage user access through SSO and role-based permissions, so that team members have appropriate access levels and all actions are auditable.

#### Acceptance Criteria

1. THE DomainRadar SHALL authenticate users exclusively through Authentik SSO integration using OIDC protocol
2. THE Admin_Panel SHALL support defining roles with granular permissions (view domains, manage domains, configure integrations, manage users, view alerts, manage alerts)
3. WHEN a user performs a create, update, or delete operation, THE DomainRadar SHALL record an audit log entry containing user identity, action type, target resource, timestamp, and changed fields, retaining audit logs for a minimum of 365 days
4. THE DomainRadar SHALL enforce role-based access control on all API endpoints, returning HTTP 403 for unauthorized access attempts
5. THE Admin_Panel SHALL provide a user management interface showing all users, their roles, last login time, and activity summary
6. WHEN a user session expires or is revoked in Authentik, THE DomainRadar SHALL invalidate the corresponding local session within 60 seconds

### Requirement 11: Registrar Configuration Management

**User Story:** As a system administrator, I want to manage all registrar API credentials and integration settings through the admin panel, so that sensitive configuration is stored securely without relying on environment variables.

#### Acceptance Criteria

1. THE Admin_Panel SHALL provide configuration forms for each supported registrar type with registrar-specific credential fields: GoDaddy (API Key + Secret or PAT), Alibaba Cloud (AccessKey ID + Secret), Tencent Cloud (SecretId + SecretKey), Cloudflare (API Token), Namecheap (API Key + Username + IP whitelist)
2. THE DomainRadar SHALL store all registrar API credentials encrypted in the database such that credentials are never retrievable in plaintext via any API response or database query without the encryption key
3. WHEN a registrar configuration is saved, THE DomainRadar SHALL mask credential values in API responses and audit logs, displaying only the last 4 characters
4. THE Admin_Panel SHALL support configuring up to 20 accounts per registrar type to accommodate domains spread across different accounts
5. WHEN a registrar configuration is created or modified, THE DomainRadar SHALL perform a connectivity test within 30 seconds and report success or failure with error cause to the administrator
6. IF a connectivity test fails, THEN THE DomainRadar SHALL persist the configuration in a "disconnected" state and display the error cause to the administrator
7. THE Admin_Panel SHALL provide a registrar plugin status page showing sync status, last sync time, domain count, and the most recent 50 error entries for each configured registrar account
8. WHEN saving registrar configuration, THE DomainRadar SHALL validate that all required fields are non-empty and credential values do not exceed 512 characters

### Requirement 12: Smart Sync Frequency Scheduling

**User Story:** As a domain administrator, I want the system to adjust monitoring frequency based on domain expiration proximity, so that resources are used efficiently while ensuring timely detection of issues near expiration.

#### Acceptance Criteria

1. WHILE a domain has more than 90 days until expiration, THE Sync_Scheduler SHALL sync it from the registrar API once per week (every 168 hours ± 1 hour)
2. WHILE a domain has 30 to 90 days until expiration, THE Sync_Scheduler SHALL sync it once per day (every 24 hours ± 30 minutes)
3. WHILE a domain has fewer than 30 days until expiration, THE Sync_Scheduler SHALL sync it every 12 hours (± 30 minutes)
4. THE Sync_Scheduler SHALL allow administrators to override the default frequency schedule per domain or per registrar account via the Admin_Panel, with a minimum override interval of 1 hour and a maximum of 30 days
5. WHEN a domain's expiration date changes (renewal detected), THE Sync_Scheduler SHALL recalculate the sync frequency tier for that domain within 5 minutes of detecting the change
6. THE Sync_Scheduler SHALL log all sync operations with start time, end time, domains synced, domains updated, and any errors encountered

### Requirement 13: Domain Tagging and Grouping

**User Story:** As a domain administrator, I want to organize domains with custom tags and groups, so that I can categorize and filter my domain portfolio efficiently.

#### Acceptance Criteria

1. THE DomainRadar SHALL support assigning up to 20 custom tags to each domain record, with each tag name being 1-50 characters
2. THE DomainRadar SHALL support organizing domains into hierarchical groups with up to 3 levels of nesting (e.g., by business unit, project, or purpose)
3. WHEN viewing the domain list, THE DomainRadar SHALL provide filtering by tags, groups, registrar, status, and expiration date range with results returned within 2 seconds for up to 1000 domains
4. THE DomainRadar SHALL preserve user-defined tags and groups when domain records are updated by the Sync_Scheduler or WHOIS_Worker
5. THE DomainRadar SHALL support bulk tag assignment and removal across up to 500 selected domains in a single operation
6. WHEN exporting domain data, THE DomainRadar SHALL include tags and group membership in the export output

### Requirement 14: Docker Containerization

**User Story:** As a system administrator, I want the entire DomainRadar system to be fully containerized with Docker, so that I can deploy, scale, and manage all services consistently across development and production environments.

#### Acceptance Criteria

1. THE Container_Orchestrator SHALL define all DomainRadar services in a docker-compose.yml file including: backend (Golang), frontend (React via Nginx), PostgreSQL database, Redis (queues and caching), and Who_Dat_Service
2. THE Container_Orchestrator SHALL provide separate docker-compose override files for development (with hot-reload, debug ports, and volume-mounted source code) and production (with optimized builds, resource limits, and restart policies)
3. THE DomainRadar backend service SHALL be built as a multi-stage Docker image producing a final image based on a minimal base (Alpine or distroless) with a maximum uncompressed image size of 100MB
4. THE DomainRadar frontend service SHALL be built as a multi-stage Docker image that compiles the React application and serves static assets via Nginx with a maximum uncompressed image size of 50MB
5. THE Container_Orchestrator SHALL configure health checks for all containers: backend (HTTP endpoint), frontend (HTTP endpoint), PostgreSQL (pg_isready), Redis (redis-cli ping), and Who_Dat_Service (HTTP health endpoint), with a check interval of 30 seconds and a start period of 60 seconds
6. THE Container_Orchestrator SHALL define named volumes for persistent data including PostgreSQL data directory and application logs, ensuring data survives container recreation
7. THE Container_Orchestrator SHALL configure an internal Docker network for inter-service communication, with only the frontend (HTTP/HTTPS) and backend API ports exposed to the host
8. THE Container_Orchestrator SHALL use environment variables for all service configuration (database credentials, Redis connection, who-dat URL, external API keys) with defaults suitable for local development provided in a documented .env.example file
9. WHEN `docker-compose up` is executed with no prior state, THE Container_Orchestrator SHALL start all services with correct dependency ordering (database and Redis start before backend, who-dat starts before backend, backend starts before frontend) and reach a fully healthy state within 120 seconds
10. THE Container_Orchestrator SHALL configure restart policies of "unless-stopped" for production and "no" for development for all service containers
11. IF a dependent service container (PostgreSQL, Redis, or Who_Dat_Service) becomes unhealthy, THEN THE Container_Orchestrator SHALL restart the unhealthy container automatically up to 3 times within a 5-minute window before marking it as failed
12. THE Container_Orchestrator SHALL include a Redis container configured with persistence (AOF or RDB) and a maximum memory limit of 256MB (configurable via environment variable) for queue and caching operations
