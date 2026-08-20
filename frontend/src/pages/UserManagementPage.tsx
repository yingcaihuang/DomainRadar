import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Select, Card, Button, Modal, Form, Input, Tag, Space, App } from 'antd';
import { PlusOutlined, EditOutlined, DeleteOutlined, KeyOutlined } from '@ant-design/icons';
import { userApi } from '../services';

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

interface UserRecord {
  id: number;
  external_id: string;
  email: string;
  display_name: string;
  auth_source: string;
  roles: string[];
  last_login_at: string | null;
  created_at: string;
}

export function UserManagementPage() {
  const queryClient = useQueryClient();
  const { modal, message } = App.useApp();
  const [createOpen, setCreateOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [resetPwOpen, setResetPwOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<UserRecord | null>(null);
  const [createForm] = Form.useForm();
  const [editForm] = Form.useForm();
  const [resetPwForm] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['users'],
    queryFn: userApi.list,
  });

  const createMutation = useMutation({
    mutationFn: userApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      message.success('用户创建成功');
      setCreateOpen(false);
      createForm.resetFields();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: { email?: string; display_name?: string; roles?: string[] } }) =>
      userApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      message.success('用户已更新');
      setEditOpen(false);
    },
    onError: (e: Error) => message.error(e.message),
  });

  const deleteMutation = useMutation({
    mutationFn: userApi.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['users'] });
      message.success('用户已删除');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const resetPwMutation = useMutation({
    mutationFn: ({ id, password }: { id: number; password: string }) =>
      userApi.resetPassword(id, password),
    onSuccess: () => {
      message.success('密码已重置');
      setResetPwOpen(false);
      resetPwForm.resetFields();
    },
    onError: (e: Error) => message.error(e.message),
  });

  const handleDelete = (user: UserRecord) => {
    modal.confirm({
      title: '确认删除',
      content: `确定要删除用户 "${user.display_name}" 吗？此操作不可撤销。`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: () => deleteMutation.mutate(user.id),
    });
  };

  const handleEdit = (user: UserRecord) => {
    setEditingUser(user);
    editForm.setFieldsValue({
      email: user.email,
      display_name: user.display_name,
      roles: user.roles,
    });
    setEditOpen(true);
  };

  const handleResetPassword = (user: UserRecord) => {
    setEditingUser(user);
    resetPwForm.resetFields();
    setResetPwOpen(true);
  };

  const columns = [
    {
      title: '用户名',
      dataIndex: 'external_id',
      key: 'external_id',
      render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span>,
    },
    { title: '邮箱', dataIndex: 'email', key: 'email' },
    { title: '显示名称', dataIndex: 'display_name', key: 'display_name' },
    {
      title: '来源',
      dataIndex: 'auth_source',
      key: 'auth_source',
      render: (s: string) => (
        <Tag color={s === 'oidc' ? 'geekblue' : 'green'}>{s === 'oidc' ? 'SSO' : '本地'}</Tag>
      ),
    },
    {
      title: '角色',
      dataIndex: 'roles',
      key: 'roles',
      render: (roles: string[]) => (
        <Space size={4}>
          {(roles || []).map((r) => (
            <Tag key={r} color={roleColors[r]}>{roleOptions.find(o => o.value === r)?.label || r}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '最后登录',
      dataIndex: 'last_login_at',
      key: 'last_login_at',
      render: (d: string | null) => d ? new Date(d).toLocaleString() : '从未',
    },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: UserRecord) => (
        <Space size={8}>
          <Button type="text" icon={<EditOutlined />} onClick={() => handleEdit(record)} />
          {record.auth_source !== 'oidc' && (
            <Button type="text" icon={<KeyOutlined />} onClick={() => handleResetPassword(record)} />
          )}
          <Button type="text" danger icon={<DeleteOutlined />} onClick={() => handleDelete(record)} />
        </Space>
      ),
    },
  ];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24, display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
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
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setCreateOpen(true)}>
          添加用户
        </Button>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.users || []}
          loading={isLoading}
          pagination={false}
        />
      </Card>

      {/* Create User Modal */}
      <Modal
        title="添加用户"
        open={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => createForm.submit()}
        confirmLoading={createMutation.isPending}
        okText="创建"
        cancelText="取消"
      >
        <Form
          form={createForm}
          layout="vertical"
          onFinish={(values) => createMutation.mutate(values)}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true, message: '请输入用户名' }]}>
            <Input placeholder="请输入用户名" />
          </Form.Item>
          <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email', message: '请输入有效邮箱' }]}>
            <Input placeholder="请输入邮箱" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称" rules={[{ required: true, message: '请输入显示名称' }]}>
            <Input placeholder="请输入显示名称" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, min: 6, message: '密码至少6位' }]}>
            <Input.Password placeholder="请输入密码" />
          </Form.Item>
          <Form.Item name="roles" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
            <Select mode="multiple" options={roleOptions} placeholder="请选择角色" />
          </Form.Item>
        </Form>
      </Modal>

      {/* Edit User Modal */}
      <Modal
        title="编辑用户"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => editForm.submit()}
        confirmLoading={updateMutation.isPending}
        okText="保存"
        cancelText="取消"
      >
        <Form
          form={editForm}
          layout="vertical"
          onFinish={(values) => editingUser && updateMutation.mutate({ id: editingUser.id, data: values })}
        >
          <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email', message: '请输入有效邮箱' }]}>
            <Input placeholder="请输入邮箱" />
          </Form.Item>
          <Form.Item name="display_name" label="显示名称" rules={[{ required: true, message: '请输入显示名称' }]}>
            <Input placeholder="请输入显示名称" />
          </Form.Item>
          <Form.Item name="roles" label="角色" rules={[{ required: true, message: '请选择角色' }]}>
            <Select mode="multiple" options={roleOptions} placeholder="请选择角色" />
          </Form.Item>
        </Form>
      </Modal>

      {/* Reset Password Modal */}
      <Modal
        title={`重置密码 - ${editingUser?.display_name || ''}`}
        open={resetPwOpen}
        onCancel={() => setResetPwOpen(false)}
        onOk={() => resetPwForm.submit()}
        confirmLoading={resetPwMutation.isPending}
        okText="重置"
        cancelText="取消"
      >
        <Form
          form={resetPwForm}
          layout="vertical"
          onFinish={(values) => editingUser && resetPwMutation.mutate({ id: editingUser.id, password: values.new_password })}
        >
          <Form.Item name="new_password" label="新密码" rules={[{ required: true, min: 6, message: '密码至少6位' }]}>
            <Input.Password placeholder="请输入新密码" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
