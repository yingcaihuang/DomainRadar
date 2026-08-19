import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Table, Button, Modal, Form, Input, Select, Tag, Space, Card, message, Alert, App } from 'antd';
import { PlusOutlined, SyncOutlined, ApiOutlined, EditOutlined } from '@ant-design/icons';
import { registrarApi, tagApi, groupApi } from '../services';


const registrarTypes = [
  { value: 'godaddy', label: 'GoDaddy', icon: 'https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/godaddy.svg', color: '#1BDBDB' },
  { value: 'cloudflare', label: 'Cloudflare', icon: 'https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/cloudflare.svg', color: '#F38020' },
  { value: 'alibaba', label: 'Alibaba Cloud', icon: 'https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/alibabacloud.svg', color: '#FF6A00' },
  { value: 'tencent', label: 'Tencent Cloud', icon: 'https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/tencentqq.svg', color: '#12B7F5' },
  { value: 'namecheap', label: 'Namecheap', icon: 'https://cdn.jsdelivr.net/npm/simple-icons@latest/icons/namecheap.svg', color: '#DE3723' },
];

const registrarCredentialFields: Record<string, { key: string; label: string; required?: boolean; placeholder?: string }[]> = {
  godaddy: [
    { key: 'api_key', label: 'API Key', placeholder: '使用 API Token 时留空' },
    { key: 'api_secret', label: 'API Secret', placeholder: '使用 API Token 时留空' },
    { key: 'token', label: 'API Token (PAT)', placeholder: 'Personal Access Token' },
  ],
  cloudflare: [
    { key: 'token', label: 'API Token', required: true },
  ],
  alibaba: [
    { key: 'access_key_id', label: 'Access Key ID', required: true },
    { key: 'secret_access_key', label: 'Secret Access Key', required: true },
  ],
  tencent: [
    { key: 'access_key_id', label: 'SecretId', required: true },
    { key: 'secret_access_key', label: 'SecretKey', required: true },
  ],
  namecheap: [
    { key: 'username', label: 'Username', required: true },
    { key: 'api_key', label: 'API Key', required: true },
    { key: 'ip_whitelist', label: 'Whitelisted IP', placeholder: '用于 API 访问的公网 IP' },
  ],
};

interface PreviewDomain {
  domain_name: string;
  status: string;
  expiration_date: string | null;
  auto_renew: boolean;
  creation_date: string | null;
}

