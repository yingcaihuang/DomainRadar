import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Button, Modal, Form, Input, Tag, Space, Card, message, App } from 'antd';
import { PlusOutlined, ExperimentOutlined, DeleteOutlined } from '@ant-design/icons';
import { notificationApi } from '../services';

export function NotificationSettingsPage() {
  const { modal } = App.useApp();
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
      message.success('Webhook 渠道已创建');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const testMutation = useMutation({
    mutationFn: notificationApi.channels.test,
    onSuccess: () => message.success('Webhook 测试发送成功'),
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
    {
      title: '类型', dataIndex: 'channel_type', key: 'channel_type',
      render: () => <Tag color="blue" style={{ borderRadius: 6 }}>Webhook</Tag>,
    },
    {
      title: '状态', dataIndex: 'status', key: 'status',
      render: (s: string) => <Tag color={s === 'active' ? 'green' : 'default'} style={{ borderRadius: 6 }}>{s === 'active' ? '正常' : '未激活'}</Tag>,
    },
    { title: '最后测试', dataIndex: 'last_tested_at', key: 'last_tested_at', render: (d: string | null) => d ? new Date(d).toLocaleString('zh-CN') : '从未' },
    {
      title: '操作',
      key: 'actions',
      render: (_: any, record: any) => (
        <Space>
          <Button size="small" icon={<ExperimentOutlined />} onClick={() => testMutation.mutate(record.id)}>测试</Button>
          <Button size="small" danger icon={<DeleteOutlined />} onClick={() => modal.confirm({ title: '确定删除此渠道？', onOk: () => deleteMutation.mutate(record.id) })}>删除</Button>
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
              通知渠道
            </h1>
            <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>配置告警通知的 Webhook 推送地址</p>
          </div>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => setModalVisible(true)}>添加 Webhook</Button>
        </div>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>

      <Modal title="添加 Webhook 渠道" open={modalVisible} onCancel={() => setModalVisible(false)} onOk={() => form.submit()} confirmLoading={createMutation.isPending} okText="创建" cancelText="取消">
        <Form form={form} layout="vertical" onFinish={(values) => {
          createMutation.mutate({
            channel_type: 'webhook',
            name: values.name,
            config: { webhook_url: values.webhook_url },
          });
        }}>
          <Form.Item name="name" label="渠道名称" rules={[{ required: true, message: '请输入渠道名称' }]}>
            <Input placeholder="如：运维告警群、DomainRadar通知" />
          </Form.Item>
          <Form.Item name="webhook_url" label="Webhook URL" rules={[{ required: true, message: '请输入 Webhook 地址' }]}
            extra="支持企业微信机器人、钉钉机器人、飞书机器人、Slack Webhook 等">
            <Input placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..." />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
