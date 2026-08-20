import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, Form, Input, Button, Typography, Divider, App } from 'antd';
import { UserOutlined, LockOutlined, GlobalOutlined } from '@ant-design/icons';
import { useAuthStore } from '../store';

const { Title, Text } = Typography;

interface AuthMode {
  sso_enabled: boolean;
  local_enabled: boolean;
}

interface LoginResponse {
  message: string;
  user: {
    id: number;
    email: string;
    display_name: string;
    roles: string[];
    must_change_password: boolean;
  };
  token: string;
}

export function LoginPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { user, fetchUser } = useAuthStore();
  const [authMode, setAuthMode] = useState<AuthMode | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // If already authenticated, redirect to dashboard
    if (user) {
      navigate('/dashboard', { replace: true });
      return;
    }

    // Fetch auth mode
    fetch('/api/v1/auth/mode')
      .then(r => r.json())
      .then(data => setAuthMode(data))
      .catch(() => setAuthMode({ sso_enabled: false, local_enabled: true }));
  }, [user, navigate]);

  const handleLocalLogin = async (values: { username: string; password: string }) => {
    setLoading(true);
    try {
      const res = await fetch('/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: { message: '登录失败' } }));
        message.error(err.error?.message || '用户名或密码错误');
        setLoading(false);
        return;
      }

      const data: LoginResponse = await res.json();

      // If must change password, redirect to change-password page
      if (data.user.must_change_password) {
        await fetchUser();
        navigate('/change-password', { replace: true });
      } else {
        await fetchUser();
        navigate('/dashboard', { replace: true });
      }
    } catch {
      message.error('登录请求失败，请检查网络');
    }
    setLoading(false);
  };

  const handleSSOLogin = () => {
    window.location.href = '/api/v1/auth/login-sso';
  };

  return (
    <div style={{
      minHeight: '100vh',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      background: 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
      padding: 24,
    }}>
      <Card
        style={{
          width: 400,
          borderRadius: 16,
          boxShadow: '0 20px 60px rgba(0,0,0,0.15)',
        }}
        styles={{ body: { padding: 40 } }}
      >
        {/* Logo and title */}
        <div style={{ textAlign: 'center', marginBottom: 32 }}>
          <div style={{
            width: 48,
            height: 48,
            borderRadius: 12,
            background: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
            display: 'inline-flex',
            alignItems: 'center',
            justifyContent: 'center',
            marginBottom: 16,
          }}>
            <GlobalOutlined style={{ color: '#fff', fontSize: 24 }} />
          </div>
          <Title level={3} style={{ marginBottom: 4 }}>DomainRadar</Title>
          <Text type="secondary">域名监控管理平台</Text>
        </div>

        {/* Local login form */}
        <Form
          name="login"
          onFinish={handleLocalLogin}
          size="large"
          autoComplete="off"
        >
          <Form.Item
            name="username"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input
              prefix={<UserOutlined style={{ color: '#9ca3af' }} />}
              placeholder="用户名 / 邮箱"
            />
          </Form.Item>

          <Form.Item
            name="password"
            rules={[{ required: true, message: '请输入密码' }]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: '#9ca3af' }} />}
              placeholder="密码"
            />
          </Form.Item>

          <Form.Item style={{ marginBottom: 12 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              style={{ height: 44, borderRadius: 8 }}
            >
              登录
            </Button>
          </Form.Item>
        </Form>

        {/* SSO login button */}
        {authMode?.sso_enabled && (
          <>
            <Divider style={{ margin: '16px 0' }}>
              <Text type="secondary" style={{ fontSize: 12 }}>或</Text>
            </Divider>
            <Button
              block
              size="large"
              onClick={handleSSOLogin}
              style={{ height: 44, borderRadius: 8 }}
            >
              使用 SSO 登录
            </Button>
          </>
        )}
      </Card>
    </div>
  );
}
