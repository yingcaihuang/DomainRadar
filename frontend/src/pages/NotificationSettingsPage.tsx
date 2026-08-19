import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Button, Modal, Form, Input, Select, Tag, Space, Card, message } from 'antd';
import { PlusOutlined, ExperimentOutlined } from '@ant-design/icons';
import { notificationApi } from '../services';

const channelTypes = [
  { value: 'email', label: 'Email (SMTP)' },
  { value: 'wechat_work', label: '企业微信' },
  { value: 'sms', label: '短信' },
  { value: 'webhook', label: 'Webhook' },
];

export function NotificationSettingsPage() {
  const queryClient = useQueryClient();
  const [modalVisible, setModalVisible] = useState(false);
  const [form] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['notification-channels'],
    queryFn: notificationApi.channels.list,
  });

  const createMutation = useMutation({
    mutationFn: notificationApi.channels.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      setModalVisible(false);
      form.resetFields();
      message.success('渠道已创建');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const testMutation = useMutation({
    mutationFn: notificationApi.channels.test,
    onSuccess: () => message.success('测试成功'),
    onError: (e: Error) => message.error(`测试失败: ${e.message}`),
  });

  const deleteMutation = useMutation({
    mutationFn: notificationApi.channels.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      message.success('渠道已删除');
    },
  });

  const columns = [
    { title: '名称', dataIndex: 'name', key: 'name', render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span> },
    { title: '类型', dataIndex: 'channel_type', key: 'channel_type', render: (t: string) => <Tag style={{ borderRadius: 6 }}>{t}</Tag> },
    { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => <Tag color={s === 'active' ? 'green' : 'default'} style={{ borderRadius: 6 }}>{s === 'active' ? '正常' : s}</Tag> },
    { title: '最后测试', dataIndex: 'last_tested_at', key: 'last_tested_at', render: (d: string | null) => d ? new Date(d).toLocaleString() : '从未' },
    {
      title: '操作',
      key: 'actions',
      render: (_: any, record: any) => (
        <Space>
          <Button size="small" icon={<ExperimentOutlined />} onClick={() => testMutation.mutate(record.id)}>测试</Button>
          <Button size="small" danger onClick={() => Modal.confirm({ title: '确定删除此渠道？', onOk: () => deleteMutation.mutate(record.id) })}>删除</Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
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
              通知渠道
            </h1>
            <p style={{ fontSize: 14, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
              配置告警通知的发送渠道
            </p>
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalVisible(true)}>添加渠道</Button>
        </div>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>

      <Modal title="添加通知渠道" open={modalVisible} onCancel={() => setModalVisible(false)} onOk={() => form.submit()} confirmLoading={createMutation.isPending}>
        <Form form={form} layout="vertical" onFinish={(values) => {
          const config: Record<string, string> = {};
          if (values.smtp_host) config.smtp_host = values.smtp_host;
          if (values.smtp_port) config.smtp_port = values.smtp_port;
          if (values.smtp_user) config.smtp_user = values.smtp_user;
          if (values.smtp_pass) config.smtp_pass = values.smtp_pass;
          if (values.webhook_url) config.webhook_url = values.webhook_url;
          if (values.bot_token) config.bot_token = values.bot_token;
          createMutation.mutate({ channel_type: values.channel_type, name: values.name, config });
        }}>
          <Form.Item name="channel_type" label="渠道类型" rules={[{ required: true }]}>
            <Select options={channelTypes} />
          </Form.Item>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="webhook_url" label="Webhook URL">
            <Input placeholder="https://..." />
          </Form.Item>
          <Form.Item name="bot_token" label="Bot Token">
            <Input.Password />
          </Form.Item>
          <Form.Item name="smtp_host" label="SMTP 主机">
            <Input />
          </Form.Item>
          <Form.Item name="smtp_port" label="SMTP 端口">
            <Input />
          </Form.Item>
          <Form.Item name="smtp_user" label="SMTP 用户名">
            <Input />
          </Form.Item>
          <Form.Item name="smtp_pass" label="SMTP 密码">
            <Input.Password />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
