import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Button, Modal, Form, Input, InputNumber, Select, Card, Space, message, App, Tag } from 'antd';
import { PlusOutlined, ReloadOutlined, DeleteOutlined, EditOutlined } from '@ant-design/icons';
import { rulesApi, emailRulesApi } from '../services';

interface ExpirationRule {
  id: number;
  days_min: number;
  days_max: number;
  severity: string;
  color: string;
  label: string;
  score: number;
  sort_order: number;
}

export function ExpirationRulesPage() {
  const { modal } = App.useApp();
  const queryClient = useQueryClient();
  const [modalVisible, setModalVisible] = useState(false);
  const [editingRule, setEditingRule] = useState<ExpirationRule | null>(null);
  const [emailModalVisible, setEmailModalVisible] = useState(false);
  const [editingEmailRule, setEditingEmailRule] = useState<any>(null);
  const [emailForm] = Form.useForm();
  const [form] = Form.useForm();

  const { data, isLoading } = useQuery({
    queryKey: ['expiration-rules'],
    queryFn: rulesApi.list,
  });

  const createMutation = useMutation({
    mutationFn: rulesApi.create,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['expiration-rules'] }); closeModal(); message.success('规则已创建'); },
    onError: (e: Error) => message.error(e.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: any }) => rulesApi.update(id, data),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['expiration-rules'] }); closeModal(); message.success('规则已更新'); },
    onError: (e: Error) => message.error(e.message),
  });

  const deleteMutation = useMutation({
    mutationFn: rulesApi.delete,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['expiration-rules'] }); message.success('规则已删除'); },
  });

  const resetMutation = useMutation({
    mutationFn: rulesApi.resetDefaults,
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['expiration-rules'] }); message.success('已恢复默认规则'); },
  });

  // Email alert rules
  const { data: emailRulesData, isLoading: emailRulesLoading } = useQuery({ queryKey: ['email-alert-rules'], queryFn: emailRulesApi.list });
  const emailCreateMutation = useMutation({ mutationFn: emailRulesApi.create, onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['email-alert-rules'] }); closeEmailModal(); message.success('规则已创建'); } });
  const emailUpdateMutation = useMutation({ mutationFn: ({ id, data }: { id: number; data: any }) => emailRulesApi.update(id, data), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['email-alert-rules'] }); closeEmailModal(); message.success('规则已更新'); } });
  const emailDeleteMutation = useMutation({ mutationFn: emailRulesApi.delete, onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['email-alert-rules'] }); message.success('规则已删除'); } });
  const emailResetMutation = useMutation({ mutationFn: emailRulesApi.resetDefaults, onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['email-alert-rules'] }); message.success('已恢复默认规则'); } });

  const closeEmailModal = () => { setEmailModalVisible(false); setEditingEmailRule(null); emailForm.resetFields(); };
  const openAddEmail = () => { setEditingEmailRule(null); emailForm.resetFields(); setEmailModalVisible(true); };
  const openEditEmail = (rule: any) => { setEditingEmailRule(rule); emailForm.setFieldsValue(rule); setEmailModalVisible(true); };
  const handleEmailSubmit = (values: any) => {
    if (editingEmailRule) { emailUpdateMutation.mutate({ id: editingEmailRule.id, data: values }); }
    else { emailCreateMutation.mutate(values); }
  };

  const ruleTypeLabels: Record<string, string> = { total_score: '总分阈值', score_drop: '单次下降', mx_score: 'MX', spf_score: 'SPF', dkim_score: 'DKIM', dmarc_score: 'DMARC' };
  const emailColumns = [
    { title: '规则类型', dataIndex: 'rule_type', key: 'rule_type', render: (t: string) => ruleTypeLabels[t] || t },
    { title: '阈值', dataIndex: 'threshold', key: 'threshold' },
    { title: '级别', dataIndex: 'severity', key: 'severity', render: (s: string) => <Tag color={s === 'critical' ? 'red' : s === 'warning' ? 'orange' : 'blue'}>{s}</Tag> },
    { title: '描述', dataIndex: 'description', key: 'description' },
    { title: '状态', dataIndex: 'enabled', key: 'enabled', render: (e: boolean) => <Tag color={e ? 'green' : 'default'}>{e ? '启用' : '禁用'}</Tag> },
    { title: '操作', key: 'actions', render: (_: any, r: any) => <Space><Button size="small" icon={<EditOutlined />} onClick={() => openEditEmail(r)} /><Button size="small" danger icon={<DeleteOutlined />} onClick={() => modal.confirm({ title: '删除此规则？', onOk: () => emailDeleteMutation.mutate(r.id) })} /></Space> },
  ];

  const closeModal = () => { setModalVisible(false); setEditingRule(null); form.resetFields(); };

  const openAdd = () => { setEditingRule(null); form.resetFields(); setModalVisible(true); };
  const openEdit = (rule: ExpirationRule) => {
    setEditingRule(rule);
    form.setFieldsValue(rule);
    setModalVisible(true);
  };

  const handleSubmit = (values: any) => {
    const data = { ...values, color: typeof values.color === 'string' ? values.color : values.color?.toHexString?.() || values.color };
    if (editingRule) {
      updateMutation.mutate({ id: editingRule.id, data });
    } else {
      createMutation.mutate(data);
    }
  };

  const columns = [
    {
      title: '天数范围', key: 'range', width: 150,
      render: (_: any, r: ExpirationRule) => {
        if (r.days_min <= -9999) return `≤ ${r.days_max} 天（已过期）`;
        if (r.days_max >= 9999) return `≥ ${r.days_min} 天`;
        return `${r.days_min} ~ ${r.days_max} 天`;
      },
    },
    {
      title: '标签', dataIndex: 'label', key: 'label',
      render: (label: string, r: ExpirationRule) => (
        <span style={{ background: r.color + '20', color: r.color, padding: '3px 10px', borderRadius: 6, fontWeight: 600, fontSize: 13 }}>
          {label}
        </span>
      ),
    },
    { title: '严重级别', dataIndex: 'severity', key: 'severity' },
    {
      title: '颜色', dataIndex: 'color', key: 'color',
      render: (color: string) => (
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <span style={{ width: 20, height: 20, borderRadius: 4, background: color, display: 'inline-block', border: '1px solid #e5e7eb' }} />
          <code style={{ fontSize: 12 }}>{color}</code>
        </span>
      ),
    },
    { title: '健康评分', dataIndex: 'score', key: 'score', render: (s: number) => `${s}/100` },
    { title: '排序', dataIndex: 'sort_order', key: 'sort_order' },
    {
      title: '操作', key: 'actions', width: 120,
      render: (_: any, r: ExpirationRule) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(r)} />
          <Button size="small" danger icon={<DeleteOutlined />} onClick={() => modal.confirm({ title: '确定删除此规则？', onOk: () => deleteMutation.mutate(r.id) })} />
        </Space>
      ),
    },
  ];

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          到期规则配置
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>配置域名到期天数的告警级别、颜色和健康评分</p>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
        title={<span style={{ fontWeight: 600 }}>规则列表</span>}
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => modal.confirm({ title: '恢复默认规则？', content: '当前所有自定义规则将被删除', onOk: () => resetMutation.mutate() })}>恢复默认</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>添加规则</Button>
          </Space>
        }
      >
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>

      <Modal title={editingRule ? '编辑规则' : '添加规则'} open={modalVisible} onCancel={closeModal} onOk={() => form.submit()}
        confirmLoading={createMutation.isPending || updateMutation.isPending}>
        <Form form={form} layout="vertical" onFinish={handleSubmit}>
          <Space style={{ width: '100%' }} direction="vertical">
            <Space>
              <Form.Item name="days_min" label="最小天数" rules={[{ required: true }]}>
                <InputNumber style={{ width: 120 }} placeholder="-99999" />
              </Form.Item>
              <Form.Item name="days_max" label="最大天数" rules={[{ required: true }]}>
                <InputNumber style={{ width: 120 }} placeholder="99999" />
              </Form.Item>
            </Space>
            <Form.Item name="label" label="标签文字" rules={[{ required: true }]}>
              <Input placeholder="如：已过期、即将到期、正常" />
            </Form.Item>
            <Space>
              <Form.Item name="severity" label="严重级别" rules={[{ required: true }]}>
                <Select style={{ width: 130 }} options={[
                  { value: 'critical', label: '严重' },
                  { value: 'warning', label: '警告' },
                  { value: 'info', label: '信息' },
                  { value: 'ok', label: '正常' },
                ]} />
              </Form.Item>
              <Form.Item name="color" label="颜色" rules={[{ required: true }]}>
                <Input placeholder="#ef4444" style={{ width: 120 }} />
              </Form.Item>
              <Form.Item name="score" label="健康评分" rules={[{ required: true }]}>
                <InputNumber min={0} max={100} style={{ width: 100 }} />
              </Form.Item>
              <Form.Item name="sort_order" label="排序">
                <InputNumber style={{ width: 80 }} />
              </Form.Item>
            </Space>
          </Space>
        </Form>
      </Modal>

      {/* Email Alert Rules */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginTop: 24 }}
        title={<span style={{ fontWeight: 600 }}>邮件安全报警规则</span>}
        extra={
          <Space>
            <Button icon={<ReloadOutlined />} onClick={() => modal.confirm({ title: '恢复邮件规则默认值？', onOk: () => emailResetMutation.mutate() })}>恢复默认</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAddEmail}>添加规则</Button>
          </Space>
        }
      >
        <Table rowKey="id" columns={emailColumns} dataSource={emailRulesData?.data || []} loading={emailRulesLoading} pagination={false} />
      </Card>

      {/* Email Rule Modal */}
      <Modal title={editingEmailRule ? '编辑邮件报警规则' : '添加邮件报警规则'} open={emailModalVisible} onCancel={closeEmailModal} onOk={() => emailForm.submit()}
        confirmLoading={emailCreateMutation.isPending || emailUpdateMutation.isPending}>
        <Form form={emailForm} layout="vertical" onFinish={handleEmailSubmit}>
          <Form.Item name="rule_type" label="规则类型" rules={[{ required: true }]}>
            <Select options={[
              { value: 'total_score', label: '总分阈值' },
              { value: 'score_drop', label: '单次下降幅度' },
              { value: 'mx_score', label: 'MX 记录分数' },
              { value: 'spf_score', label: 'SPF 记录分数' },
              { value: 'dkim_score', label: 'DKIM 记录分数' },
              { value: 'dmarc_score', label: 'DMARC 记录分数' },
            ]} />
          </Form.Item>
          <Form.Item name="threshold" label="阈值（低于/超过此值触发告警）" rules={[{ required: true }]}>
            <InputNumber min={0} max={100} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="severity" label="告警级别" rules={[{ required: true }]}>
            <Select options={[
              { value: 'critical', label: '严重' },
              { value: 'warning', label: '警告' },
              { value: 'info', label: '信息' },
            ]} />
          </Form.Item>
          <Form.Item name="description" label="描述">
            <Input placeholder="如：邮件安全总分低于 50 分" />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked" initialValue={true}>
            <Select options={[{ value: true, label: '启用' }, { value: false, label: '禁用' }]} />
          </Form.Item>
        </Form>
      </Modal>

    </div>
  );
}
