import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, Col, Row, Spin } from 'antd';
import { AlertOutlined, GlobalOutlined, WarningOutlined, HeartOutlined, SafetyCertificateOutlined, MailOutlined } from '@ant-design/icons';
import { PieChart, Pie, Cell, ResponsiveContainer, Tooltip, Legend } from 'recharts';
import { dashboardApi } from '../services';
import { StatsCard } from '../components/StatsCard';

const COLORS = ['#6366f1', '#10b981', '#f59e0b', '#ef4444', '#8b5cf6', '#06b6d4', '#ec4899'];

export function DashboardPage() {
  const [expandedHealth, setExpandedHealth] = useState(false);

  const { data, isLoading } = useQuery({
    queryKey: ['dashboard'],
    queryFn: dashboardApi.get,
  });

  const { data: healthData } = useQuery({
    queryKey: ['dashboard-health'],
    queryFn: dashboardApi.healthScores,
  });

  if (isLoading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '50vh' }}>
        <Spin size="large" />
      </div>
    );
  }

  const chartData = data?.by_registrar?.map(r => ({
    name: r.registrar || 'Unknown',
    value: r.domain_count,
  })) || [];

  const healthChartData = healthData?.data
    ?.sort((a: any, b: any) => a.health_score - b.health_score) // expired first, then by score ascending
    .map((d: any) => ({
    name: d.domain_name,
    score: d.health_score,
  })) || [];

  return (
    <div>
      {/* Page Header */}
      <div style={{ marginBottom: 32 }}>
        <h1 style={{
          fontSize: 28,
          fontWeight: 700,
          margin: 0,
          background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
          WebkitBackgroundClip: 'text',
          WebkitTextFillColor: 'transparent',
          backgroundClip: 'text',
        }}>
          Dashboard
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>
          域名资产监控与到期管理概览
        </p>
      </div>

      {/* Stats Cards */}
      <Row gutter={[16, 16]} style={{ marginBottom: 32 }}>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<GlobalOutlined />}
            label="域名总数"
            value={data?.total_domains || 0}
            color="indigo"
          />
        </Col>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<WarningOutlined />}
            label="30天内到期"
            value={data?.expiring_within_30_days || 0}
            color="amber"
          />
        </Col>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<AlertOutlined />}
            label="活跃告警"
            value={data?.active_alerts || 0}
            color="red"
          />
        </Col>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<HeartOutlined />}
            label="健康评分"
            value={data?.overall_health_score || 0}
            color="emerald"
            suffix="/ 100"
          />
        </Col>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<SafetyCertificateOutlined />}
            label="证书监控"
            value={`${data?.cert_monitors || 0}`}
            color="sky"
            suffix={data?.cert_expiring ? `(${data.cert_expiring}即将到期)` : undefined}
          />
        </Col>
        <Col xs={24} sm={12} lg={4}>
          <StatsCard
            icon={<MailOutlined />}
            label="邮件监控"
            value={`${data?.email_monitors || 0}`}
            color="violet"
            suffix={data?.email_avg_score ? `均分${data.email_avg_score}` : undefined}
          />
        </Col>
      </Row>

      {/* Charts Section */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            title="注册商分布"
            style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
            styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}
          >
            {chartData.length > 0 ? (
              <div style={{ position: 'relative' }}>
                <ResponsiveContainer width="100%" height={300}>
                  <PieChart>
                    <Pie data={chartData} dataKey="value" nameKey="name" cx="50%" cy="50%" innerRadius={70} outerRadius={110} paddingAngle={2} stroke="none">
                      {chartData.map((_, index) => (
                        <Cell key={index} fill={COLORS[index % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip contentStyle={{ borderRadius: 8, border: '1px solid #e5e7eb', boxShadow: '0 4px 12px rgba(0,0,0,0.08)' }} />
                    <Legend iconType="circle" wrapperStyle={{ fontSize: 14, paddingTop: 8 }} />
                  </PieChart>
                </ResponsiveContainer>
                <div style={{ position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%, -60%)', textAlign: 'center', pointerEvents: 'none' }}>
                  <div style={{ fontSize: 28, fontWeight: 700, color: '#1f2937' }}>{data?.total_domains || 0}</div>
                  <div style={{ fontSize: 12, color: '#9ca3af' }}>总域名</div>
                </div>
              </div>
            ) : (
              <div style={{ textAlign: 'center', padding: 40, color: '#9ca3af' }}>暂无域名数据</div>
            )}
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card
            title="域名到期概览"
            style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}
            styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}
          >
            {healthChartData.length > 0 ? (() => {
              const displayData = expandedHealth ? healthChartData : healthChartData.slice(0, 7);
              return (
                <div style={{ padding: '8px 0' }}>
                  {displayData.map((item, idx) => {
                    const score = item.score;
                    const color = score === 0 ? '#ef4444' : score <= 50 ? '#f59e0b' : score <= 75 ? '#6366f1' : '#10b981';
                    const bg = score === 0 ? '#fef2f2' : score <= 50 ? '#fffbeb' : score <= 75 ? '#eef2ff' : '#ecfdf5';
                    // Calculate days from expiration in healthData
                    const domainInfo = healthData?.data?.find((d: any) => d.domain_name === item.name);
                    let daysText = '';
                    if (domainInfo && (domainInfo as any).expiration_date) {
                      const days = Math.ceil((new Date((domainInfo as any).expiration_date).getTime() - Date.now()) / (1000*60*60*24));
                      daysText = days < 0 ? `过期${Math.abs(days)}天` : `${days}天`;
                    }
                    const label = score === 0 ? '已过期' : '正常';
                    return (
                      <div key={idx} style={{ display: 'flex', alignItems: 'center', padding: '8px 0', borderBottom: idx < displayData.length - 1 ? '1px solid #f3f4f6' : 'none' }}>
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <span style={{ fontSize: 14, fontWeight: 500, color: '#1f2937' }}>{item.name}</span>
                        </div>
                        <div style={{ width: 140, marginRight: 12, position: 'relative' }}>
                          <div style={{ height: 20, borderRadius: 4, background: '#f3f4f6', overflow: 'hidden', position: 'relative' }}>
                            <div style={{ height: '100%', width: `${score}%`, borderRadius: 4, background: `linear-gradient(90deg, ${color}, ${color}cc)` }} />
                            {daysText && <span style={{ position: 'absolute', top: 2, left: 6, fontSize: 11, fontWeight: 600, color: score > 50 ? '#fff' : '#1f2937' }}>{daysText}</span>}
                          </div>
                        </div>
                        <div style={{ background: bg, color, fontSize: 12, fontWeight: 600, padding: '3px 10px', borderRadius: 6, minWidth: 60, textAlign: 'center' }}>
                          {label} {score > 0 ? score : ''}
                        </div>
                      </div>
                    );
                  })}
                  {healthChartData.length > 7 && (
                    <div style={{ textAlign: 'center', paddingTop: 8 }}>
                      <a onClick={() => setExpandedHealth(!expandedHealth)} style={{ fontSize: 13, color: '#6366f1', cursor: 'pointer' }}>
                        {expandedHealth ? '收起' : `展开全部 (${healthChartData.length} 个域名)`}
                      </a>
                    </div>
                  )}
                </div>
              );
            })() : (
              <div style={{ textAlign: 'center', padding: 40, color: '#9ca3af' }}>暂无域名数据</div>
            )}
          </Card>
        </Col>
      </Row>

      {/* Row 3: Service Monitor + Certificate Summary */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <Card title="服务监控可用率" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}>
            {data?.service_monitors && data.service_monitors.length > 0 ? (
              <div>
                {data.service_monitors.map((item: any, idx: number) => {
                  const pct = item.uptime_percent;
                  const color = pct >= 99.9 ? '#10b981' : pct >= 95 ? '#f59e0b' : '#ef4444';
                  return (
                    <div key={idx} style={{ display: 'flex', alignItems: 'center', padding: '8px 0', borderBottom: idx < data.service_monitors.length - 1 ? '1px solid #f3f4f6' : 'none' }}>
                      <span style={{ flex: 1, fontSize: 14, fontWeight: 500 }}>{item.domain_name}</span>
                      <div style={{ width: 120, marginRight: 12 }}>
                        <div style={{ height: 6, borderRadius: 3, background: '#f3f4f6', overflow: 'hidden' }}>
                          <div style={{ height: '100%', width: `${pct}%`, borderRadius: 3, background: color }} />
                        </div>
                      </div>
                      <span style={{ fontSize: 13, fontWeight: 600, color, minWidth: 50, textAlign: 'right' }}>{pct.toFixed(1)}%</span>
                    </div>
                  );
                })}
              </div>
            ) : <div style={{ textAlign: 'center', padding: 30, color: '#9ca3af' }}>暂无服务监控数据</div>}
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="证书到期概览" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}>
            {data?.certificate_summary && data.certificate_summary.length > 0 ? (
              <div>
                {data.certificate_summary.sort((a: any, b: any) => a.days_remaining - b.days_remaining).map((item: any, idx: number) => {
                  const days = item.days_remaining;
                  const color = days < 0 ? '#ef4444' : days <= 30 ? '#f59e0b' : days <= 90 ? '#6366f1' : '#10b981';
                  const label = days < 0 ? `已过期${Math.abs(days)}天` : `${days}天`;
                  return (
                    <div key={idx} style={{ display: 'flex', alignItems: 'center', padding: '8px 0', borderBottom: idx < data.certificate_summary.length - 1 ? '1px solid #f3f4f6' : 'none' }}>
                      <span style={{ flex: 1, fontSize: 14, fontWeight: 500 }}>{item.domain_name}</span>
                      <span style={{ fontSize: 12, color: '#9ca3af', marginRight: 12 }}>{item.endpoint}</span>
                      <span style={{ background: color + '15', color, padding: '2px 10px', borderRadius: 6, fontSize: 12, fontWeight: 600 }}>{label}</span>
                    </div>
                  );
                })}
              </div>
            ) : <div style={{ textAlign: 'center', padding: 30, color: '#9ca3af' }}>暂无证书监控数据</div>}
          </Card>
        </Col>
      </Row>

      {/* Row 4: Email Grades + WHOIS Status */}
      <Row gutter={[16, 16]} style={{ marginTop: 16 }}>
        <Col xs={24} lg={12}>
          <Card title="邮件安全评分" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}>
            {data?.email_grades?.items && data.email_grades.items.length > 0 ? (
              <div>
                {data.email_grades.items.map((item: any, idx: number) => {
                  const score = item.total_score;
                  const color = score >= 90 ? '#10b981' : score >= 70 ? '#f59e0b' : score >= 50 ? '#f97316' : '#ef4444';
                  return (
                    <div key={idx} style={{ display: 'flex', alignItems: 'center', padding: '8px 0', borderBottom: idx < data.email_grades.items.length - 1 ? '1px solid #f3f4f6' : 'none' }}>
                      <span style={{ flex: 1, fontSize: 14, fontWeight: 500 }}>{item.domain_name}</span>
                      <div style={{ width: 100, marginRight: 12 }}>
                        <div style={{ height: 6, borderRadius: 3, background: '#f3f4f6', overflow: 'hidden' }}>
                          <div style={{ height: '100%', width: `${score}%`, borderRadius: 3, background: color }} />
                        </div>
                      </div>
                      <span style={{ background: color + '15', color, padding: '2px 8px', borderRadius: 6, fontSize: 12, fontWeight: 600, minWidth: 50, textAlign: 'center' }}>
                        {item.grade} {score}
                      </span>
                    </div>
                  );
                })}
              </div>
            ) : <div style={{ textAlign: 'center', padding: 30, color: '#9ca3af' }}>暂无邮件检测数据</div>}
          </Card>
        </Col>
        <Col xs={24} lg={12}>
          <Card title="WHOIS 检测状态" style={{ borderRadius: 12, border: '1px solid #e5e7eb' }} styles={{ header: { borderBottom: '1px solid #f3f4f6', fontWeight: 600 } }}>
            {data?.whois_status ? (() => {
              const ws = data.whois_status;
              const total = ws.checked + ws.unchecked;
              const pct = total > 0 ? Math.round(ws.checked / total * 100) : 0;
              return (
                <div style={{ padding: '8px 0' }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 16 }}>
                    <div style={{ textAlign: 'center' }}>
                      <div style={{ fontSize: 28, fontWeight: 700, color: '#6366f1' }}>{ws.checked}</div>
                      <div style={{ fontSize: 12, color: '#6b7280' }}>已检测</div>
                    </div>
                    <div style={{ textAlign: 'center' }}>
                      <div style={{ fontSize: 28, fontWeight: 700, color: '#9ca3af' }}>{ws.unchecked}</div>
                      <div style={{ fontSize: 12, color: '#6b7280' }}>未检测</div>
                    </div>
                    <div style={{ textAlign: 'center' }}>
                      <div style={{ fontSize: 28, fontWeight: 700, color: '#10b981' }}>{pct}%</div>
                      <div style={{ fontSize: 12, color: '#6b7280' }}>覆盖率</div>
                    </div>
                  </div>
                  <div style={{ height: 8, borderRadius: 4, background: '#f3f4f6', overflow: 'hidden', marginBottom: 16 }}>
                    <div style={{ height: '100%', width: `${pct}%`, borderRadius: 4, background: 'linear-gradient(90deg, #6366f1, #8b5cf6)' }} />
                  </div>
                  <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8, fontSize: 13 }}>
                    <div><span style={{ color: '#6b7280' }}>上次检测：</span>{ws.last_checked || '-'}</div>
                    <div><span style={{ color: '#6b7280' }}>下次检测：</span>{ws.next_check || '-'}</div>
                  </div>
                </div>
              );
            })() : <div style={{ textAlign: 'center', padding: 30, color: '#9ca3af' }}>暂无 WHOIS 数据</div>}
          </Card>
        </Col>
      </Row>
    </div>
  );
}
