import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Select, Card, message } from 'antd';
import { userApi } from '../services';

const roleOptions = [
  { value: 'viewer', label: '查看者' },
  { value: 'operator', label: '操作员' },
  { value: 'admin', label: '管理员' },
];

export function UserManagementPage() {
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: userApi.list,
  });

  const updateRolesMutation = useMutation({
    mutationFn: ({ id, roles }: { id: number; roles: string[] }) => userApi.updateRoles(id, roles),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      message.success('角色已更新');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const columns = [
    { title: '姓名', dataIndex: 'display_name', key: 'display_name', render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span> },
    { title: '邮箱', dataIndex: 'email', key: 'email' },
    {
      title: '角色',
      dataIndex: 'roles',
      key: 'roles',
      render: (roles: string[], record: any) => (
        <Select
          mode="multiple"
          value={roles || []}
          options={roleOptions}
          style={{ minWidth: 200 }}
          onChange={(newRoles) => updateRolesMutation.mutate({ id: record.id, roles: newRoles })}
        />
      ),
    },
    {
      title: '最后登录',
      dataIndex: 'last_login_at',
      key: 'last_login_at',
      render: (d: string | null) => d ? new Date(d).toLocaleString() : '从未',
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
          用户管理
        </h1>
        <p style={{ fontSize: 14, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          管理系统用户及其权限角色
        </p>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>
    </div>
  );
}
