// Domain types
export interface Domain {
  id: number;
  domain_name: string;
  registrar_account_id: number | null;
  registrar_identifier: string;
  creation_date: string | null;
  expiration_date: string | null;
  auto_renew: boolean;
  renewal_deadline: string | null;
  status: string;
  nameservers: string[];
  privacy_protection: boolean;
  lock_status: boolean;
  data_source_type: string;
  last_sync_at: string | null;
  group_id: number | null;
  notes: string;
  website_url: string;
  email_enabled: boolean;
  health_score: number;
  check_interval_minutes: number;
  response_time_threshold_ms: number;
  tags: Tag[];
  group: Group | null;
  created_at: string;
  updated_at: string;
}

export interface Tag {
  id: number;
  name: string;
}

export interface Group {
  id: number;
  name: string;
  parent_id: number | null;
  level: number;
  children?: Group[];
}

// Alert types
export interface Alert {
  id: number;
  domain_id: number;
  domain_name?: string;
  alert_type: string;
  severity: string;
  message: string;
  days_remaining: number | null;
  acknowledged: boolean;
  delivery_status: string;
  generated_at: string;
  acknowledged_at: string | null;
}

// Dashboard types
export interface DashboardData {
  total_domains: number;
  expiring_within_30_days: number;
  active_alerts: number;
  overall_health_score: number;
  by_registrar: RegistrarGroup[];
  cert_monitors: number;
  cert_expiring: number;
  email_monitors: number;
  email_avg_score: number;
}

export interface RegistrarGroup {
  registrar: string;
  domain_count: number;
}

export interface HealthScoreEntry {
  id: number;
  domain_name: string;
  health_score: number;
}

// Calendar types
export interface CalendarEntry {
  id: number;
  domain_name: string;
  expiration_date: string;
  type: 'domain' | 'certificate';
  severity: string;
  days_remaining: number;
}

// Monitoring types
export interface HealthCheck {
  id: number;
  domain_id: number;
  http_status_code: number;
  response_time_ms: number;
  ssl_valid: boolean;
  ssl_expiry: string | null;
  redirect_chain: string;
  check_type: string;
  failure_category: string;
  failure_detail: string;
  checked_at: string;
}

export interface UptimeData {
  domain_id: number;
  period: string;
  total_checks: number;
  successful_checks: number;
  failed_checks: number;
  uptime_percentage: number;
  avg_response_time_ms: number;
  max_response_time_ms: number;
  min_response_time_ms: number;
}

export interface CertificateCheck {
  id: number;
  domain_id: number;
  issuer: string;
  subject: string;
  valid_from: string;
  valid_to: string;
  chain_complete: boolean;
  serial_number: string;
  days_remaining: number;
  checked_at: string;
}

export interface EmailCheck {
  id: number;
  domain_id: number;
  mx_records: string;
  spf_valid: boolean;
  dkim_valid: boolean;
  dmarc_valid: boolean;
  compliance_score: number;
  mx_changed: boolean;
  checked_at: string;
}

// Settings types
export interface RegistrarAccount {
  id: number;
  registrar_config_id: number;
  registrar_type: string;
  display_name: string;
  account_name: string;
  credentials: Record<string, string>;
  status: string;
  sync_interval_hours: number;
  last_sync_at: string | null;
  domain_count: number;
  created_at: string;
  updated_at: string;
}

export interface NotificationChannel {
  id: number;
  channel_type: string;
  name: string;
  config: Record<string, string>;
  status: string;
  last_tested_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface NotificationRule {
  id: number;
  domain_id: number;
  channel_id: number;
  severity_filter: string;
  created_at: string;
}

export interface User {
  id: number;
  external_id: string;
  email: string;
  display_name: string;
  roles: string[];
  last_login_at: string | null;
}

export interface AuditLog {
  id: number;
  user_id: number;
  action_type: string;
  resource_type: string;
  resource_id: string;
  changed_fields: string;
  created_at: string;
}

// API response types
export interface PaginatedResponse<T> {
  total: number;
  page: number;
  page_size: number;
  total_pages: number;
  data?: T[];
}

export interface ImportResult {
  total_rows: number;
  created: number;
  updated: number;
  errors: ImportError[];
  total_errors: number;
}

export interface ImportError {
  row: number;
  field: string;
  reason: string;
}
