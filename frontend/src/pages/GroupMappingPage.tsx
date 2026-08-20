import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Select, Card, Button, Input, Tag, Space, App } from 'antd';
import { PlusOutlined, DeleteOutlined } from '@ant-design/icons';
import { groupMappingApi } from '../services';
import type { GroupMappingItem } from '../services';

const roleOptions = [
  { value: 'viewer', label: '查看者' },
  { value: 'operator', label: '操作员' },
  { value: 'admin', label: '管理员' },
];

const roleColors: Record<string, string> = {
  admin: 'purple',
  operator: 'blue',
  viewer: 'default',
};

export function GroupMappingPage() {
  const queryClient = useQueryClient();
  const { message } = App.useApp();
  const [groupName, setGroupName] = useState('');
  const [role, setRole] = useState<string>('viewer');

  const { data, isLoading } = useQuery({
    queryKey: ['group-mappings'],
    queryFn: groupMappingApi.list,
  });

  const createMutation = useMutation({
    mutationFn: groupMappingApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings'] });
      message.success('映射已添加');
      setGroupName('');
      setRole('viewer');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const deleteMutation = useMutation({
    mutationFn: groupMappingApi.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['group-mappings'] });
      message.success('映射已删除');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const handleAdd = () => {
    if (!groupName.trim()) {
      message.warning('请输入组名称');
      return;
    }
    createMutation.mutate({ group_name: groupName.trim(), role });
  };

  const columns = [
    {
      title: 'SSO 组名',
      dataIndex: 'group_name',
      key: 'group_name',
      render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span>,
    },
    {
      title: '平台角色',
      dataIndex: 'role',
      key: 'role',
      render: (r: string) => (
        <Tag color={roleColors[r]}>{roleOptions.find(o => o.value === r)?.label || r}</Tag>
      ),
    },
    {
      title: '创建时间',
      dataIndex: 'created_at',
      key: 'created_at',
      render: (d: string) => new Date(d).toLocaleString(),
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: GroupMappingItem) => (
        <Button
          type="text"
          danger
          icon={<DeleteOutlined />}
          onClick={() => deleteMutation.mutate(record.id)}
        />
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
          组 → 角色映射
        </h1>
        <p style={{ fontSize: 14, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          将 SSO 组映射到平台角色，用户登录时自动分配权限
        </p>
      </div>

      {/* Add form */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }} styles={{ body: { padding: '16px 24px' } }}>
        <Space size={12}>
          <Input
            placeholder="SSO 组名称"
            value={groupName}
            onChange={(e) => setGroupName(e.target.value)}
            style={{ width: 240 }}
            onPressEnter={handleAdd}
          />
          <Select
            value={role}
            onChange={setRole}
            options={roleOptions}
            style={{ width: 140 }}
          />
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={handleAdd}
            loading={createMutation.isPending}
          >
            添加
          </Button>
        </Space>
      </Card>

      {/* Mappings table */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.data || []}
          loading={isLoading}
          pagination={false}
        />
      </Card>
    </div>
  );
}
