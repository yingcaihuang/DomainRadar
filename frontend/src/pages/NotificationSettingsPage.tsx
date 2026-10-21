import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Button, Modal, Form, Input, Select, Tag, Space, Card, message, App, Drawer } from 'antd';
import { PlusOutlined, ExperimentOutlined, DeleteOutlined, HistoryOutlined, EditOutlined } from '@ant-design/icons';
import { notificationApi } from '../services';
import type { NotificationLogEntry } from '../services';
import type { NotificationChannel } from '../types';

export function NotificationSettingsPage() {
  const { modal } = App.useApp();
  const queryClient = useQueryClient();
  const [modalVisible, setModalVisible] = useState(false);
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null);
  const [form] = Form.useForm();
  const [logsDrawer, setLogsDrawer] = useState<{ visible: boolean; channelId: number; channelName: string }>({ visible: false, channelId: 0, channelName: '' });
  const [logsPage, setLogsPage] = useState(1);

  const { data, isLoading } = useQuery({
    queryKey: ['notification-channels'],
    queryFn: notificationApi.channels.list,
  });

  const { data: logsData, isLoading: logsLoading } = useQuery({
    queryKey: ['channel-logs', logsDrawer.channelId, logsPage],
    queryFn: () => notificationApi.channels.logs(logsDrawer.channelId, logsPage),
    enabled: logsDrawer.visible && logsDrawer.channelId > 0,
  });

  const closeModal = () => {
    setModalVisible(false);
    setEditingChannel(null);
    form.resetFields();
  };
  const openAdd = () => { setEditingChannel(null); form.resetFields(); setModalVisible(true); };
  const openEdit = async (record: NotificationChannel) => {
    setEditingChannel(record);
    form.setFieldsValue({ channel_type: record.channel_type, name: record.name });
    setModalVisible(true);
    // 拉取解密后的真实配置回填表单，用户可见并按需修改
    try {
      const res = await notificationApi.channels.get(record.id);
      form.setFieldsValue(res.data.config);
    } catch (e: any) {
      message.error(`获取渠道配置失败: ${e.message}`);
      closeModal();
    }
  };

  const createMutation = useMutation({
    mutationFn: notificationApi.channels.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      closeModal();
      message.success('Webhook 渠道已创建');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: Parameters<typeof notificationApi.channels.update>[1] }) => notificationApi.channels.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      closeModal();
      message.success('通知渠道已更新');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const testMutation = useMutation({
    mutationFn: notificationApi.channels.test,
    onSuccess: (data: any) => {
      queryClient.invalidateQueries({ queryKey: ['notification-channels'] });
      queryClient.invalidateQueries({ queryKey: ['channel-logs'] });
      if (data.status === 'active') {
        message.success('✅ 测试通过 — 通知已成功送达目标平台');
      } else {
        message.error(`❌ 测试失败: ${data.message}`);
      }
    },
    onError: (e: Error) => message.error(`❌ 测试失败: ${e.message}`),
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
      render: (type: string) => {
        const typeMap: Record<string, { label: string; color: string }> = {
          webhook: { label: 'Webhook', color: 'blue' },
          cne_alarm: { label: 'CNE 告警', color: 'purple' },
        };
        const cfg = typeMap[type] || { label: type, color: 'default' };
        return <Tag color={cfg.color} style={{ borderRadius: 6 }}>{cfg.label}</Tag>;
      },
    },
    {
      title: '状态', dataIndex: 'status', key: 'status',
      render: (s: string) => <Tag color={s === 'active' ? 'green' : s === 'error' ? 'red' : 'default'} style={{ borderRadius: 6 }}>{s === 'active' ? '正常' : s === 'error' ? '异常' : '未激活'}</Tag>,
    },
    { title: '最后测试', dataIndex: 'last_tested_at', key: 'last_tested_at', render: (d: string | null) => d ? new Date(d).toLocaleString('zh-CN') : '从未' },
    {
      title: '操作',
      key: 'actions',
      render: (_: unknown, record: NotificationChannel) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(record)}>编辑</Button>
          <Button size="small" icon={<HistoryOutlined />} onClick={() => { setLogsDrawer({ visible: true, channelId: record.id, channelName: record.name }); setLogsPage(1); }}>日志</Button>
          <Button size="small" icon={<ExperimentOutlined />} onClick={() => testMutation.mutate(record.id)}>测试</Button>
          <Button size="small" danger icon={<DeleteOutlined />} onClick={() => modal.confirm({ title: '确定删除此渠道？', onOk: () => deleteMutation.mutateAsync(record.id) })}>删除</Button>
        </Space>
      ),
    },
  ];

  const logColumns = [
    {
      title: '时间',
      dataIndex: 'created_at',
      key: 'created_at',
      width: 150,
      render: (d: string) => new Date(d).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }),
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 70,
      render: (s: string) => <Tag color={s === 'sent' ? 'green' : s === 'failed' ? 'red' : 'orange'} style={{ borderRadius: 6 }}>{s === 'sent' ? '成功' : s === 'failed' ? '失败' : '重试'}</Tag>,
    },
    {
      title: '类型',
      dataIndex: 'alert_type',
      key: 'alert_type',
      width: 120,
      ellipsis: true,
    },
    {
      title: '域名',
      dataIndex: 'domain_name',
      key: 'domain_name',
      width: 160,
      ellipsis: true,
    },
    {
      title: '消息',
      dataIndex: 'message',
      key: 'message',
      width: 200,
      ellipsis: true,
    },
    {
      title: '错误',
      dataIndex: 'error_reason',
      key: 'error_reason',
      width: 150,
      ellipsis: true,
      render: (e: string) => e ? <span style={{ color: '#ef4444' }}>{e}</span> : '-',
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
          <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>添加 Webhook</Button>
        </div>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>

      <Modal title={editingChannel ? '编辑通知渠道' : '添加通知渠道'} open={modalVisible} onCancel={closeModal} onOk={() => form.submit()} confirmLoading={createMutation.isPending || updateMutation.isPending} okText={editingChannel ? '保存' : '创建'} cancelText="取消">
        <Form form={form} layout="vertical" initialValues={{ channel_type: 'webhook' }} onFinish={(values) => {
          // 编辑回填了真实配置，与新建一样构建完整 config 提交（api_url/alarm_code 空值走后端默认）
          const config: Record<string, string> = {};
          if (values.channel_type === 'webhook') {
            config.webhook_url = values.webhook_url;
          } else if (values.channel_type === 'cne_alarm') {
            config.api_url = values.api_url || 'https://cne.verycloud.cn/monitor/api/v1/alarms';
            config.api_key = values.api_key;
            if (values.alarm_code) config.alarm_code = values.alarm_code;
          }
          if (editingChannel) {
            updateMutation.mutate({ id: editingChannel.id, data: { name: values.name, config } });
          } else {
            createMutation.mutate({ channel_type: values.channel_type, name: values.name, config });
          }
        }}>
          <Form.Item name="channel_type" label="渠道类型" rules={[{ required: true }]}>
            <Select disabled={!!editingChannel} options={[
              { value: 'webhook', label: 'Webhook (钉钉/企微/飞书/Slack)' },
              { value: 'cne_alarm', label: 'CNE 告警平台' },
            ]} />
          </Form.Item>
          <Form.Item name="name" label="渠道名称" rules={[{ required: true, message: '请输入渠道名称' }]}>
            <Input placeholder="如：运维告警群、CNE告警" />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(prev, cur) => prev.channel_type !== cur.channel_type}>
            {({ getFieldValue }) => getFieldValue('channel_type') === 'webhook' ? (
              <Form.Item name="webhook_url" label="Webhook URL" rules={[{ required: true, message: '请输入 Webhook 地址' }]}
                extra="支持企业微信机器人、钉钉机器人、飞书机器人、Slack Webhook 等">
                <Input placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..." />
              </Form.Item>
            ) : (
              <>
                <Form.Item name="api_url" label="API 地址" extra="默认: https://cne.verycloud.cn/monitor/api/v1/alarms">
                  <Input placeholder="https://cne.verycloud.cn/monitor/api/v1/alarms" />
                </Form.Item>
                <Form.Item name="api_key" label="API Key (X-Api-Key)" rules={[{ required: true, message: '请输入 API Key' }]}>
                  <Input placeholder="4d3c2b1a-9f8e-47d6-a5c4-123456789abc" />
                </Form.Item>
                <Form.Item name="alarm_code" label="告警编码" extra="默认: 2809">
                  <Input placeholder="2809" />
                </Form.Item>
              </>
            )}
          </Form.Item>
        </Form>
      </Modal>

      <Drawer
        title={`通知日志 — ${logsDrawer.channelName}`}
        open={logsDrawer.visible}
        onClose={() => setLogsDrawer({ visible: false, channelId: 0, channelName: '' })}
        width={800}
      >
        <Table<NotificationLogEntry>
          rowKey="id"
          columns={logColumns}
          dataSource={logsData?.data || []}
          loading={logsLoading}
          size="small"
          scroll={{ x: 900 }}
          tableLayout="fixed"
          pagination={{
            current: logsPage,
            total: logsData?.total || 0,
            pageSize: 20,
            onChange: setLogsPage,
            showTotal: (total) => `共 ${total} 条记录`,
          }}
        />
      </Drawer>
    </div>
  );
}
