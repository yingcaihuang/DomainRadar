import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, Form, Input, Button, Typography, App, Alert } from 'antd';
import { LockOutlined } from '@ant-design/icons';
import { useAuthStore } from '../store';

const { Title, Paragraph } = Typography;

export function ChangePasswordPage() {
  const navigate = useNavigate();
  const { message } = App.useApp();
  const { user, fetchUser } = useAuthStore();
  const [loading, setLoading] = useState(false);

  const isMustChange = user?.must_change_password;

  const handleSubmit = async (values: { old_password: string; new_password: string }) => {
    setLoading(true);
    try {
      const res = await fetch('/api/v1/auth/change-password', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          old_password: values.old_password,
          new_password: values.new_password,
        }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: { message: '修改密码失败' } }));
        message.error(err.error?.message || '修改密码失败');
        setLoading(false);
        return;
      }

      message.success('密码修改成功');
      await fetchUser();
      navigate('/dashboard', { replace: true });
    } catch {
      message.error('请求失败，请检查网络');
    }
    setLoading(false);
  };

  return (
    <div style={{
      maxWidth: 480,
      margin: isMustChange ? '80px auto' : '0 auto',
      padding: isMustChange ? 24 : 0,
    }}>
      <Title level={4} style={{ marginBottom: 4 }}>
        <LockOutlined style={{ marginRight: 8 }} />
        修改密码
      </Title>
      <Paragraph type="secondary" style={{ marginBottom: 24 }}>
        {isMustChange ? '首次登录需要修改默认密码。' : '更新您的登录密码。'}
      </Paragraph>

      {isMustChange && (
        <Alert
          type="warning"
          message="安全提示"
          description="您正在使用默认密码，请立即修改以确保账户安全。"
          showIcon
          style={{ marginBottom: 24 }}
        />
      )}

      <Card>
        <Form
          layout="vertical"
          onFinish={handleSubmit}
          size="large"
        >
          <Form.Item
            name="old_password"
            label="当前密码"
            rules={[{ required: true, message: '请输入当前密码' }]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: '#9ca3af' }} />}
              placeholder="请输入当前密码"
            />
          </Form.Item>

          <Form.Item
            name="new_password"
            label="新密码"
            rules={[
              { required: true, message: '请输入新密码' },
              { min: 6, message: '密码至少6个字符' },
            ]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: '#9ca3af' }} />}
              placeholder="请输入新密码（至少6位）"
            />
          </Form.Item>

          <Form.Item
            name="confirm_password"
            label="确认新密码"
            dependencies={['new_password']}
            rules={[
              { required: true, message: '请确认新密码' },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('new_password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error('两次输入的密码不一致'));
                },
              }),
            ]}
          >
            <Input.Password
              prefix={<LockOutlined style={{ color: '#9ca3af' }} />}
              placeholder="请再次输入新密码"
            />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0, marginTop: 24 }}>
            <Button
              type="primary"
              htmlType="submit"
              loading={loading}
              block
              style={{ height: 44, borderRadius: 8 }}
            >
              确认修改
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
