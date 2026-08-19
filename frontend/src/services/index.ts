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
  login: () => { window.location.href = `${API_BASE}/auth/login`; },
  logout: () => request('/auth/logout', { method: 'POST' }),
  me: () => request<{ data: User }>('/auth/me'),
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
  list: () => request<{ data: User[] }>('/users'),
  updateRoles: (id: number, roles: string[]) =>
    request(`/users/${id}/roles`, { method: 'PUT', body: JSON.stringify({ roles }) }),
};

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
