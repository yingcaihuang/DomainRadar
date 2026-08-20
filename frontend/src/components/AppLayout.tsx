import { useEffect, useRef } from 'react';
import { Outlet, useNavigate, useLocation } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { alertApi } from '../services';
import { Layout, Menu, Avatar, Typography, Divider } from 'antd';
import {
  DashboardOutlined,
  GlobalOutlined,
  CalendarOutlined,
  AlertOutlined,
  SettingOutlined,
  UserOutlined,
  LogoutOutlined,
  AuditOutlined,
  BellOutlined,
  CloudServerOutlined,
  TagsOutlined,
  SafetyOutlined,
  TeamOutlined,
} from '@ant-design/icons';
import { useAuthStore } from '../store';

const { Sider, Content } = Layout;
const { Text } = Typography;

// Play a subtle notification bell sound using Web Audio API
function playNotificationSound() {
  try {
    const ctx = new (window.AudioContext || (window as any).webkitAudioContext)();
    const oscillator = ctx.createOscillator();
    const gain = ctx.createGain();
    oscillator.connect(gain);
    gain.connect(ctx.destination);
    oscillator.type = 'sine';
    oscillator.frequency.setValueAtTime(800, ctx.currentTime);
    oscillator.frequency.setValueAtTime(600, ctx.currentTime + 0.1);
    oscillator.frequency.setValueAtTime(800, ctx.currentTime + 0.2);
    gain.gain.setValueAtTime(0.3, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.01, ctx.currentTime + 0.4);
    oscillator.start(ctx.currentTime);
    oscillator.stop(ctx.currentTime + 0.4);
  } catch { /* ignore audio errors */ }
}

