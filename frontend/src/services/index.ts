import type { Domain, Alert, DashboardData, CalendarEntry, UptimeData, Tag, Group, RegistrarAccount, NotificationChannel, User, AuditLog, ImportResult } from '../types';

const API_BASE = '/api/v1';

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: { message: response.statusText } }));
    throw new Error(error.error?.message || response.statusText);
  }

  // Handle CSV export (no JSON)
  if (response.headers.get('content-type')?.includes('text/csv')) {
    return response.text() as unknown as T;
  }

  return response.json();
}

// Auth
export const authApi = {
  loginSSO: () => { window.location.href = `${API_BASE}/auth/login-sso`; },
  login: (data: { username: string; password: string }) =>
    request<{ message: string; user: any; token: string }>('/auth/login', { method: 'POST', body: JSON.stringify(data) }),
  logout: () => request('/auth/logout', { method: 'POST' }),
  me: () => request<{ data: User }>('/auth/me'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    request<{ message: string }>('/auth/change-password', { method: 'POST', body: JSON.stringify(data) }),
  mode: () => request<{ sso_enabled: boolean; local_enabled: boolean }>('/auth/mode'),
};

// Domains
export const domainApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<{ domains: Domain[]; total: number; page: number; page_size: number; total_pages: number }>(`/domains${qs}`);
  },
  get: (id: number) => request<{ data: Domain }>(`/domains/${id}`),
  whois: (id: number) => request<{ data: any }>(`/domains/${id}/whois`),
  create: (data: Partial<Domain> & { tag_ids?: number[] }) =>
    request<{ data: Domain }>('/domains', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: Partial<Domain> & { tag_ids?: number[] }) =>
    request<{ data: Domain }>(`/domains/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/domains/${id}`, { method: 'DELETE' }),
  import: (file: File) => {
    const formData = new FormData();
    formData.append('file', file);
    return fetch(`${API_BASE}/domains/import`, { method: 'POST', body: formData })
      .then(r => r.json()) as Promise<{ data: ImportResult }>;
  },
  export: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    window.open(`${API_BASE}/domains/export${qs}`, '_blank');
  },
  bulk: (data: { domain_ids: number[]; action: string; tag_ids?: number[]; group_id?: number }) =>
    request('/domains/bulk', { method: 'POST', body: JSON.stringify(data) }),
  calendar: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<{ data: CalendarEntry[] }>(`/domains/calendar${qs}`);
  },
};

// Tags
export const tagApi = {
  list: () => request<{ data: Tag[] }>('/tags'),
  create: (name: string) => request<{ data: Tag }>('/tags', { method: 'POST', body: JSON.stringify({ name }) }),
  delete: (id: number) => request(`/tags/${id}`, { method: 'DELETE' }),
};

// Groups
export const groupApi = {
  list: () => request<{ data: Group[] }>('/groups'),
  create: (data: { name: string; parent_id?: number }) =>
    request<{ data: Group }>('/groups', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: { name?: string; parent_id?: number }) =>
    request<{ data: Group }>(`/groups/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/groups/${id}`, { method: 'DELETE' }),
};

// Alerts
export const alertApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<{ alerts: Alert[]; total: number; page: number; page_size: number; total_pages: number }>(`/alerts${qs}`);
  },
  get: (id: number) => request<Alert>(`/alerts/${id}`),
  acknowledge: (id: number) => request(`/alerts/${id}/acknowledge`, { method: 'PUT' }),
};

// Dashboard
export const dashboardApi = {
  get: () => request<DashboardData>('/dashboard'),
  healthScores: () => request<{ data: { id: number; domain_name: string; health_score: number }[] }>('/dashboard/health-scores'),
};

// Monitoring
export const monitorApi = {
  website: (domainId: number) => request<{ domain_id: number; checks: any[]; total: number }>(`/monitoring/websites/${domainId}`),
  uptime: (domainId: number, period?: string) => {
    const qs = period ? `?period=${period}` : '';
    return request<UptimeData>(`/monitoring/uptime/${domainId}${qs}`);
  },
  certificate: (domainId: number) => request<{ domain_id: number; latest: any; history: any[] }>(`/monitoring/certificates/${domainId}`),
  email: (domainId: number) => request<{ domain_id: number; latest: any; history: any[] }>(`/monitoring/email/${domainId}`),
};

