import { useParams, useNavigate, useSearchParams } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Card, Col, Row, Descriptions, Tag, Spin, Button, Space, Tabs, Progress, Badge, Table, Modal, Form, Input, Select, message, Popconfirm, Collapse } from 'antd';
import { EditOutlined, ArrowLeftOutlined, GlobalOutlined, SafetyCertificateOutlined, CloudOutlined, PlusOutlined, SyncOutlined, DeleteOutlined, MailOutlined } from '@ant-design/icons';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { domainApi, monitorApi, certApi, emailMonitorApi } from '../services';
import type { CertMonitor, EmailMonitorData, EmailCheckResultData, EmailCheckDetails, ServiceMonitorItem, ServiceCheckItem, ServiceMonitorStats } from '../services';
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

// Grade color helper
function gradeColor(grade: string): string {
  switch (grade) {
    case 'A': return '#52c41a';
    case 'B': return '#faad14';
    case 'C': return '#fa8c16';
    case 'D': return '#f5222d';
    default: return '#d9d9d9';
  }
}

// Score bar color
function scoreBarColor(score: number, max: number): string {
  const pct = max > 0 ? score / max : 0;
  if (pct >= 0.8) return '#52c41a';
  if (pct >= 0.5) return '#faad14';
  return '#f5222d';
}

// Category labels for display
const emailCategories = [
  { key: 'mx', label: 'MX 记录', maxScore: 30 },
  { key: 'spf', label: 'SPF 记录', maxScore: 20 },
  { key: 'dkim', label: 'DKIM 记录', maxScore: 20 },
  { key: 'dmarc', label: 'DMARC 记录', maxScore: 15 },
  { key: 'ptr', label: 'PTR 记录', maxScore: 5 },
  { key: 'mta_sts', label: 'MTA-STS', maxScore: 5 },
  { key: 'tlsrpt', label: 'TLSRPT', maxScore: 3 },
  { key: 'bimi', label: 'BIMI', maxScore: 2 },
] as const;

interface EmailSecurityTabProps {
  domainId: number;
  emailMonitorData?: EmailMonitorData;
  emailMonitorLoading: boolean;
  emailHistoryData?: EmailCheckResultData[];
  emailConfigForm: any;
  emailConfigMutation: any;
  emailCheckMutation: any;
}

