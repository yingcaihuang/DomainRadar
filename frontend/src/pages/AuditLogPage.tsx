import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Table, Select, Space, Card } from 'antd';
import { auditApi } from '../services';

export function AuditLogPage() {
  const [page, setPage] = useState(1);
  const [actionType, setActionType] = useState('');
  const [resourceType, setResourceType] = useState('');

  const params: Record<string, string> = { page: String(page), page_size: '20' };
  if (actionType) params.action_type = actionType;
  if (resourceType) params.resource_type = resourceType;

  const { data, isLoading } = useQuery({
    queryKey: ['audit-logs', params],
    queryFn: () => auditApi.list(params),
  });

  const columns = [
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (d: string) => new Date(d).toLocaleString(),
    },
    { title: '用户 ID', dataIndex: 'user_id', key: 'user_id' },
    { title: '操作', dataIndex: 'action_type', key: 'action_type' },
    { title: '资源类型', dataIndex: 'resource_type', key: 'resource_type' },
    { title: '资源 ID', dataIndex: 'resource_id', key: 'resource_id' },
    {
      title: '变更内容',
      dataIndex: 'changed_fields',
      key: 'changed_fields',
      ellipsis: true,
      render: (f: any) => {
        try {
          const str = typeof f === "string" ? f : JSON.stringify(f); return str?.slice(0, 100) || "-";
        } catch {
        }
      },
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
          审计日志
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          查看系统操作记录与变更历史
        </p>
      </div>

      {/* Filters */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }} styles={{ body: { padding: '16px 20px' } }}>
        <Space>
          <Select placeholder="操作类型" allowClear style={{ width: 120 }} onChange={(v) => setActionType(v || '')}
            options={[
              { value: 'CREATE', label: '创建' },
              { value: 'UPDATE', label: '更新' },
              { value: 'DELETE', label: '删除' },
            ]}
          />
          <Select placeholder="资源类型" allowClear style={{ width: 150 }} onChange={(v) => setResourceType(v || '')}
            options={[
              { value: 'domain', label: '域名' },
              { value: 'registrar_account', label: '注册商' },
              { value: 'notification_channel', label: '通知渠道' },
            ]}
          />
        </Space>
      </Card>

      {/* Table */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.logs || []}
          loading={isLoading}
          pagination={{
            current: page,
            total: data?.total || 0,
            pageSize: 20,
            onChange: setPage,
            showTotal: (total) => `共 ${total} 条记录`,
          }}
        />
      </Card>
    </div>
  );
}