// Registrar configuration
export const registrarApi = {
  list: () => request<{ data: RegistrarAccount[] }>('/registrars'),
  create: (data: { registrar_type: string; display_name: string; account_name: string; credentials: Record<string, string> }) =>
    request<{ data: RegistrarAccount }>('/registrars', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: { account_name?: string; credentials?: Record<string, string> }) =>
    request<{ data: RegistrarAccount }>(`/registrars/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/registrars/${id}`, { method: 'DELETE' }),
  test: async (id: number) => { const res = await request<{ status: string; message: string }>(`/registrars/${id}/test`, { method: 'POST' }); if (res.status === 'disconnected') throw new Error(res.message); return res; },
  sync: (id: number) => request(`/registrars/${id}/sync`, { method: 'POST' }),
  previewSync: (id: number) => request<{ data: any[]; total: number }>(`/registrars/${id}/preview-sync`, { method: 'POST' }),
  importDomains: (id: number, data: { domain_names: string[]; tag_ids?: number[]; group_id?: number }) => request(`/registrars/${id}/import`, { method: 'POST', body: JSON.stringify(data) }),
  status: (id: number) => request(`/registrars/${id}/status`),
};

// Notification channels
export const notificationApi = {
  channels: {
    list: () => request<{ data: NotificationChannel[] }>('/notifications/channels'),
    create: (data: { channel_type: string; name: string; config: Record<string, string> }) =>
      request<{ data: NotificationChannel }>('/notifications/channels', { method: 'POST', body: JSON.stringify(data) }),
    update: (id: number, data: { name?: string; config?: Record<string, string> }) =>
      request<{ data: NotificationChannel }>(`/notifications/channels/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
    delete: (id: number) => request(`/notifications/channels/${id}`, { method: 'DELETE' }),
    test: (id: number) => request(`/notifications/channels/${id}/test`, { method: 'POST' }),
  },
  rules: {
    list: () => request<{ data: any[] }>('/notifications/rules'),
    create: (data: { domain_id: number; channel_id: number; severity_filter: string }) =>
      request('/notifications/rules', { method: 'POST', body: JSON.stringify(data) }),
    delete: (id: number) => request(`/notifications/rules/${id}`, { method: 'DELETE' }),
  },
};

// Users
export const userApi = {
  list: () => request<{ users: userResponse[]; total: number; page: number; page_size: number; total_pages: number }>('/users'),
  create: (data: { username: string; email: string; display_name: string; password: string; roles: string[] }) =>
    request('/users', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: { email?: string; display_name?: string; roles?: string[] }) =>
    request(`/users/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/users/${id}`, { method: 'DELETE' }),
  resetPassword: (id: number, newPassword: string) =>
    request(`/users/${id}/reset-password`, { method: 'POST', body: JSON.stringify({ new_password: newPassword }) }),
  updateRoles: (id: number, roles: string[]) =>
    request(`/users/${id}/roles`, { method: 'PUT', body: JSON.stringify({ roles }) }),
};

// Internal type for user list response
interface userResponse {
  id: number;
  external_id: string;
  email: string;
  display_name: string;
  auth_source: string;
  roles: string[];
  last_login_at: string | null;
  created_at: string;
}

// Group Mappings
export const groupMappingApi = {
  list: () => request<{ data: GroupMappingItem[] }>('/config/group-mappings'),
  create: (data: { group_name: string; role: string }) =>
    request('/config/group-mappings', { method: 'POST', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/config/group-mappings/${id}`, { method: 'DELETE' }),
};

export interface GroupMappingItem {
  id: number;
  group_name: string;
  role: string;
  created_at: string;
}

// Audit logs
export const auditApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<{ logs: AuditLog[]; total: number; page: number; page_size: number; total_pages: number }>(`/audit-logs${qs}`);
  },
};

// Reports
export const reportApi = {
  costs: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<{ data: any[]; period: string }>(`/reports/costs${qs}`);
  },
};

// Expiration Rules Configuration
export const rulesApi = {
  list: () => request<{ data: any[] }>('/config/expiration-rules'),
  create: (data: any) => request('/config/expiration-rules', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: any) => request(`/config/expiration-rules/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/config/expiration-rules/${id}`, { method: 'DELETE' }),
  resetDefaults: () => request('/config/expiration-rules/reset-defaults', { method: 'POST' }),
};