export function AppLayout() {
  // Alert count for bell notification
  const { data: alertData } = useQuery({
    queryKey: ['alert-count'],
    queryFn: () => alertApi.list({ page: '1', page_size: '1' }),
    refetchInterval: 30000, // refresh every 30s
  });
  const alertCount = alertData?.total || 0;
  const prevAlertCount = useRef(0);

  // Play notification sound when new alerts appear
  useEffect(() => {
    if (alertCount > prevAlertCount.current && prevAlertCount.current !== 0) {
      playNotificationSound();
    }
    prevAlertCount.current = alertCount;
  }, [alertCount]);
  const navigate = useNavigate();
  const location = useLocation();
  const { user, logout, hasPermission } = useAuthStore();

  const mainMenuItems = [
    { key: '/dashboard', icon: <DashboardOutlined />, label: '仪表盘' },
    { key: '/domains', icon: <GlobalOutlined />, label: '域名管理' },
    { key: '/calendar', icon: <CalendarOutlined />, label: '到期日历' },
    { key: '/tags-groups', icon: <TagsOutlined />, label: '标签与分组' },
    { key: '/alerts', icon: <AlertOutlined />, label: '告警中心' },
  ];

  const settingsMenuItems = hasPermission('configure_integrations') ? [
    { key: '/settings/rules', icon: <SettingOutlined />, label: '到期规则' },
    { key: '/settings/registrars', icon: <CloudServerOutlined />, label: '注册商' },
    { key: '/settings/notifications', icon: <BellOutlined />, label: '通知渠道' },
    { key: '/settings/sso', icon: <SafetyOutlined />, label: 'SSO 配置' },
    { key: '/settings/users', icon: <UserOutlined />, label: '用户管理' },
    { key: '/settings/group-mappings', icon: <TeamOutlined />, label: '组映射' },
    { key: '/settings/audit', icon: <AuditOutlined />, label: '审计日志' },
  ] : [];

  const allItems = [...mainMenuItems, ...settingsMenuItems];
  const selectedKey = allItems.find(item => location.pathname.startsWith(item.key))?.key || '/dashboard';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider
        width={260}
        style={{
          background: '#ffffff',
          borderRight: '1px solid #e5e7eb',
          position: 'fixed',
          left: 0,
          top: 0,
          bottom: 0,
          zIndex: 100,
          display: 'flex',
          flexDirection: 'column',
        }}
      >
        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
          {/* Logo */}
          <div style={{ padding: '24px 24px', display: 'flex', alignItems: 'center', gap: 12 }}>
            <div style={{
              width: 32,
              height: 32,
              borderRadius: 8,
              background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
            }}>
              <GlobalOutlined style={{ color: '#fff', fontSize: 16 }} />
            </div>
            <Text strong style={{ fontSize: 20, color: '#1f2937', letterSpacing: '-0.3px' }}>DomainRadar</Text>
          </div>

          {/* Main Navigation */}
          <Menu
            mode="inline"
            selectedKeys={[selectedKey]}
            items={mainMenuItems}
            onClick={({ key }) => navigate(key)}
            style={{
              border: 'none',
              padding: '0 8px',
            }}
          />

          {/* Settings Section */}
          {settingsMenuItems.length > 0 && (
            <>
              <Divider style={{ margin: '12px 24px', borderColor: '#f3f4f6' }} />
              <div style={{ padding: '8px 24px 10px', fontSize: 12, fontWeight: 600, color: '#9ca3af', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
                <SettingOutlined style={{ marginRight: 6 }} />
                管理后台
              </div>
              <Menu
                mode="inline"
                selectedKeys={[selectedKey]}
                items={settingsMenuItems}
                onClick={({ key }) => navigate(key)}
                style={{
                  border: 'none',
                  padding: '0 8px',
                }}
              />
            </>
          )}

          {/* Spacer */}
          <div style={{ flex: 1 }} />

          {/* User Info */}
          <div style={{
            padding: '16px 20px',
            borderTop: '1px solid #f3f4f6',
            display: 'flex',
            alignItems: 'center',
            gap: 10,
          }}>
            <Avatar size={36} style={{ background: '#6366f1', flexShrink: 0 }} icon={<UserOutlined />} />
            <div style={{ flex: 1, minWidth: 0 }}>
              <div style={{ fontSize: 14, fontWeight: 600, color: '#1f2937', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {user?.display_name || user?.email}
              </div>
              <div style={{ fontSize: 12, color: '#9ca3af', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {user?.email}
              </div>
            </div>
            <LogoutOutlined
              style={{ fontSize: 16, color: '#9ca3af', cursor: 'pointer', flexShrink: 0 }}
              onClick={logout}
            />
          </div>
        </div>
      </Sider>
      <Layout style={{ marginLeft: 260, background: '#f8fafc' }}>
        {/* Top bar with notification bell */}
        <div style={{ height: 60, background: '#fff', borderBottom: '1px solid #e5e7eb', display: 'flex', alignItems: 'center', justifyContent: 'flex-end', padding: '0 24px', position: 'sticky', top: 0, zIndex: 50 }}>
          <div style={{ position: 'relative', cursor: 'pointer' }} onClick={() => navigate('/alerts')}>
            <BellOutlined style={{ fontSize: 22, color: alertCount > 0 ? '#6366f1' : '#9ca3af' }} />
            {alertCount > 0 && (
              <>
                <span style={{
                  position: 'absolute', top: -4, right: -6,
                  background: '#ef4444', color: '#fff', fontSize: 10, fontWeight: 700,
                  borderRadius: 10, minWidth: 16, height: 16,
                  display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '0 4px',
                }}>
                  {alertCount > 99 ? '99+' : alertCount}
                </span>
                <style>{`
                  @keyframes bellShake {
                    0%, 100% { transform: rotate(0); }
                    15% { transform: rotate(12deg); }
                    30% { transform: rotate(-12deg); }
                    45% { transform: rotate(8deg); }
                    60% { transform: rotate(-8deg); }
                    75% { transform: rotate(4deg); }
                  }
                `}</style>
                <span style={{
                  position: 'absolute', inset: -4,
                  animation: 'bellShake 2s ease-in-out infinite',
                  pointerEvents: 'none',
                }} />
              </>
            )}
          </div>
        </div>
        <Content style={{ margin: 32, minHeight: 'calc(100vh - 104px)' }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