export function RegistrarSettingsPage() {
  const { modal } = App.useApp();
  const queryClient = useQueryClient();
  const [modalVisible, setModalVisible] = useState(false);
  const [editingRecord, setEditingRecord] = useState<any>(null);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);
  const [testLoading, setTestLoading] = useState(false);
  const [testingId, setTestingId] = useState<number | null>(null);
  const [form] = Form.useForm();
  const selectedType = Form.useWatch('registrar_type', form);

  // Sync preview state
  const [previewVisible, setPreviewVisible] = useState(false);
  const [previewData, setPreviewData] = useState<PreviewDomain[]>([]);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [selectedDomains, setSelectedDomains] = useState<string[]>([]);
  const [importLoading, setImportLoading] = useState(false);
  const [previewAccountId, setPreviewAccountId] = useState<number | null>(null);
  const [statusFilter, setStatusFilter] = useState<string>('active');
  const [importTagIds, setImportTagIds] = useState<number[]>([]);
  const [importGroupId, setImportGroupId] = useState<number | undefined>(undefined);

  const { data: tagsData } = useQuery({ queryKey: ['tags'], queryFn: tagApi.list });
  const { data: groupsData } = useQuery({ queryKey: ['groups'], queryFn: groupApi.list });

  const { data, isLoading } = useQuery({
    queryKey: ['registrars'],
    queryFn: registrarApi.list,
  });

  const createMutation = useMutation({
    mutationFn: registrarApi.create,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registrars'] });
      closeModal();
      message.success('注册商已添加');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: any }) => registrarApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registrars'] });
    },
    onError: (e: Error) => message.error(e.message),
  });


  const deleteMutation = useMutation({
    mutationFn: registrarApi.delete,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registrars'] });
      message.success('注册商已删除');
    },
  });

  const closeModal = () => {
    setModalVisible(false);
    setEditingRecord(null);
    setTestResult(null);
    form.resetFields();
  };

  const openAdd = () => { setEditingRecord(null); setTestResult(null); form.resetFields(); setModalVisible(true); };
  const openEdit = (record: any) => {
    setEditingRecord(record); setTestResult(null);
    form.setFieldsValue({ registrar_type: record.registrar_type, account_name: record.account_name });
    setModalVisible(true);
  };

  // Sync preview flow
  const handleSyncPreview = async (accountId: number) => {
    setPreviewAccountId(accountId);
    setPreviewLoading(true);
    setPreviewVisible(true);
    setPreviewData([]);
    setSelectedDomains([]);
    setStatusFilter('active');
    setImportTagIds([]);
    setImportGroupId(undefined);
    try {
      const result = await registrarApi.previewSync(accountId);
      const domains = result.data || [];
      setPreviewData(domains);
      // Default select all active domains
      const activeDomains = domains.filter((d: PreviewDomain) => d.status === 'active').map((d: PreviewDomain) => d.domain_name);
      setSelectedDomains(activeDomains);
    } catch (e: any) {
      message.error(`获取域名列表失败: ${e.message}`);
      setPreviewVisible(false);
    } finally {
      setPreviewLoading(false);
    }
  };

  const handleImport = async () => {
    if (!previewAccountId || selectedDomains.length === 0) {
      message.warning('请至少选择一个域名');
      return;
    }
    setImportLoading(true);
    try {
      const importData: any = { domain_names: selectedDomains };
      if (importTagIds.length > 0) importData.tag_ids = importTagIds;
      if (importGroupId) importData.group_id = importGroupId;
      const result: any = await registrarApi.importDomains(previewAccountId, importData);
      message.success(`导入完成: 成功导入 ${result.imported} 个域名`);
      setPreviewVisible(false);
      queryClient.invalidateQueries({ queryKey: ['registrars'] });
      queryClient.invalidateQueries({ queryKey: ['domains'] });
    } catch (e: any) {
      message.error(`导入失败: ${e.message}`);
    } finally {
      setImportLoading(false);
    }
  };

  const filteredPreview = statusFilter === 'all'
    ? previewData
    : previewData.filter(d => d.status === statusFilter);

  const handleSaveAndTest = async () => {
    try {
      const values = await form.validateFields();
      const fields = registrarCredentialFields[values.registrar_type] || [];
      const credentials: Record<string, string> = {};
      for (const field of fields) { if (values[field.key]) credentials[field.key] = values[field.key]; }
      setTestLoading(true); setTestResult(null);
      if (editingRecord) {
        const updateData: any = { account_name: values.account_name };
        if (Object.keys(credentials).length > 0) updateData.credentials = credentials;
        await registrarApi.update(editingRecord.id, updateData);
        queryClient.invalidateQueries({ queryKey: ['registrars'] });
        try { await registrarApi.test(editingRecord.id); setTestResult({ success: true, message: '连接成功!' }); }
        catch (e: any) { setTestResult({ success: false, message: e.message || '连接失败' }); }
      } else {
        try {
          const result: any = await registrarApi.create({ registrar_type: values.registrar_type, display_name: registrarTypes.find(r => r.value === values.registrar_type)?.label || values.registrar_type, account_name: values.account_name, credentials });
          queryClient.invalidateQueries({ queryKey: ['registrars'] });
          const newId = result?.data?.id;
          if (newId) { setEditingRecord({ ...result, id: newId, registrar_type: values.registrar_type }); try { await registrarApi.test(newId); setTestResult({ success: true, message: '连接成功!' }); } catch (e: any) { setTestResult({ success: false, message: e.message || '连接失败' }); } }
        } catch (e: any) { message.error(e.message); }
      }
    } catch {} finally { setTestLoading(false); }
  };

  const handleSubmit = (values: any) => {
    const fields = registrarCredentialFields[values.registrar_type] || [];
    const credentials: Record<string, string> = {};
    for (const field of fields) { if (values[field.key]) credentials[field.key] = values[field.key]; }
    if (editingRecord) {
      const updateData: any = { account_name: values.account_name };
      if (Object.keys(credentials).length > 0) updateData.credentials = credentials;
      updateMutation.mutate({ id: editingRecord.id, data: updateData }, { onSuccess: () => { closeModal(); message.success('注册商已更新'); } });
    } else {
      createMutation.mutate({ registrar_type: values.registrar_type, display_name: registrarTypes.find(r => r.value === values.registrar_type)?.label || values.registrar_type, account_name: values.account_name, credentials });
    }
  };

  const columns = [
    { title: '账号名称', dataIndex: 'account_name', key: 'account_name', render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span> },
    { title: '类型', dataIndex: 'registrar_type', key: 'registrar_type', render: (t: string) => {
      const r = registrarTypes.find(x => x.value === t);
      return <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>{r && <span style={{ width: 22, height: 22, borderRadius: 6, background: (r.color || '#6366f1') + '15', display: 'inline-flex', alignItems: 'center', justifyContent: 'center' }}><img src={r.icon} alt={t} style={{ width: 13, height: 13 }} /></span>}<Tag style={{ borderRadius: 6 }}>{r?.label || t}</Tag></span>;
    }},
    { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => <Tag color={s === 'connected' ? 'green' : 'red'} style={{ borderRadius: 6 }}>{s === 'connected' ? '已连接' : '未连接'}</Tag> },
    { title: '域名数', dataIndex: 'domain_count', key: 'domain_count' },
    { title: '最后同步', dataIndex: 'last_sync_at', key: 'last_sync_at', render: (d: string | null) => d ? new Date(d).toLocaleString() : '从未' },
    {
      title: '操作',
      key: 'actions',
      render: (_: any, record: any) => (
        <Space>
          <Button size="small" icon={<EditOutlined />} onClick={() => openEdit(record)}>编辑</Button>
          <Button size="small" icon={<ApiOutlined />} loading={testingId === record.id} onClick={() => {
            setTestingId(record.id);
            registrarApi.test(record.id)
              .then(() => { message.success('连接成功'); })
              .catch((e: any) => { message.error('连接失败: ' + e.message); })
              .finally(() => { setTestingId(null); queryClient.invalidateQueries({ queryKey: ['registrars'] }); });
          }}>测试</Button>
          <Button size="small" icon={<SyncOutlined />} onClick={() => handleSyncPreview(record.id)}>同步</Button>
          <Button size="small" danger onClick={() => modal.confirm({ title: '确定删除此注册商？', onOk: () => deleteMutation.mutate(record.id) })}>删除</Button>
        </Space>
      ),
    },
  ];

  const credentialFields = registrarCredentialFields[selectedType] || [];

  // Preview table columns
  const previewColumns = [
    { title: '域名', dataIndex: 'domain_name', key: 'domain_name', render: (t: string) => <span style={{ fontWeight: 500 }}>{t}</span> },
    { title: '状态', dataIndex: 'status', key: 'status', render: (s: string) => {
      const colorMap: Record<string, string> = { active: 'green', expired: 'red', cancelled: 'default', locked: 'orange' };
      return <Tag color={colorMap[s] || 'default'}>{s}</Tag>;
    }},
    { title: '到期时间', dataIndex: 'expiration_date', key: 'expiration_date', render: (d: string | null) => d || '-' },
    { title: '自动续费', dataIndex: 'auto_renew', key: 'auto_renew', render: (v: boolean) => v ? <Tag color="blue">是</Tag> : <Tag>否</Tag> },
    { title: '注册时间', dataIndex: 'creation_date', key: 'creation_date', render: (d: string | null) => d || '-' },
  ];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          注册商配置
        </h1>
        <p style={{ fontSize: 14, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>管理域名注册商账号与 API 集成</p>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}
        title={<span style={{ fontWeight: 600 }}>注册商列表</span>}
        extra={<Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>添加注册商</Button>}
      >
        <Table rowKey="id" columns={columns} dataSource={data?.data || []} loading={isLoading} pagination={false} />
      </Card>

      {/* Edit/Add Modal */}
      <Modal title={editingRecord ? '编辑注册商' : '添加注册商'} open={modalVisible} onCancel={closeModal}
        footer={[
          <Button key="cancel" onClick={closeModal}>取消</Button>,
          <Button key="test" icon={<ApiOutlined />} loading={testLoading} onClick={handleSaveAndTest}>保存并测试</Button>,
          <Button key="ok" type="primary" loading={createMutation.isPending || updateMutation.isPending} onClick={() => form.submit()}>{editingRecord ? '保存' : '确定'}</Button>,
        ]}
      >
        {testResult && <Alert type={testResult.success ? 'success' : 'error'} message={testResult.message} style={{ marginBottom: 16 }} closable onClose={() => setTestResult(null)} />}
        <Form form={form} layout="vertical" onFinish={handleSubmit}>
          <Form.Item name="registrar_type" label="注册商类型" rules={[{ required: true }]}>
            <Select disabled={!!editingRecord}>
              {registrarTypes.map(r => (
                <Select.Option key={r.value} value={r.value}>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <span style={{ width: 24, height: 24, borderRadius: 6, background: r.color + '15', display: 'flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}>
                      <img src={r.icon} alt={r.label} style={{ width: 14, height: 14, filter: 'none' }} />
                    </span>
                    {r.label}
                  </span>
                </Select.Option>
              ))}
            </Select>
          </Form.Item>
          <Form.Item name="account_name" label="账号名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          {credentialFields.map(field => (
            <Form.Item key={field.key} name={field.key} label={field.label} rules={!editingRecord && field.required ? [{ required: true }] : []} extra={editingRecord ? '留空保持现有值' : undefined}>
              <Input.Password placeholder={field.placeholder} />
            </Form.Item>
          ))}
        </Form>
      </Modal>

      {/* Sync Preview Modal */}
      <Modal
        title="同步预览 — 选择要导入的域名"
        open={previewVisible}
        onCancel={() => setPreviewVisible(false)}
        width={900}
        footer={[
          <Button key="cancel" onClick={() => setPreviewVisible(false)}>取消</Button>,
          <Button key="import" type="primary" loading={importLoading} disabled={selectedDomains.length === 0} onClick={handleImport}>
            确认导入 ({selectedDomains.length} 个域名)
          </Button>,
        ]}
      >
        <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', gap: 12 }}>
          <span style={{ fontSize: 13, color: '#6b7280' }}>状态筛选：</span>
          <Select value={statusFilter} onChange={(v) => {
            setStatusFilter(v);
            if (v === 'all') {
              setSelectedDomains(previewData.map(d => d.domain_name));
            } else {
              setSelectedDomains(previewData.filter(d => d.status === v).map(d => d.domain_name));
            }
          }} style={{ width: 120 }} options={[
            { value: 'all', label: '全部' },
            { value: 'active', label: 'Active' },
            { value: 'expired', label: 'Expired' },
            { value: 'cancelled', label: 'Cancelled' },
          ]} />
          <span style={{ fontSize: 13, color: '#9ca3af' }}>
            共 {previewData.length} 个域名，已选 {selectedDomains.length} 个
          </span>
          <Button size="small" onClick={() => setSelectedDomains(filteredPreview.map(d => d.domain_name))}>全选当前</Button>
          <Button size="small" onClick={() => setSelectedDomains([])}>取消全选</Button>
        </div>
        <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 13, color: '#6b7280' }}>导入时打标签：</span>
          <Select mode="multiple" placeholder="选择标签" allowClear style={{ minWidth: 200 }} value={importTagIds} onChange={setImportTagIds}
            options={(tagsData?.data || []).map((t: any) => ({ value: t.id, label: t.name }))} />
          <span style={{ fontSize: 13, color: '#6b7280', marginLeft: 8 }}>分入组：</span>
          <Select placeholder="选择组" allowClear style={{ width: 150 }} value={importGroupId} onChange={setImportGroupId}
            options={(groupsData?.data || []).map((g: any) => ({ value: g.id, label: g.name }))} />
        </div>
        <Table
          rowKey="domain_name"
          columns={previewColumns}
          dataSource={filteredPreview}
          loading={previewLoading}
          size="small"
          pagination={{ pageSize: 50, showTotal: (total) => `共 ${total} 条` }}
          rowSelection={{
            selectedRowKeys: selectedDomains,
            onChange: (keys) => setSelectedDomains(keys as string[]),
          }}
        />
      </Modal>
    </div>
  );
}