// Certificate Monitoring
export const certApi = {
  listMonitors: (domainId: number) => request<{ data: CertMonitor[] }>(`/domains/${domainId}/certificates`),
  addMonitor: (domainId: number, data: { endpoint: string; label: string }) =>
    request<{ data: CertMonitor }>(`/domains/${domainId}/certificates`, { method: 'POST', body: JSON.stringify(data) }),
  deleteMonitor: (monitorId: number) => request(`/certificates/${monitorId}`, { method: 'DELETE' }),
  checkNow: (monitorId: number) => request<{ data: CertCheckResult }>(`/certificates/${monitorId}/check`, { method: 'POST' }),
  history: (monitorId: number) => request<{ data: CertCheckResult[] }>(`/certificates/${monitorId}/history`),
};

// Email Monitoring
export const emailMonitorApi = {
  get: (domainId: number) => request<{ data: EmailMonitorData }>(`/domains/${domainId}/email-monitor`),
  configure: (domainId: number, data: { dkim_selectors: string; mail_server_ips: string }) =>
    request<{ data: EmailMonitorData }>(`/domains/${domainId}/email-monitor`, { method: 'POST', body: JSON.stringify(data) }),
  check: (domainId: number) => request<{ data: EmailCheckResultData }>(`/domains/${domainId}/email-monitor/check`, { method: 'POST' }),
  history: (domainId: number) => request<{ data: EmailCheckResultData[] }>(`/domains/${domainId}/email-monitor/history`),
};

// Certificate monitoring types (inline for service use)
export interface CertMonitor {
  id: number;
  domain_id: number;
  endpoint: string;
  label: string;
  enabled: boolean;
  last_checked_at: string | null;
  next_check_at: string | null;
  created_at: string;
  latest?: CertCheckResult;
}

export interface CertCheckResult {
  id: number;
  subject: string;
  issuer: string;
  valid_from: string;
  valid_to: string;
  days_remaining: number;
  sans: string[];
  chain_complete: boolean;
  chain: { subject: string; issuer: string; valid_from: string; valid_to: string; serial_number: string; is_ca: boolean; sans?: string[] }[];
  error?: string;
  connected_ip: string;
  sni: string;
  dns_resolve_ms: number;
  handshake_ms: number;
  total_ms: number;
  tls_version: string;
  cipher_suite: string;
  checked_at: string;
}

// Email monitoring types
export interface EmailCheckDetail {
  score: number;
  max_score: number;
  findings: string[];
}

export interface EmailCheckDetails {
  mx: EmailCheckDetail;
  spf: EmailCheckDetail;
  dkim: EmailCheckDetail;
  dmarc: EmailCheckDetail;
  ptr: EmailCheckDetail;
  mta_sts: EmailCheckDetail;
  tlsrpt: EmailCheckDetail;
  bimi: EmailCheckDetail;
}

export interface EmailCheckResultData {
  id: number;
  total_score: number;
  grade: string;
  mx_score: number;
  spf_score: number;
  dkim_score: number;
  dmarc_score: number;
  ptr_score: number;
  mta_sts_score: number;
  tlsrpt_score: number;
  bimi_score: number;
  details?: EmailCheckDetails;
  checked_at: string;
}

export interface EmailMonitorData {
  id: number;
  domain_id: number;
  enabled: boolean;
  dkim_selectors: string;
  mail_server_ips: string;
  last_checked_at: string | null;
  next_check_at: string | null;
  latest_result?: EmailCheckResultData;
}

// Email Alert Rules Configuration
export const emailRulesApi = {
  list: () => request<{ data: any[] }>('/config/email-alert-rules'),
  create: (data: any) => request('/config/email-alert-rules', { method: 'POST', body: JSON.stringify(data) }),
  update: (id: number, data: any) => request(`/config/email-alert-rules/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) => request(`/config/email-alert-rules/${id}`, { method: 'DELETE' }),
  resetDefaults: () => request('/config/email-alert-rules/reset-defaults', { method: 'POST' }),
};

// SSO Configuration
export const ssoConfigApi = {
  get: () => request<{ data: any }>('/config/sso'),
  update: (data: any) =>
    request<{ message: string; data: any }>('/config/sso', { method: 'PUT', body: JSON.stringify(data) }),
  test: (data: { issuer_url: string; client_id?: string; redirect_url?: string }) =>
    request<{ success: boolean; message: string }>('/config/sso/test', { method: 'POST', body: JSON.stringify(data) }),
  discover: (data: { issuer_url: string }) =>
    request<{ success: boolean; message?: string; data?: any }>('/config/sso/discover', { method: 'POST', body: JSON.stringify(data) }),
};
