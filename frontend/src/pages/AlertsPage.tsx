import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Tag, Button, Select, Space, Card, message } from 'antd';
import { CheckOutlined } from '@ant-design/icons';
import { alertApi } from '../services';
import type { Alert } from '../types';

const severityConfig: Record<string, { color: string; bg: string; border: string }> = {
  informational: { color: '#3b82f6', bg: '#eff6ff', border: '#bfdbfe' },
  warning: { color: '#f59e0b', bg: '#fffbeb', border: '#fde68a' },
  critical: { color: '#ef4444', bg: '#fef2f2', border: '#fecaca' },
  expired: { color: '#6b7280', bg: '#f9fafb', border: '#e5e7eb' },
};

export function AlertsPage() {
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [severity, setSeverity] = useState('');
  const [alertType, setAlertType] = useState('');

  const params: Record<string, string> = { page: String(page), page_size: '20' };
  if (severity) params.severity = severity;
  if (alertType) params.alert_type = alertType;

  const { data, isLoading } = useQuery({
    queryKey: ['alerts', params],
    queryFn: () => alertApi.list(params),
  });

  const ackMutation = useMutation({
    mutationFn: alertApi.acknowledge,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['alerts'] });
      message.success('告警已确认');
    },
  });

  const columns = [
    {
      title: '严重性',
      dataIndex: 'severity',
      key: 'severity',
      render: (s: string) => {
        const config = severityConfig[s] || severityConfig.expired;
        return (
          <span style={{
            display: 'inline-block',
            padding: '2px 10px',
            borderRadius: 6,
            fontSize: 12,
            fontWeight: 600,
            color: config.color,
            background: config.bg,
            border: `1px solid ${config.border}`,
          }}>
            {s}
          </span>
        );
      },
    },
    {
      title: '类型',
      dataIndex: 'alert_type',
      key: 'alert_type',
      render: (t: string) => <Tag style={{ borderRadius: 6 }}>{t}</Tag>,
    },
    {
      title: '域名',
      dataIndex: 'domain_name',
      key: 'domain_name',
      render: (d: string) => <span style={{ fontWeight: 500 }}>{d}</span>,
    },
    {
      title: '消息',
      dataIndex: 'message',
      key: 'message',
      ellipsis: true,
    },
    {
      title: '剩余天数',
      dataIndex: 'days_remaining',
      key: 'days_remaining',
      render: (d: number | null) => d !== null ? (
        <span style={{ fontWeight: 600, color: d <= 7 ? '#ef4444' : d <= 30 ? '#f59e0b' : '#10b981' }}>
          {d} 天
        </span>
      ) : '-',
    },
    {
      title: '投递状态',
      dataIndex: 'delivery_status',
      key: 'delivery_status',
      render: (s: string) => <Tag color={s === 'delivered' ? 'green' : s === 'failed' ? 'red' : 'default'} style={{ borderRadius: 6 }}>{s}</Tag>,
    },
    {
      title: '时间',
      dataIndex: 'generated_at',
      key: 'generated_at',
      render: (d: string) => new Date(d).toLocaleDateString(),
    },
    {
      title: '操作',
      key: 'action',
      render: (_: any, record: Alert) => (
        !record.acknowledged ? (
          <Button size="small" type="primary" ghost icon={<CheckOutlined />} onClick={() => ackMutation.mutate(record.id)}>
            确认
          </Button>
        ) : <Tag color="green" style={{ borderRadius: 6 }}>已确认</Tag>
      ),
    },
  ];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24 }}>
        <h1 style={{
          fontSize: 28,
          fontWeight: 700,
          margin: 0,
          background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
          WebkitBackgroundClip: 'text',
          WebkitTextFillColor: 'transparent',
          backgroundClip: 'text',
        }}>
          告警中心
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          查看和管理域名相关告警
        </p>
      </div>

      {/* Filters */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }} styles={{ body: { padding: '16px 20px' } }}>
        <Space>
          <Select placeholder="严重性" allowClear style={{ width: 130 }} onChange={(v) => setSeverity(v || '')}
            options={[
              { value: 'critical', label: '严重' },
              { value: 'warning', label: '警告' },
              { value: 'informational', label: '信息' },
            ]}
          />
          <Select placeholder="类型" allowClear style={{ width: 130 }} onChange={(v) => setAlertType(v || '')}
            options={[
              { value: 'expiration', label: '到期' },
              { value: 'certificate', label: '证书' },
              { value: 'downtime', label: '宕机' },
              { value: 'email', label: '邮件' },
            ]}
          />
        </Space>
      </Card>

      {/* Table */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.alerts || []}
          loading={isLoading}
          pagination={{
            current: page,
            total: data?.total || 0,
            pageSize: 20,
            onChange: setPage,
            showTotal: (total) => `共 ${total} 条告警`,
          }}
        />
      </Card>
    </div>
  );
}
