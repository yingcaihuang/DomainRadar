import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { Table, Button, Space, Tag, Input, Select, Upload, Modal, Badge, Card, message, App, Tooltip } from 'antd';
import { PlusOutlined, UploadOutlined, DownloadOutlined, DeleteOutlined, TagOutlined, SafetyCertificateOutlined, MailOutlined, GlobalOutlined, CloudOutlined } from '@ant-design/icons';
import type { Domain, ImportResult } from '../types';
import { domainApi, tagApi, groupApi, registrarApi, rulesApi } from '../services';

const { Search } = Input;

export function DomainListPage() {
  const { modal } = App.useApp();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [registrarFilter, setRegistrarFilter] = useState('');
  const [sortField, setSortField] = useState('expiration_date');
  const [sortOrder, setSortOrder] = useState<'ascend' | 'descend'>('ascend');
  const [tagFilter, setTagFilter] = useState('');
  const [groupFilter, setGroupFilter] = useState('');
  const [selectedRowKeys, setSelectedRowKeys] = useState<number[]>([]);
  const [importModalVisible, setImportModalVisible] = useState(false);
  const [importResult, setImportResult] = useState<ImportResult | null>(null);

  const params: Record<string, string> = { page: String(page), page_size: String(pageSize) };
  if (search) params.search = search;
  if (statusFilter) params.status = statusFilter;
  if (registrarFilter) params.registrar_account_id = registrarFilter;
  if (tagFilter) params.tag_ids = tagFilter;
  if (groupFilter) params.group_id = groupFilter;
  if (sortField) params.sort_by = sortField;
  if (sortOrder) params.sort_order = sortOrder === 'ascend' ? 'asc' : 'desc';

  const { data, isLoading } = useQuery({
    queryKey: ['domains', params],
    queryFn: () => domainApi.list(params),
  });

  const { data: tagsData } = useQuery({ queryKey: ['tags'], queryFn: tagApi.list });
  const { data: registrarsData } = useQuery({ queryKey: ['registrars'], queryFn: registrarApi.list });
  const { data: groupsData } = useQuery({ queryKey: ['groups'], queryFn: groupApi.list });
  const { data: rulesData } = useQuery({ queryKey: ['expiration-rules'], queryFn: rulesApi.list });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => domainApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      message.success('域名已删除');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const bulkMutation = useMutation({
    mutationFn: domainApi.bulk,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      setSelectedRowKeys([]);
      message.success('批量操作完成');
    },
    onError: (e: Error) => message.error(e.message),
  });

  const handleImport = async (file: File) => {
    try {
      const result = await domainApi.import(file);
      setImportResult(result.data);
      queryClient.invalidateQueries({ queryKey: ['domains'] });
      message.success(`导入完成: 创建 ${result.data.created} 个, 更新 ${result.data.updated} 个`);
    } catch (e: any) {
      message.error(e.message || '导入失败');
    }
  };

  const severityColor = (score: number) => {
    if (score >= 80) return 'green';
    if (score >= 60) return 'gold';
    if (score >= 40) return 'orange';
    return 'red';
  };

  const columns = [
    {
      title: '域名',
      dataIndex: 'domain_name',
      key: 'domain_name',
      sorter: true,
      sortOrder: sortField === 'domain_name' ? sortOrder : undefined,
      render: (text: string, record: Domain) => (
        <a onClick={() => navigate(`/domains/${record.id}`)} style={{ color: '#6366f1', fontWeight: 500 }}>{text}</a>
      ),
    },
    {
      title: '注册商',
      dataIndex: 'registrar_identifier',
      key: 'registrar_identifier',
    },
    {
      title: '到期时间',
      dataIndex: 'expiration_date',
      key: 'expiration_date',
      sorter: true,
      sortOrder: sortField === 'expiration_date' ? sortOrder : undefined,
      render: (date: string | null) => {
        if (!date) return '-';
        const d = new Date(date);
        const days = Math.ceil((d.getTime() - Date.now()) / (1000 * 60 * 60 * 24));
        const formatted = d.toLocaleDateString('zh-CN');
        const rules = rulesData?.data || [];
        const rule = rules.find((r: any) => days >= r.days_min && days < r.days_max);
        if (rule) {
          const daysText = days < 0 ? `已过期${Math.abs(days)}天` : `${days}天`;
          return <span style={{ color: rule.color, fontWeight: days <= 30 ? 600 : 400 }}>{formatted} <span style={{ background: rule.color + '18', color: rule.color, padding: '2px 8px', borderRadius: 6, fontSize: 12, fontWeight: 600 }}>{rule.label} {daysText}</span></span>;
        }
        return <span>{formatted} <span style={{ color: '#9ca3af', fontSize: 12 }}>({days}天)</span></span>;
      },
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (status: string) => (
        <Tag color={status === 'active' ? 'green' : status === 'expired' ? 'red' : 'default'}
          style={{ borderRadius: 6 }}>{status}</Tag>
      ),
    },
    {
      title: '健康度',
      dataIndex: 'health_score',
      key: 'health_score',
      render: (score: number) => <Badge color={severityColor(score)} text={`${score}/100`} />,
    },
    {
      title: '标签',
      dataIndex: 'tags',
      key: 'tags',
      render: (tags: { id: number; name: string }[]) => (
        <Space size={2} wrap>
          {tags?.slice(0, 3).map(t => <Tag key={t.id} color="purple" style={{ borderRadius: 6 }}>{t.name}</Tag>)}
          {tags?.length > 3 && <Tag style={{ borderRadius: 6 }}>+{tags.length - 3}</Tag>}
        </Space>
      ),
    },
    {
      title: '分组',
      dataIndex: 'group',
      key: 'group',
      render: (group: { id: number; name: string } | null) => group ? <Tag color="blue" style={{ borderRadius: 6 }}>{group.name}</Tag> : '-',
    },
    {
      title: '监控',
      key: 'monitors',
      width: 140,
      render: (_: any, record: Domain) => {
        const r = record as any;
        // WHOIS status
        const whoisTip = r.whois_checked
          ? `WHOIS: 已检测${r.whois_last_checked_at ? ' (' + new Date(r.whois_last_checked_at).toLocaleDateString('zh-CN') + ')' : ''}`
          : 'WHOIS: 未检测（点击查看）';
        const whoisColor = r.whois_checked ? '#6366f1' : '#d1d5db';
        // Service monitor
        const svcTip = r.service_monitor_enabled
          ? `服务监控: 已启用${r.service_uptime_percent != null ? ' | 可用率' + r.service_uptime_percent.toFixed(1) + '%' : ''}`
          : '服务监控: 未配置（点击添加）';
        const svcColor = !r.service_monitor_enabled ? '#d1d5db' : r.service_uptime_percent != null && r.service_uptime_percent < 100 ? '#f59e0b' : '#10b981';
        // Cert
        const certTip = r.cert_monitor_enabled
          ? `证书: 已启用${r.cert_days_remaining != null ? ' | 剩余' + r.cert_days_remaining + '天' : ''}`
          : '证书: 未配置';
        const certColor = !r.cert_monitor_enabled ? '#d1d5db' : r.cert_days_remaining != null && r.cert_days_remaining <= 30 ? '#f59e0b' : '#10b981';
        // Email
        const emailTip = r.email_monitor_enabled
          ? `邮件: 已启用${r.email_score != null ? ' | ' + r.email_score + '分' : ''}`
          : '邮件: 未配置';
        const emailColor = !r.email_monitor_enabled ? '#d1d5db' : r.email_score != null && r.email_score < 70 ? '#f59e0b' : '#10b981';
        return (
          <Space size={3}>
            <Tooltip title={whoisTip}><span style={{ color: whoisColor, fontSize: 15, cursor: 'pointer' }} onClick={(e) => { e.stopPropagation(); navigate(`/domains/${record.id}?tab=whois`); }}><GlobalOutlined /></span></Tooltip>
            <Tooltip title={svcTip}><span style={{ color: svcColor, fontSize: 15, cursor: 'pointer' }} onClick={(e) => { e.stopPropagation(); navigate(`/domains/${record.id}?tab=monitoring`); }}><CloudOutlined /></span></Tooltip>
            <Tooltip title={certTip}><span style={{ color: certColor, fontSize: 15, cursor: 'pointer' }} onClick={(e) => { e.stopPropagation(); navigate(`/domains/${record.id}?tab=cert-monitor`); }}><SafetyCertificateOutlined /></span></Tooltip>
            <Tooltip title={emailTip}><span style={{ color: emailColor, fontSize: 15, cursor: 'pointer' }} onClick={(e) => { e.stopPropagation(); navigate(`/domains/${record.id}?tab=email-security`); }}><MailOutlined /></span></Tooltip>
          </Space>
        );
      },
    },
    {
      title: '操作',
      key: 'actions',
      width: 80,
      render: (_: any, record: Domain) => (
        <Button size="small" danger icon={<DeleteOutlined />} onClick={() => {
          modal.confirm({
            title: '确定删除此域名？',
            content: record.domain_name,
            onOk: () => deleteMutation.mutateAsync(record.id),
          });
        }} />
      ),
    },
  ];

  // Build registrar account options for filter
  const registrarOptions = (registrarsData?.data || []).map((r: any) => ({
    value: String(r.id),
    label: `${r.account_name} (${r.registrar_type})`,
  }));

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <div>
            <h1 style={{
              fontSize: 32, fontWeight: 700, margin: 0,
              background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
              WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text',
            }}>
              域名管理
            </h1>
            <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>管理您的所有域名资产</p>
          </div>
          <Space>
            <Button icon={<UploadOutlined />} onClick={() => setImportModalVisible(true)}>导入</Button>
            <Button icon={<DownloadOutlined />} onClick={() => domainApi.export()}>导出</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/domains/new')}>添加域名</Button>
          </Space>
        </div>
      </div>

      {/* Filters */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }} styles={{ body: { padding: '16px 20px' } }}>
        <Space wrap>
          <Search placeholder="搜索域名..." onSearch={setSearch} allowClear style={{ width: 250 }} />
          <Select placeholder="状态" allowClear style={{ width: 130 }} onChange={(v) => { setStatusFilter(v || ''); setPage(1); }}
            options={[
              { value: 'active', label: '正常' },
              { value: 'expired', label: '已过期' },
            ]}
          />
          <Select placeholder="注册商账号" allowClear style={{ width: 200 }} onChange={(v) => { setRegistrarFilter(v || ''); setPage(1); }}
            options={registrarOptions}
          />
          <Select placeholder="标签" allowClear style={{ width: 150 }} onChange={(v) => { setTagFilter(v ? String(v) : ''); setPage(1); }}
            options={(tagsData?.data || []).map((t: any) => ({ value: t.id, label: t.name }))}
          />
          <Select placeholder="分组" allowClear style={{ width: 150 }} onChange={(v) => { setGroupFilter(v ? String(v) : ''); setPage(1); }}
            options={(groupsData?.data || []).map((g: any) => ({ value: g.id, label: g.name }))}
          />
        </Space>
      </Card>

      {/* Bulk Actions */}
      {selectedRowKeys.length > 0 && (
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }} styles={{ body: { padding: '12px 20px' } }}>
          <Space>
            <span style={{ color: '#6b7280' }}>已选 {selectedRowKeys.length} 项</span>
            <Button size="small" icon={<TagOutlined />} onClick={() => {
              const firstTag = tagsData?.data?.[0];
              if (firstTag) {
                bulkMutation.mutate({ domain_ids: selectedRowKeys, action: 'tag', tag_ids: [firstTag.id] });
              }
            }}>添加标签</Button>
            <Button size="small" danger icon={<DeleteOutlined />} onClick={() => {
              modal.confirm({
                title: `确定删除所选的 ${selectedRowKeys.length} 个域名？`,
                content: '此操作不可撤销',
                onOk: () => bulkMutation.mutateAsync({ domain_ids: selectedRowKeys, action: 'delete' }),
              });
            }}>批量删除</Button>
          </Space>
        </Card>
      )}

      {/* Table */}
      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ body: { padding: 0 } }}>
        <Table
          rowKey="id"
          columns={columns}
          dataSource={data?.domains || []}
          loading={isLoading}
          onChange={(_pagination, _filters, sorter: any) => {
            if (sorter.order) {
              setSortField(sorter.field);
              setSortOrder(sorter.order);
            } else {
              // Reset to default when sort is cancelled
              setSortField('expiration_date');
              setSortOrder('ascend');
            }
            setPage(1);
          }}
          rowSelection={{
            selectedRowKeys,
            onChange: (keys) => setSelectedRowKeys(keys as number[]),
          }}
          pagination={{
            current: page,
            pageSize,
            total: data?.total || 0,
            onChange: (p, ps) => { setPage(p); setPageSize(ps); },
            showSizeChanger: true,
            showTotal: (total) => `共 ${total} 个域名`,
          }}
        />
      </Card>

      <Modal title="导入域名" open={importModalVisible} onCancel={() => { setImportModalVisible(false); setImportResult(null); }} footer={null}>
        <Upload.Dragger accept=".csv,.xlsx,.xls" beforeUpload={(file) => { handleImport(file); return false; }} showUploadList={false}>
          <p>点击或拖拽 CSV/Excel 文件以导入域名</p>
          <p style={{ color: '#999' }}>最大 10MB，5000 行</p>
        </Upload.Dragger>
        {importResult && (
          <div style={{ marginTop: 16 }}>
            <p>总计: {importResult.total_rows} | 创建: {importResult.created} | 更新: {importResult.updated} | 错误: {importResult.total_errors}</p>
            {importResult.errors.length > 0 && (
              <ul style={{ maxHeight: 200, overflow: 'auto' }}>
                {importResult.errors.map((e, i) => <li key={i} style={{ color: 'red' }}>第 {e.row} 行: {e.field} - {e.reason}</li>)}
              </ul>
            )}
          </div>
        )}
      </Modal>
    </div>
  );
}
