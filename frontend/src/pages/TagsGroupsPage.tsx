import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Table, Button, Input, Space, Form, Tag, message, Popconfirm, Modal } from 'antd';
import { PlusOutlined, DeleteOutlined, EditOutlined } from '@ant-design/icons';
import { tagApi, groupApi } from '../services';

export function TagsGroupsPage() {
  
  const queryClient = useQueryClient();
  const [tagInput, setTagInput] = useState('');
  const [groupModalVisible, setGroupModalVisible] = useState(false);
  const [editingGroup, setEditingGroup] = useState<any>(null);
  const [groupForm] = Form.useForm();

  // Tags
  const { data: tagsData, isLoading: tagsLoading } = useQuery({ queryKey: ['tags'], queryFn: tagApi.list });
  const createTagMutation = useMutation({
    mutationFn: tagApi.create,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tags'] }); setTagInput(''); message.success('标签已创建'); },
    onError: (e: Error) => message.error(e.message),
  });
  const deleteTagMutation = useMutation({
    mutationFn: tagApi.delete,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['tags'] }); message.success('标签已删除'); },
    onError: (e: Error) => message.error(e.message),
  });

  // Groups
  const { data: groupsData, isLoading: groupsLoading } = useQuery({ queryKey: ['groups'], queryFn: groupApi.list });
  const createGroupMutation = useMutation({
    mutationFn: groupApi.create,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['groups'] }); closeGroupModal(); message.success('分组已创建'); },
    onError: (e: Error) => message.error(e.message),
  });
  const updateGroupMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: any }) => groupApi.update(id, data),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['groups'] }); closeGroupModal(); message.success('分组已更新'); },
    onError: (e: Error) => message.error(e.message),
  });
  const deleteGroupMutation = useMutation({
    mutationFn: groupApi.delete,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['groups'] }); message.success('分组已删除'); },
    onError: (e: Error) => message.error(e.message),
  });

  const closeGroupModal = () => { setGroupModalVisible(false); setEditingGroup(null); groupForm.resetFields(); };
  const openAddGroup = () => { setEditingGroup(null); groupForm.resetFields(); setGroupModalVisible(true); };
  const openEditGroup = (record: any) => { setEditingGroup(record); groupForm.setFieldsValue({ name: record.name }); setGroupModalVisible(true); };

  const handleGroupSubmit = (values: any) => {
    if (editingGroup) {
      updateGroupMutation.mutate({ id: editingGroup.id, data: { name: values.name } });
    } else {
      createGroupMutation.mutate({ name: values.name });
    }
  };

  const tagColumns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: '标签名称', dataIndex: 'name', key: 'name', render: (name: string) => <Tag color="purple" style={{ borderRadius: 6 }}>{name}</Tag> },
    {
      title: '操作', key: 'actions', width: 100,
      render: (_: any, record: any) => (
        <Popconfirm title="确定删除此标签？" onConfirm={() => deleteTagMutation.mutate(record.id)}>
          <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
        </Popconfirm>
      ),
    },
  ];

  const groupColumns = [
    { title: 'ID', dataIndex: 'id', key: 'id', width: 80 },
    { title: '分组名称', dataIndex: 'name', key: 'name', render: (name: string) => <span style={{ fontWeight: 500 }}>{name}</span> },
    {
      title: '操作', key: 'actions', width: 160,
      render: (_: any, record: any) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEditGroup(record)}>编辑</Button>
          <Popconfirm title="确定删除此分组？" onConfirm={() => deleteGroupMutation.mutate(record.id)}>
            <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          标签与分组
        </h1>
        <p style={{ fontSize: 14, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>管理域名的标签和分组分类</p>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
        {/* Tags Card */}
        <Card title={<span style={{ fontWeight: 600 }}>标签管理</span>} style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
          <div style={{ marginBottom: 16, display: 'flex', gap: 8 }}>
            <Input placeholder="输入标签名称" value={tagInput} onChange={(e) => setTagInput(e.target.value)}
              onPressEnter={() => { if (tagInput.trim()) createTagMutation.mutate(tagInput.trim()); }}
              style={{ flex: 1 }} />
            <Button type="primary" icon={<PlusOutlined />} loading={createTagMutation.isPending}
              onClick={() => { if (tagInput.trim()) createTagMutation.mutate(tagInput.trim()); }}
              disabled={!tagInput.trim()}>
              添加
            </Button>
          </div>
          <Table rowKey="id" columns={tagColumns} dataSource={tagsData?.data || []} loading={tagsLoading}
            pagination={false} size="small" />
        </Card>

        {/* Groups Card */}
        <Card title={<span style={{ fontWeight: 600 }}>分组管理</span>} style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
          extra={<Button type="primary" icon={<PlusOutlined />} size="small" onClick={openAddGroup}>新建分组</Button>}>
          <Table rowKey="id" columns={groupColumns} dataSource={groupsData?.data || []} loading={groupsLoading}
            pagination={false} size="small" />
        </Card>
      </div>

      {/* Group Edit Modal */}
      <Modal title={editingGroup ? '编辑分组' : '新建分组'} open={groupModalVisible} onCancel={closeGroupModal}
        onOk={() => groupForm.submit()} confirmLoading={createGroupMutation.isPending || updateGroupMutation.isPending}>
        <Form form={groupForm} layout="vertical" onFinish={handleGroupSubmit}>
          <Form.Item name="name" label="分组名称" rules={[{ required: true, message: '请输入分组名称' }]}>
            <Input placeholder="例如：生产环境、测试环境" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
