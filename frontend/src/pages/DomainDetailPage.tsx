import { useParams, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Col, Row, Descriptions, Tag, Spin, Button, Space, Tabs, Progress, Badge, Table, Modal, Form, Input, Select, message, Popconfirm } from 'antd';
import { EditOutlined, ArrowLeftOutlined, GlobalOutlined, SafetyCertificateOutlined, CloudOutlined, PlusOutlined, SyncOutlined, DeleteOutlined } from '@ant-design/icons';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { domainApi, monitorApi, certApi } from '../services';
import type { CertMonitor } from '../services';
import { useState } from 'react';

// Format date to Chinese format
function formatDate(dateStr: string | null | undefined): string {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return dateStr;
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`;
}

function formatDateTime(dateStr: string | null | undefined): string {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return dateStr;
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 ${d.getHours().toString().padStart(2, '0')}:${d.getMinutes().toString().padStart(2, '0')}`;
}

function daysUntil(dateStr: string | null | undefined): string {
  if (!dateStr) return '-';
  const d = new Date(dateStr);
  if (isNaN(d.getTime())) return '-';
  const days = Math.ceil((d.getTime() - Date.now()) / (1000 * 60 * 60 * 24));
  if (days < 0) return `已过期 ${Math.abs(days)} 天`;
  if (days === 0) return '今天到期';
  return `${days} 天后到期`;
}