function EmailSecurityTab({ emailMonitorData, emailMonitorLoading, emailHistoryData, emailConfigForm, emailConfigMutation, emailCheckMutation }: EmailSecurityTabProps) {
  const result = emailMonitorData?.latest_result;

  return (
    <Row gutter={[16, 16]}>
      {/* Score Overview Card */}
      <Col span={24}>
        <Card
          style={{ borderRadius: 12, border: '1px solid #e5e7eb', overflow: 'hidden' }}
          styles={{ body: { padding: 0 } }}
        >
          <div style={{ background: 'linear-gradient(135deg, #7c3aed, #a855f7)', padding: '24px 32px', color: '#fff' }}>
            <Space align="center" size={16}>
              <MailOutlined style={{ fontSize: 28 }} />
              <span style={{ fontSize: 20, fontWeight: 600 }}>邮件安全评分</span>
            </Space>
            {result && (
              <div style={{ display: 'flex', alignItems: 'center', gap: 24, marginTop: 16 }}>
                <div style={{ textAlign: 'center' }}>
                  <div style={{ fontSize: 48, fontWeight: 700, lineHeight: 1 }}>{result.total_score}</div>
                  <div style={{ opacity: 0.8, marginTop: 4 }}>/100</div>
                </div>
                <Tag
                  style={{ fontSize: 24, fontWeight: 700, padding: '4px 16px', borderRadius: 8, border: 'none', background: 'rgba(255,255,255,0.2)', color: '#fff' }}
                >
                  等级: {result.grade}
                </Tag>
              </div>
            )}
            {!result && !emailMonitorLoading && (
              <div style={{ marginTop: 16, opacity: 0.8 }}>尚未进行检测，请点击"立即检测"开始</div>
            )}
          </div>

          {/* Score Bars */}
          {result && result.details && (
            <div style={{ padding: '24px 32px' }}>
              {emailCategories.map(cat => {
                const detail = result.details![cat.key as keyof EmailCheckDetails];
                const score = detail?.score ?? 0;
                const max = cat.maxScore;
                const pct = max > 0 ? (score / max) * 100 : 0;
                return (
                  <div key={cat.key} style={{ marginBottom: 12 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                      <span style={{ fontWeight: 500, color: '#374151' }}>{cat.label}</span>
                      <span style={{ color: '#6b7280', fontFamily: 'monospace' }}>{score}/{max}</span>
                    </div>
                    <Progress
                      percent={pct}
                      showInfo={false}
                      strokeColor={scoreBarColor(score, max)}
                      trailColor="#f3f4f6"
                      size="small"
                    />
                  </div>
                );
              })}
            </div>
          )}
        </Card>
      </Col>

      {/* Detailed Findings */}
      {result && result.details && (
        <Col span={24}>
          <Card title="详细检测结果" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
            <Collapse
              items={emailCategories.map(cat => {
                const detail = result.details![cat.key as keyof EmailCheckDetails];
                const score = detail?.score ?? 0;
                const max = cat.maxScore;
                const findings = detail?.findings ?? [];
                return {
                  key: cat.key,
                  label: (
                    <Space>
                      <span style={{ fontWeight: 500 }}>{cat.label}</span>
                      <Tag color={score === max ? 'green' : score > 0 ? 'orange' : 'red'} style={{ borderRadius: 6 }}>
                        {score}/{max}
                      </Tag>
                    </Space>
                  ),
                  children: (
                    <ul style={{ margin: 0, paddingLeft: 20, listStyle: 'none' }}>
                      {findings.length > 0 ? findings.map((f: string, i: number) => {
                        const isRecord = f.startsWith('  →');
                        return (
                          <li key={i} style={{ marginBottom: isRecord ? 2 : 6, color: isRecord ? '#6366f1' : '#4b5563' }}>
                            {isRecord ? (
                              <code style={{ background: '#f3f4f6', padding: '2px 8px', borderRadius: 4, fontSize: 12, fontFamily: 'monospace' }}>
                                {f.replace('  → ', '')}
                              </code>
                            ) : (
                              <span>• {f}</span>
                            )}
                          </li>
                        );
                      }) : <li style={{ color: '#9ca3af' }}>无详细信息</li>}
                    </ul>
                  ),
                };
              })}
            />
          </Card>
        </Col>
      )}

      {/* Actions & Config */}
      <Col xs={24} lg={12}>
        <Card
          title="检测操作"
          style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
          extra={
            <Button
              type="primary"
              icon={<SyncOutlined />}
              loading={emailCheckMutation.isPending}
              onClick={() => emailCheckMutation.mutate()}
            >
              立即检测
            </Button>
          }
        >
          <Descriptions column={1} size="small">
            <Descriptions.Item label="上次检测">
              {emailMonitorData?.last_checked_at
                ? new Date(emailMonitorData.last_checked_at).toLocaleString('zh-CN')
                : '从未'}
            </Descriptions.Item>
            <Descriptions.Item label="下次检测">
              {emailMonitorData?.next_check_at
                ? new Date(emailMonitorData.next_check_at).toLocaleString('zh-CN')
                : '-'}
            </Descriptions.Item>
            <Descriptions.Item label="监控状态">
              {emailMonitorData?.enabled
                ? <Tag color="green">已启用</Tag>
                : <Tag>未启用</Tag>}
            </Descriptions.Item>
          </Descriptions>
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card title="检测配置" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
          <Form
            form={emailConfigForm}
            layout="vertical"
            initialValues={{
              dkim_selectors: emailMonitorData?.dkim_selectors || 'google,default,selector1,selector2,k1,s1,dkim',
              mail_server_ips: emailMonitorData?.mail_server_ips || '',
            }}
            onFinish={(values: { dkim_selectors: string; mail_server_ips: string }) => emailConfigMutation.mutate(values)}
          >
            <Form.Item
              label="DKIM 选择器"
              name="dkim_selectors"
              tooltip="逗号分隔，例如: google,default,selector1"
            >
              <Input placeholder="google,default,selector1,selector2,k1,s1,dkim" />
            </Form.Item>
            <Form.Item
              label="邮件服务器 IP"
              name="mail_server_ips"
              tooltip="逗号分隔，用于 PTR 反向解析检查"
            >
              <Input placeholder="如: 1.2.3.4,5.6.7.8" />
            </Form.Item>
            <Form.Item>
              <Button type="primary" htmlType="submit" loading={emailConfigMutation.isPending}>
                保存配置
              </Button>
            </Form.Item>
          </Form>
        </Card>
      </Col>

      {/* History */}
      {emailHistoryData && emailHistoryData.length > 0 && (
        <Col span={24}>
          <Card title="历史检测记录" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
            <Table
              dataSource={emailHistoryData}
              rowKey="id"
              pagination={false}
              size="small"
              columns={[
                {
                  title: '检测时间',
                  dataIndex: 'checked_at',
                  render: (v: string) => new Date(v).toLocaleString('zh-CN'),
                },
                {
                  title: '总分',
                  dataIndex: 'total_score',
                  render: (v: number) => <span style={{ fontWeight: 600 }}>{v}/100</span>,
                },
                {
                  title: '等级',
                  dataIndex: 'grade',
                  render: (v: string) => <Tag color={gradeColor(v)} style={{ borderRadius: 6, fontWeight: 600 }}>{v}</Tag>,
                },
                { title: 'MX', dataIndex: 'mx_score', render: (v: number) => `${v}/30` },
                { title: 'SPF', dataIndex: 'spf_score', render: (v: number) => `${v}/20` },
                { title: 'DKIM', dataIndex: 'dkim_score', render: (v: number) => `${v}/20` },
                { title: 'DMARC', dataIndex: 'dmarc_score', render: (v: number) => `${v}/15` },
                { title: 'PTR', dataIndex: 'ptr_score', render: (v: number) => `${v}/5` },
                { title: 'MTA-STS', dataIndex: 'mta_sts_score', render: (v: number) => `${v}/5` },
                { title: 'TLSRPT', dataIndex: 'tlsrpt_score', render: (v: number) => `${v}/3` },
                { title: 'BIMI', dataIndex: 'bimi_score', render: (v: number) => `${v}/2` },
              ]}
            />
          </Card>
        </Col>
      )}
    </Row>
  );
}

// Service Monitor Tab Component
function ServiceMonitorTab({ domainId, domainName }: { domainId: number; domainName: string }) {
  const queryClient = useQueryClient();
  const [addModalOpen, setAddModalOpen] = useState(false);
  const [addForm] = Form.useForm();
  const [selectedMonitorId, setSelectedMonitorId] = useState<number | null>(null);

  const { data: monitorsData, isLoading: monitorsLoading } = useQuery({
    queryKey: ['service-monitors', domainId],
    queryFn: () => monitorApi.listMonitors(domainId),
    enabled: !!domainId,
  });

  const monitors = monitorsData?.data || [];

  // Auto-select first monitor for chart
  const activeMonitorId = selectedMonitorId ?? (monitors.length > 0 ? monitors[0].id : null);

  const { data: statsData } = useQuery({
    queryKey: ['monitor-stats', activeMonitorId],
    queryFn: () => monitorApi.stats(activeMonitorId!),
    enabled: !!activeMonitorId,
  });

  const { data: checksData } = useQuery({
    queryKey: ['monitor-checks', activeMonitorId],
    queryFn: () => monitorApi.checks(activeMonitorId!),
    enabled: !!activeMonitorId,
  });

  const addMutation = useMutation({
    mutationFn: (data: { monitor_type: string; target: string; label: string; interval_sec?: number; timeout_sec?: number; expected_status?: number }) =>
      monitorApi.addMonitor(domainId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['service-monitors', domainId] });
      setAddModalOpen(false);
      addForm.resetFields();
      message.success('监控已添加');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const deleteMutation = useMutation({
    mutationFn: (monitorId: number) => monitorApi.deleteMonitor(monitorId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['service-monitors', domainId] });
      message.success('监控已删除');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const checkNowMut = useMutation({
    mutationFn: (monitorId: number) => monitorApi.checkNow(monitorId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['service-monitors', domainId] });
      queryClient.invalidateQueries({ queryKey: ['monitor-stats', activeMonitorId] });
      queryClient.invalidateQueries({ queryKey: ['monitor-checks', activeMonitorId] });
      message.success('检测完成');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const stats: ServiceMonitorStats | undefined = statsData?.data;
  const checks: ServiceCheckItem[] = checksData?.data || [];

  const chartData = checks.map((c) => ({
    time: new Date(c.checked_at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }),
    ms: c.response_time_ms,
    success: c.success,
  }));

  const monitorTypeOptions = [
    { value: 'tcp', label: 'TCP' },
    { value: 'udp', label: 'UDP' },
    { value: 'http', label: 'HTTP' },
    { value: 'https', label: 'HTTPS' },
  ];

  return (
    <Row gutter={[16, 16]}>
      {/* Header with Add button */}
      <Col span={24}>
        <Card
          style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
          styles={{ body: { padding: '16px 24px' } }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: 16, fontWeight: 600 }}>服务监控探针</span>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => setAddModalOpen(true)}>
              添加监控
            </Button>
          </div>
        </Card>
      </Col>

      {/* Monitor cards */}
      {monitorsLoading && <Col span={24}><Spin /></Col>}
      {monitors.map((m: ServiceMonitorItem) => (
        <Col xs={24} sm={12} lg={8} key={m.id}>
          <MonitorCard
            monitor={m}
            isSelected={m.id === activeMonitorId}
            onSelect={() => setSelectedMonitorId(m.id)}
            onCheckNow={() => checkNowMut.mutate(m.id)}
            onDelete={() => deleteMutation.mutate(m.id)}
            checkLoading={checkNowMut.isPending}
          />
        </Col>
      ))}

      {monitors.length === 0 && !monitorsLoading && (
        <Col span={24}>
          <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', textAlign: 'center', padding: 40 }}>
            <p style={{ color: '#9ca3af', marginBottom: 16 }}>尚未配置监控探针，请点击"添加监控"开始</p>
          </Card>
        </Col>
      )}

      {/* Stats summary for selected monitor */}
      {stats && activeMonitorId && (
        <Col span={24}>
          <Row gutter={[16, 16]}>
            <Col xs={24} sm={8}>
              <Card title="可用率 (7天)" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
                <div style={{ textAlign: 'center' }}>
                  <Progress
                    type="circle"
                    percent={stats.uptime_percent}
                    format={(p) => `${p}%`}
                    status={stats.uptime_percent < 99 ? 'exception' : 'normal'}
                  />
                </div>
              </Card>
            </Col>
            <Col xs={24} sm={8}>
              <Card title="响应时间" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="平均">{stats.avg_response_ms?.toFixed(0) || '-'} ms</Descriptions.Item>
                  <Descriptions.Item label="最大">{stats.max_response_ms || '-'} ms</Descriptions.Item>
                  <Descriptions.Item label="最小">{stats.min_response_ms || '-'} ms</Descriptions.Item>
                </Descriptions>
              </Card>
            </Col>
            <Col xs={24} sm={8}>
              <Card title="检测统计" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
                <Descriptions column={1} size="small">
                  <Descriptions.Item label="总检测">{stats.total_checks} 次</Descriptions.Item>
                  <Descriptions.Item label="成功">{stats.success_checks} 次</Descriptions.Item>
                  <Descriptions.Item label="失败">{stats.failed_checks} 次</Descriptions.Item>
                </Descriptions>
              </Card>
            </Col>
          </Row>
        </Col>
      )}

      {/* Response time trend chart */}
      {chartData.length > 0 && activeMonitorId && (
        <Col span={24}>
          <Card title="响应时间趋势" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
            <ResponsiveContainer width="100%" height={250}>
              <LineChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="#f3f4f6" />
                <XAxis dataKey="time" fontSize={12} />
                <YAxis unit="ms" fontSize={12} />
                <Tooltip formatter={(value: any) => [`${value} ms`, '响应时间'] as any} />
                <Line type="monotone" dataKey="ms" stroke="#6366f1" dot={false} strokeWidth={2} />
              </LineChart>
            </ResponsiveContainer>
          </Card>
        </Col>
      )}

      {/* Detailed timing table */}
      {checks.length > 0 && (
        <Col span={24}>
          <Card title="详细检测记录" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
            <div style={{ overflowX: 'auto' }}>
              <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
                <thead>
                  <tr style={{ background: '#f8fafc', borderBottom: '2px solid #e5e7eb' }}>
                    <th style={{ padding: '10px 12px', textAlign: 'left', fontWeight: 600, color: '#374151' }}>检测时间</th>
                    <th style={{ padding: '10px 12px', textAlign: 'center', fontWeight: 600, color: '#374151' }}>状态</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#6366f1' }}>DNS</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#10b981' }}>TCP</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#f59e0b' }}>TLS</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#8b5cf6' }}>首字节</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#06b6d4' }}>下载</th>
                    <th style={{ padding: '10px 12px', textAlign: 'right', fontWeight: 600, color: '#1f2937' }}>总计</th>
                    <th style={{ padding: '10px 12px', textAlign: 'center', fontWeight: 600, color: '#374151' }}>状态码</th>
                    <th style={{ padding: '10px 12px', textAlign: 'left', fontWeight: 600, color: '#374151' }}>IP</th>
                  </tr>
                </thead>
                <tbody>
                  {checks.slice(-20).reverse().map((check, idx) => (
                    <tr key={check.id} style={{ borderBottom: '1px solid #f3f4f6', background: idx % 2 === 0 ? '#fff' : '#fafafa' }}>
                      <td style={{ padding: '8px 12px', fontSize: 12, color: '#6b7280' }}>{new Date(check.checked_at).toLocaleString('zh-CN')}</td>
                      <td style={{ padding: '8px 12px', textAlign: 'center' }}>
                        <span style={{ display: 'inline-block', width: 8, height: 8, borderRadius: '50%', background: check.success ? '#10b981' : '#ef4444' }} />
                      </td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', color: '#6366f1' }}>{check.dns_ms || 0}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', color: '#10b981' }}>{check.tcp_ms || 0}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', color: '#f59e0b' }}>{check.tls_ms || 0}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', color: '#8b5cf6' }}>{check.ttfb_ms || 0}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', color: '#06b6d4' }}>{check.download_ms || 0}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'right', fontFamily: 'monospace', fontWeight: 600 }}>{check.total_ms || check.response_time_ms}ms</td>
                      <td style={{ padding: '8px 12px', textAlign: 'center' }}>
                        {check.status_code > 0 ? <span style={{ background: check.status_code < 400 ? '#dcfce7' : '#fef2f2', color: check.status_code < 400 ? '#15803d' : '#991b1b', padding: '2px 8px', borderRadius: 4, fontSize: 12, fontWeight: 600 }}>{check.status_code}</span> : '-'}
                      </td>
                      <td style={{ padding: '8px 12px', fontSize: 12, color: '#6b7280', fontFamily: 'monospace' }}>{check.connected_ip || '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </Card>
        </Col>
      )}
      {/* Add Monitor Modal */}
      <Modal
        title="添加监控探针"
        open={addModalOpen}
        onCancel={() => setAddModalOpen(false)}
        onOk={() => addForm.submit()}
        confirmLoading={addMutation.isPending}
      >
        <Form
          form={addForm}
          layout="vertical"
          initialValues={{ monitor_type: 'http', interval_sec: 300, timeout_sec: 10, expected_status: 200 }}
          onFinish={(values) => addMutation.mutate(values)}
        >
          <Form.Item label="监控类型" name="monitor_type" rules={[{ required: true }]}>
            <Select options={monitorTypeOptions} />
          </Form.Item>
          <Form.Item label="目标地址" name="target" rules={[{ required: true, message: '请输入目标地址' }]}
            tooltip="TCP/UDP: host:port; HTTP/HTTPS: 完整URL"
          >
            <Input placeholder={`如: ${domainName}:443 或 https://${domainName}`} />
          </Form.Item>
          <Form.Item label="标签" name="label">
            <Input placeholder="如: 主站HTTP, SMTP, API" />
          </Form.Item>
          <Form.Item label="检测间隔(秒)" name="interval_sec">
            <Select options={[
              { value: 60, label: '60秒 (1分钟)' },
              { value: 300, label: '300秒 (5分钟)' },
              { value: 600, label: '600秒 (10分钟)' },
              { value: 1800, label: '1800秒 (30分钟)' },
              { value: 3600, label: '3600秒 (1小时)' },
            ]} />
          </Form.Item>
          <Form.Item label="超时(秒)" name="timeout_sec">
            <Select options={[
              { value: 5, label: '5秒' },
              { value: 10, label: '10秒' },
              { value: 15, label: '15秒' },
              { value: 30, label: '30秒' },
            ]} />
          </Form.Item>
          <Form.Item
            noStyle
            shouldUpdate={(prev, cur) => prev.monitor_type !== cur.monitor_type}
          >
            {({ getFieldValue }) =>
              (getFieldValue('monitor_type') === 'http' || getFieldValue('monitor_type') === 'https') ? (
                <Form.Item label="期望状态码" name="expected_status">
                  <Select options={[
                    { value: 200, label: '200 OK' },
                    { value: 201, label: '201 Created' },
                    { value: 301, label: '301 Moved' },
                    { value: 302, label: '302 Found' },
                    { value: 0, label: '不检查状态码' },
                  ]} />
                </Form.Item>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>
    </Row>
  );
}

// Single monitor card component
function MonitorCard({ monitor, isSelected, onSelect, onCheckNow, onDelete, checkLoading }: {
  monitor: ServiceMonitorItem;
  isSelected: boolean;
  onSelect: () => void;
  onCheckNow: () => void;
  onDelete: () => void;
  checkLoading: boolean;
}) {
  const { data: statsData } = useQuery({
    queryKey: ['monitor-stats', monitor.id],
    queryFn: () => monitorApi.stats(monitor.id),
  });

  const stats = statsData?.data;
  const uptimePercent = stats?.uptime_percent ?? 100;

  const typeColors: Record<string, string> = {
    tcp: '#1890ff',
    udp: '#722ed1',
    http: '#52c41a',
    https: '#13c2c2',
  };

  return (
    <Card
      hoverable
      onClick={onSelect}
      style={{
        borderRadius: 12,
        border: isSelected ? '2px solid #6366f1' : '1px solid #e5e7eb',
        cursor: 'pointer',
      }}
      styles={{ body: { padding: 16 } }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 12 }}>
        <div>
          <Tag color={typeColors[monitor.monitor_type] || '#666'} style={{ borderRadius: 6, fontWeight: 600 }}>
            {monitor.monitor_type.toUpperCase()}
          </Tag>
          <span style={{ fontWeight: 500, marginLeft: 4 }}>{monitor.label || monitor.target}</span>
        </div>
        <Progress type="circle" size={40} percent={uptimePercent} format={(p) => `${p}%`}
          status={uptimePercent < 99 ? 'exception' : 'normal'}
        />
      </div>
      <div style={{ fontSize: 12, color: '#6b7280', marginBottom: 8, wordBreak: 'break-all' }}>
        {monitor.target}
      </div>
      {stats && (
        <div style={{ fontSize: 12, color: '#374151', marginBottom: 12 }}>
          平均: {stats.avg_response_ms?.toFixed(0) || 0}ms | 最大: {stats.max_response_ms || 0}ms | 最小: {stats.min_response_ms || 0}ms
        </div>
      )}
      <Space>
        <Button size="small" icon={<SyncOutlined />} loading={checkLoading} onClick={(e) => { e.stopPropagation(); onCheckNow(); }}>
          立即检测
        </Button>
        <Popconfirm title="确认删除此监控?" onConfirm={(e) => { e?.stopPropagation(); onDelete(); }} onCancel={(e) => e?.stopPropagation()}>
          <Button size="small" danger icon={<DeleteOutlined />} onClick={(e) => e.stopPropagation()}>
            删除
          </Button>
        </Popconfirm>
      </Space>
    </Card>
  );
}

export function DomainDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const domainId = Number(id);
  const [searchParams] = useSearchParams();
  const defaultTab = searchParams.get('tab') || 'overview';
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


  // Certificate monitoring queries
  const { data: certMonitors, isLoading: certMonitorsLoading } = useQuery({
    queryKey: ['cert-monitors', domainId],
    queryFn: () => certApi.listMonitors(domainId),
    enabled: !!domainId,
  });

  // Email monitoring queries
  const { data: emailMonitorData, isLoading: emailMonitorLoading } = useQuery({
    queryKey: ['email-monitor', domainId],
    queryFn: () => emailMonitorApi.get(domainId),
    enabled: !!domainId,
  });

  const { data: emailHistoryData } = useQuery({
    queryKey: ['email-monitor-history', domainId],
    queryFn: () => emailMonitorApi.history(domainId),
    enabled: !!domainId,
  });

  const [emailConfigForm] = Form.useForm();

  const emailConfigMutation = useMutation({
    mutationFn: (data: { dkim_selectors: string; mail_server_ips: string }) => emailMonitorApi.configure(domainId, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['email-monitor', domainId] });
      message.success('配置已保存');
    },
    onError: (err: Error) => message.error(err.message),
  });

  const emailCheckMutation = useMutation({
    mutationFn: () => emailMonitorApi.check(domainId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['email-monitor', domainId] });
      queryClient.invalidateQueries({ queryKey: ['email-monitor-history', domainId] });
      message.success('检测完成');
    },
    onError: (err: Error) => message.error(err.message),
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
      children: <ServiceMonitorTab domainId={domainId} domainName={domain.domain_name} />,
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
                expandable={{
                  expandedRowRender: (record) => {
                    const latest = record.latest;
                    if (!latest) return <span style={{ color: '#9ca3af' }}>暂无检测数据</span>;
                    return (
                      <div style={{ padding: '12px 0' }}>
                        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16, marginBottom: 16 }}>
                          <Card size="small" title="连接详情" style={{ borderRadius: 10 }}>
                            <Descriptions size="small" column={1}>
                              <Descriptions.Item label="连接IP">{latest.connected_ip || '-'}</Descriptions.Item>
                              <Descriptions.Item label="SNI主机头">{latest.sni || '-'}</Descriptions.Item>
                              <Descriptions.Item label="DNS解析">{latest.dns_resolve_ms}ms</Descriptions.Item>
                              <Descriptions.Item label="TLS握手">{latest.handshake_ms}ms</Descriptions.Item>
                              <Descriptions.Item label="总耗时">{latest.total_ms}ms</Descriptions.Item>
                              <Descriptions.Item label="TLS版本">{latest.tls_version || '-'}</Descriptions.Item>
                              <Descriptions.Item label="加密套件">{latest.cipher_suite || '-'}</Descriptions.Item>
                            </Descriptions>
                          </Card>
                          <Card size="small" title="证书链" style={{ borderRadius: 10 }}>
                            {latest.chain?.length > 0 ? (
                              <div>
                                {latest.chain.map((cert: any, idx: number) => (
                                  <div key={idx} style={{ padding: '8px 0', borderBottom: idx < latest.chain.length - 1 ? '1px solid #f3f4f6' : 'none' }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                                      <span style={{ background: cert.is_ca ? '#dbeafe' : '#dcfce7', color: cert.is_ca ? '#1d4ed8' : '#15803d', fontSize: 11, padding: '2px 6px', borderRadius: 4, fontWeight: 600 }}>
                                        {cert.is_ca ? 'CA' : '叶子'}
                                      </span>
                                      <span style={{ fontWeight: 500 }}>{cert.subject || '(empty)'}</span>
                                    </div>
                                    <div style={{ fontSize: 12, color: '#6b7280', marginTop: 4, paddingLeft: 48 }}>
                                      颁发者: {cert.issuer} | 有效期: {cert.valid_from ? new Date(cert.valid_from).toLocaleDateString('zh-CN') : '-'} ~ {cert.valid_to ? new Date(cert.valid_to).toLocaleDateString('zh-CN') : '-'}
                                    </div>
                                  </div>
                                ))}
                              </div>
                            ) : <span style={{ color: '#9ca3af' }}>无证书链数据</span>}
                          </Card>
                        </div>
                        {latest.sans?.length > 0 && (
                          <Card size="small" title="SAN (Subject Alternative Names)" style={{ borderRadius: 10 }}>
                            <Space wrap>
                              {latest.sans.map((san: string, idx: number) => <Tag key={idx} style={{ borderRadius: 6 }}>{san}</Tag>)}
                            </Space>
                          </Card>
                        )}
                      </div>
                    );
                  },
                }}
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
                    title: '检测时间',
                    key: 'check_times',
                    width: 180,
                    render: (_: any, record: CertMonitor) => (
                      <div style={{ fontSize: 12 }}>
                        <div><span style={{ color: '#6b7280' }}>上次: </span>{record.last_checked_at ? new Date(record.last_checked_at).toLocaleString('zh-CN') : '从未'}</div>
                        <div><span style={{ color: '#6b7280' }}>下次: </span>{record.next_check_at ? new Date(record.next_check_at).toLocaleString('zh-CN') : '-'}</div>
                      </div>
                    ),
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
    {
      key: 'email-security',
      label: '邮件安全',
      children: <EmailSecurityTab
        domainId={domainId}
        emailMonitorData={emailMonitorData?.data}
        emailMonitorLoading={emailMonitorLoading}
        emailHistoryData={emailHistoryData?.data}
        emailConfigForm={emailConfigForm}
        emailConfigMutation={emailConfigMutation}
        emailCheckMutation={emailCheckMutation}
      />,
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

      <Tabs items={tabItems} defaultActiveKey={defaultTab} />
    </div>
  );
}