export function DomainDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const domainId = Number(id);
  const queryClient = useQueryClient();
  const [addModalOpen, setAddModalOpen] = useState(false);
  const [addForm] = Form.useForm();

  const { data: domainData, isLoading } = useQuery({
    queryKey: ['domain', domainId],
    queryFn: () => domainApi.get(domainId),
    enabled: !!domainId,
  });

  const { data: whoisData, isLoading: whoisLoading } = useQuery({
    queryKey: ['whois', domainId],
    queryFn: () => domainApi.whois(domainId),
    enabled: !!domainId,
  });

  const { data: uptimeData } = useQuery({
    queryKey: ['uptime', domainId],
    queryFn: () => monitorApi.uptime(domainId, '7d'),
    enabled: !!domainId,
  });

  const { data: certData } = useQuery({
    queryKey: ['certificate', domainId],
    queryFn: () => monitorApi.certificate(domainId),
    enabled: !!domainId,
  });

  const { data: websiteData } = useQuery({
    queryKey: ['website-checks', domainId],
    queryFn: () => monitorApi.website(domainId),
    enabled: !!domainId,
  });

  // Certificate monitoring queries
  const { data: certMonitors, isLoading: certMonitorsLoading } = useQuery({
    queryKey: ['cert-monitors', domainId],
    queryFn: () => certApi.listMonitors(domainId),
    enabled: !!domainId,
  });

  const addMonitorMutation = useMutation({
    mutationFn: (data: { endpoint: string; label: string }) => certApi.addMonitor(domainId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cert-monitors', domainId] });
      setAddModalOpen(false);
      addForm.resetFields();
      message.success('端点已添加');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const deleteMonitorMutation = useMutation({
    mutationFn: (monitorId: number) => certApi.deleteMonitor(monitorId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cert-monitors', domainId] });
      message.success('端点已删除');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const checkNowMutation = useMutation({
    mutationFn: (monitorId: number) => certApi.checkNow(monitorId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['cert-monitors', domainId] });
      message.success('检测完成');
    },
    onError: (err: Error) => message.error(err.message),
  });

  if (isLoading) return <div style={{ display: 'flex', justifyContent: 'center', padding: 100 }}><Spin size="large" /></div>;

  const domain = domainData?.data;
  if (!domain) return <div>域名未找到</div>;

  const whois = whoisData?.data;

  const responseTimeData = websiteData?.checks?.slice(0, 30).reverse().map((c: any) => ({
    time: new Date(c.checked_at).toLocaleTimeString(),
    ms: c.response_time_ms,
  })) || [];

  const tabItems = [
    {
      key: 'overview',
      label: '基本信息',
      children: (
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              <Descriptions bordered column={{ xs: 1, sm: 2, lg: 3 }} size="small">
                <Descriptions.Item label="域名">{domain.domain_name}</Descriptions.Item>
                <Descriptions.Item label="状态"><Tag color={domain.status === 'active' ? 'green' : 'red'} style={{ borderRadius: 6 }}>{domain.status}</Tag></Descriptions.Item>
                <Descriptions.Item label="健康评分"><Progress type="circle" size={40} percent={domain.health_score} /></Descriptions.Item>
                <Descriptions.Item label="注册商">{domain.registrar_identifier || '-'}</Descriptions.Item>
                <Descriptions.Item label="数据来源"><Tag style={{ borderRadius: 6 }}>{domain.data_source_type}</Tag></Descriptions.Item>
                <Descriptions.Item label="自动续费">{domain.auto_renew ? <Tag color="blue">是</Tag> : <Tag>否</Tag>}</Descriptions.Item>
                <Descriptions.Item label="到期时间">
                  <span>{formatDate(domain.expiration_date)}</span>
                  {domain.expiration_date && <span style={{ marginLeft: 8, fontSize: 12, color: '#6b7280' }}>({daysUntil(domain.expiration_date)})</span>}
                </Descriptions.Item>
                <Descriptions.Item label="注册时间">{formatDate(domain.creation_date)}</Descriptions.Item>
                <Descriptions.Item label="最后同步">{formatDateTime(domain.last_sync_at)}</Descriptions.Item>
                <Descriptions.Item label="隐私保护">{domain.privacy_protection ? '已开启' : '未开启'}</Descriptions.Item>
                <Descriptions.Item label="锁定状态">{domain.lock_status ? '已锁定' : '未锁定'}</Descriptions.Item>
                <Descriptions.Item label="邮箱服务">{domain.email_enabled ? '已启用' : '未启用'}</Descriptions.Item>
                <Descriptions.Item label="DNS 服务器" span={3}>{domain.nameservers?.join(', ') || '-'}</Descriptions.Item>
                <Descriptions.Item label="标签" span={2}>{domain.tags?.length ? domain.tags.map((t: any) => <Tag key={t.id} color="purple" style={{ borderRadius: 6 }}>{t.name}</Tag>) : '-'}</Descriptions.Item>
                <Descriptions.Item label="分组">{domain.group?.name || '-'}</Descriptions.Item>
                <Descriptions.Item label="网站地址" span={2}>{domain.website_url || '-'}</Descriptions.Item>
                <Descriptions.Item label="备注" span={3}>{domain.notes || '-'}</Descriptions.Item>
              </Descriptions>
            </Card>
          </Col>
        </Row>
      ),
    },
    {
      key: 'whois',
      label: 'WHOIS 信息',
      children: whoisLoading ? <Spin /> : whois ? (
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title={<><GlobalOutlined style={{ marginRight: 8 }} />域名注册信息</>} style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              <Descriptions column={1} size="small" bordered>
                <Descriptions.Item label="域名">{whois.domain || '-'}</Descriptions.Item>
                <Descriptions.Item label="顶级域">{whois.tld || '-'}</Descriptions.Item>
                <Descriptions.Item label="注册商">{whois.registrar?.name || '-'}</Descriptions.Item>
                <Descriptions.Item label="IANA ID">{whois.registrar?.ianaId || '-'}</Descriptions.Item>
                <Descriptions.Item label="注册商网站">{whois.registrar?.url || '-'}</Descriptions.Item>
                <Descriptions.Item label="滥用投诉邮箱">{whois.registrar?.abuseEmail || '-'}</Descriptions.Item>
                <Descriptions.Item label="滥用投诉电话">{whois.registrar?.abusePhone || '-'}</Descriptions.Item>
                <Descriptions.Item label="注册时间">{formatDateTime(whois.dates?.created)}</Descriptions.Item>
                <Descriptions.Item label="更新时间">{formatDateTime(whois.dates?.updated)}</Descriptions.Item>
                <Descriptions.Item label="到期时间">{formatDateTime(whois.dates?.expires)}</Descriptions.Item>
                <Descriptions.Item label="DNSSEC">{whois.dnssec?.signed ? <Tag color="green">已签名</Tag> : <Tag>未签名</Tag>}</Descriptions.Item>
              </Descriptions>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title={<><CloudOutlined style={{ marginRight: 8 }} />DNS 服务器</>} style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 16 }}>
              {whois.nameservers?.length ? (
                <ul style={{ margin: 0, paddingLeft: 20 }}>
                  {whois.nameservers.map((ns: any, i: number) => (
                    <li key={i} style={{ marginBottom: 4 }}>
                      <strong>{ns.name || ns}</strong>
                      {ns.ipv4?.length > 0 && <span style={{ marginLeft: 8, color: '#6b7280' }}>IPv4: {ns.ipv4.join(', ')}</span>}
                      {ns.ipv6?.length > 0 && <span style={{ marginLeft: 8, color: '#6b7280' }}>IPv6: {ns.ipv6.join(', ')}</span>}
                    </li>
                  ))}
                </ul>
              ) : <span style={{ color: '#9ca3af' }}>无数据</span>}
            </Card>
            <Card title={<><SafetyCertificateOutlined style={{ marginRight: 8 }} />域名状态码</>} style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              {whois.status?.length ? (
                <Space wrap>
                  {whois.status.map((s: string, i: number) => <Tag key={i} color="blue" style={{ borderRadius: 6 }}>{s}</Tag>)}
                </Space>
              ) : <span style={{ color: '#9ca3af' }}>无数据</span>}
            </Card>
          </Col>
          {(whois.contacts?.registrant || whois.contacts?.admin || whois.contacts?.tech) && (
            <Col span={24}>
              <Card title="联系人信息" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
                <Row gutter={16}>
                  {['registrant', 'admin', 'tech'].map(type => {
                    const contact = whois.contacts?.[type];
                    if (!contact) return null;
                    const label = type === 'registrant' ? '注册人' : type === 'admin' ? '管理员' : '技术联系人';
                    return (
                      <Col xs={24} sm={8} key={type}>
                        <h4 style={{ marginBottom: 8, color: '#374151' }}>{label}</h4>
                        {contact.redacted ? (
                          <Tag color="orange" style={{ borderRadius: 6 }}>已隐私保护</Tag>
                        ) : (
                          <Descriptions column={1} size="small">
                            {contact.name && <Descriptions.Item label="姓名">{contact.name}</Descriptions.Item>}
                            {contact.organization && <Descriptions.Item label="组织">{contact.organization}</Descriptions.Item>}
                            {contact.email && <Descriptions.Item label="邮箱">{contact.email}</Descriptions.Item>}
                            {contact.phone && <Descriptions.Item label="电话">{contact.phone}</Descriptions.Item>}
                            {contact.address?.country && <Descriptions.Item label="国家">{contact.address.country}</Descriptions.Item>}
                          </Descriptions>
                        )}
                      </Col>
                    );
                  })}
                </Row>
              </Card>
            </Col>
          )}
        </Row>
      ) : <div style={{ textAlign: 'center', padding: 40, color: '#9ca3af' }}>无法获取 WHOIS 信息</div>,
    },
    {
      key: 'monitoring',
      label: '监控数据',
      children: (
        <Row gutter={[16, 16]}>
          <Col xs={24} sm={8}>
            <Card title="可用率 (7天)" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              <div style={{ textAlign: 'center' }}>
                <Progress type="circle" percent={uptimeData?.uptime_percentage || 0} format={(p) => `${p}%`}
                  status={uptimeData && uptimeData.uptime_percentage < 99 ? 'exception' : 'normal'} />
              </div>
            </Card>
          </Col>
          <Col xs={24} sm={8}>
            <Card title="响应时间" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              <Descriptions column={1} size="small">
                <Descriptions.Item label="平均">{uptimeData?.avg_response_time_ms?.toFixed(0) || '-'} ms</Descriptions.Item>
                <Descriptions.Item label="最大">{uptimeData?.max_response_time_ms || '-'} ms</Descriptions.Item>
                <Descriptions.Item label="最小">{uptimeData?.min_response_time_ms || '-'} ms</Descriptions.Item>
              </Descriptions>
            </Card>
          </Col>
          <Col xs={24} sm={8}>
            <Card title="SSL 证书" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              {certData?.latest ? (
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="颁发机构">{certData.latest.issuer}</Descriptions.Item>
                  <Descriptions.Item label="到期时间">{formatDate(certData.latest.valid_to)}</Descriptions.Item>
                  <Descriptions.Item label="剩余天数">{certData.latest.days_remaining} 天</Descriptions.Item>
                  <Descriptions.Item label="证书链">{certData.latest.chain_complete ? <Tag color="green">完整</Tag> : <Tag color="red">不完整</Tag>}</Descriptions.Item>
                </Descriptions>
              ) : <p style={{ color: '#9ca3af', textAlign: 'center' }}>暂无证书数据</p>}
            </Card>
          </Col>
          <Col span={24}>
            <Card title="响应时间趋势" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
              {responseTimeData.length > 0 ? (
                <ResponsiveContainer width="100%" height={200}>
                  <LineChart data={responseTimeData}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#f3f4f6" />
                    <XAxis dataKey="time" />
                    <YAxis />
                    <Tooltip />
                    <Line type="monotone" dataKey="ms" stroke="#6366f1" dot={false} strokeWidth={2} />
                  </LineChart>
                </ResponsiveContainer>
              ) : <p style={{ color: '#9ca3af', textAlign: 'center' }}>暂无监控数据</p>}
            </Card>
          </Col>
        </Row>
      ),
    },
    {
      key: 'cert-monitor',
      label: '证书监控',
      children: (
        <Row gutter={[16, 16]}>
          <Col span={24}>
            <Card
              title={<><SafetyCertificateOutlined style={{ marginRight: 8 }} />证书监控端点</>}
              style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
              extra={<Button type="primary" icon={<PlusOutlined />} onClick={() => setAddModalOpen(true)}>添加端点</Button>}
            >
              <Table<CertMonitor>
                loading={certMonitorsLoading}
                dataSource={certMonitors?.data || []}
                rowKey="id"
                pagination={false}
                columns={[
                  {
                    title: '端点',
                    dataIndex: 'endpoint',
                    key: 'endpoint',
                    render: (text: string, record: CertMonitor) => (
                      <span>{text}{record.label && <Tag style={{ marginLeft: 8, borderRadius: 6 }}>{record.label}</Tag>}</span>
                    ),
                  },
                  {
                    title: '证书主体',
                    key: 'subject',
                    render: (_: unknown, record: CertMonitor) => record.latest?.subject || '-',
                  },
                  {
                    title: '颁发机构',
                    key: 'issuer',
                    render: (_: unknown, record: CertMonitor) => record.latest?.issuer || '-',
                  },
                  {
                    title: '到期时间',
                    key: 'valid_to',
                    render: (_: unknown, record: CertMonitor) => record.latest?.valid_to ? formatDate(record.latest.valid_to) : '-',
                  },
                  {
                    title: '剩余天数',
                    key: 'days_remaining',
                    render: (_: unknown, record: CertMonitor) => {
                      if (!record.latest) return '-';
                      if (record.latest.error) return <Tag color="red">检测失败</Tag>;
                      const days = record.latest.days_remaining;
                      const color = days <= 7 ? 'red' : days <= 30 ? 'orange' : 'green';
                      return <Tag color={color}>{days} 天</Tag>;
                    },
                  },
                  {
                    title: '证书链',
                    key: 'chain',
                    render: (_: unknown, record: CertMonitor) => {
                      if (!record.latest || record.latest.error) return '-';
                      return record.latest.chain_complete
                        ? <Tag color="green">完整</Tag>
                        : <Tag color="red">不完整</Tag>;
                    },
                  },
                  {
                    title: '操作',
                    key: 'actions',
                    render: (_: unknown, record: CertMonitor) => (
                      <Space>
                        <Button
                          size="small"
                          icon={<SyncOutlined />}
                          loading={checkNowMutation.isPending}
                          onClick={() => checkNowMutation.mutate(record.id)}
                        >
                          立即检测
                        </Button>
                        <Popconfirm title="确认删除此端点?" onConfirm={() => deleteMonitorMutation.mutate(record.id)}>
                          <Button size="small" danger icon={<DeleteOutlined />}>删除</Button>
                        </Popconfirm>
                      </Space>
                    ),
                  },
                ]}
              />
            </Card>
          </Col>

          {/* Add Endpoint Modal */}
          <Modal
            title="添加证书监控端点"
            open={addModalOpen}
            onCancel={() => setAddModalOpen(false)}
            onOk={() => addForm.submit()}
            confirmLoading={addMonitorMutation.isPending}
          >
            <Form
              form={addForm}
              layout="vertical"
              onFinish={(values) => addMonitorMutation.mutate(values)}
            >
              <Form.Item label="端点地址" name="endpoint" rules={[{ required: true, message: '请输入端点地址' }]}>
                <Select
                  mode="tags"
                  maxCount={1}
                  placeholder="选择或输入端点 (如 www.example.com:443)"
                  options={domain ? [
                    { value: `${domain.domain_name}:443`, label: `${domain.domain_name}:443 (主域名)` },
                    { value: `www.${domain.domain_name}:443`, label: `www.${domain.domain_name}:443 (WWW)` },
                    { value: `api.${domain.domain_name}:443`, label: `api.${domain.domain_name}:443 (API)` },
                    { value: `mail.${domain.domain_name}:443`, label: `mail.${domain.domain_name}:443 (邮箱)` },
                    { value: `cdn.${domain.domain_name}:443`, label: `cdn.${domain.domain_name}:443 (CDN)` },
                  ] : []}
                  onChange={(values) => {
                    if (values && values.length > 0) {
                      addForm.setFieldsValue({ endpoint: values[values.length - 1] });
                    }
                  }}
                />
              </Form.Item>
              <Form.Item label="标签" name="label">
                <Input placeholder="如：主站、API、邮箱" />
              </Form.Item>
            </Form>
          </Modal>
        </Row>
      ),
    },
  ];

  return (
    <div>
      {/* Header */}
      <div style={{ marginBottom: 24 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <Space align="center">
            <Button icon={<ArrowLeftOutlined />} type="text" onClick={() => navigate('/domains')} />
            <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
              {domain.domain_name}
            </h1>
            <Tag color={domain.status === 'active' ? 'green' : 'red'} style={{ borderRadius: 6, marginLeft: 8 }}>{domain.status}</Tag>
            <Badge color={domain.health_score >= 80 ? 'green' : domain.health_score >= 60 ? 'gold' : 'red'} text={`健康 ${domain.health_score}/100`} />
          </Space>
          <Button icon={<EditOutlined />} onClick={() => navigate(`/domains/${domainId}/edit`)}>编辑</Button>
        </div>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          {domain.expiration_date ? `到期时间：${formatDate(domain.expiration_date)}（${daysUntil(domain.expiration_date)}）` : '未知到期时间'}
        </p>
      </div>

      <Tabs items={tabItems} />
    </div>
  );
}
