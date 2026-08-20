import { useState, useEffect } from 'react';
import { Card, Form, Input, Button, Switch, Typography, Space, App, Spin, Select, Divider } from 'antd';
import { SafetyOutlined, SearchOutlined } from '@ant-design/icons';
import { ssoConfigApi } from '../services';

const { Title, Paragraph, Text } = Typography;

export function SsoConfigPage() {
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [discovering, setDiscovering] = useState(false);
  const [config, setConfig] = useState<any>(null);

  useEffect(() => { loadConfig(); }, []);

  const loadConfig = async () => {
    setLoading(true);
    try {
      const res = await ssoConfigApi.get();
      setConfig(res.data);
      form.setFieldsValue({
        enabled: res.data.enabled,
        issuer_url: res.data.issuer_url || '',
        discovery_url: res.data.discovery_url || '',
        client_id: res.data.client_id || '',
        client_secret: '',
        authorization_endpoint: res.data.authorization_endpoint || '',
        token_endpoint: res.data.token_endpoint || '',
        userinfo_endpoint: res.data.userinfo_endpoint || '',
        jwks_uri: res.data.jwks_uri || '',
        end_session_endpoint: res.data.end_session_endpoint || '',
        redirect_url: res.data.redirect_url || `${window.location.origin}/api/v1/auth/callback`,
        scopes: res.data.scopes || 'openid profile email groups',
        groups_claim: res.data.groups_claim || 'groups',
        groups_source: res.data.groups_source || 'userinfo',
        show_on_login_page: res.data.show_on_login_page !== false,
        cookie_secure: res.data.cookie_secure || false,
      });
    } catch { message.error('加载SSO配置失败'); }
    setLoading(false);
  };

  const handleDiscover = async () => {
    const issuerUrl = form.getFieldValue('issuer_url');
    if (!issuerUrl) { message.warning('请先填写 Issuer URL'); return; }
    setDiscovering(true);
    try {
      const res = await ssoConfigApi.discover({ issuer_url: issuerUrl });
      if (res.success && res.data) {
        form.setFieldsValue({
          discovery_url: res.data.discovery_url,
          authorization_endpoint: res.data.authorization_endpoint,
          token_endpoint: res.data.token_endpoint,
          userinfo_endpoint: res.data.userinfo_endpoint,
          jwks_uri: res.data.jwks_uri,
          end_session_endpoint: res.data.end_session_endpoint,
        });
        message.success('端点已自动发现并填充');
      } else {
        message.error(res.message || '发现失败');
      }
    } catch { message.error('自动发现请求失败'); }
    setDiscovering(false);
  };

  const handleAutoFillRedirect = () => {
    form.setFieldsValue({ redirect_url: `${window.location.origin}/api/v1/auth/callback` });
    message.success('Redirect URI 已自动填写');
  };

  const handleSave = async (values: any) => {
    setSaving(true);
    try {
      await ssoConfigApi.update(values);
      message.success('SSO 配置已保存');
      await loadConfig();
    } catch { message.error('保存失败'); }
    setSaving(false);
  };

  if (loading) return <div style={{ display: 'flex', justifyContent: 'center', padding: 100 }}><Spin size="large" /></div>;

  return (
    <div style={{ maxWidth: 960 }}>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          SSO 单点登录配置
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4 }}>配置 OIDC 提供商以启用统一身份认证</p>
      </div>

      <Form form={form} layout="vertical" onFinish={handleSave}>
        {/* Section 1: 基础信息 */}
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 20 }}>
          <Title level={5} style={{ marginBottom: 4 }}>
            <SafetyOutlined style={{ marginRight: 8 }} />
            基础信息
          </Title>
          <Paragraph type="secondary" style={{ marginBottom: 20 }}>配置 IdP 的基本连接信息和客户端凭证</Paragraph>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item name="issuer_url" label="Issuer" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/application/o/app/" />
            </Form.Item>
            <Form.Item label="Discovery URL" style={{ marginBottom: 16 }}>
              <Space.Compact style={{ width: '100%' }}>
                <Form.Item name="discovery_url" noStyle>
                  <Input placeholder="自动发现后填充" readOnly style={{ flex: 1 }} />
                </Form.Item>
                <Button icon={<SearchOutlined />} loading={discovering} onClick={handleDiscover}>自动发现</Button>
              </Space.Compact>
            </Form.Item>
            <Form.Item name="client_id" label="Client ID" style={{ marginBottom: 16 }}>
              <Input placeholder="your-client-id" />
            </Form.Item>
            <Form.Item name="client_secret" label={<span>Client Secret {config?.has_secret && <Text type="success" style={{ fontSize: 12 }}>(已设置)</Text>}</span>} style={{ marginBottom: 16 }}>
              <Input.Password placeholder={config?.has_secret ? '留空不修改' : '请输入 Client Secret'} />
            </Form.Item>
          </div>
        </Card>

        {/* Section 2: 端点配置 */}
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 20 }}>
          <Title level={5} style={{ marginBottom: 4 }}>端点配置</Title>
          <Paragraph type="secondary" style={{ marginBottom: 20 }}>OAuth2 / OIDC 各端点地址，可通过 Discovery URL 自动填充</Paragraph>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item name="authorization_endpoint" label="Authorization Endpoint" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/authorize/" />
            </Form.Item>
            <Form.Item name="token_endpoint" label="Token Endpoint" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/token/" />
            </Form.Item>
            <Form.Item name="userinfo_endpoint" label="Userinfo Endpoint" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/userinfo/" />
            </Form.Item>
            <Form.Item name="jwks_uri" label="JWKS URI" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/jwks/" />
            </Form.Item>
            <Form.Item name="end_session_endpoint" label="End Session Endpoint" extra="自动发现可回填" style={{ marginBottom: 16 }}>
              <Input placeholder="https://auth.example.com/end-session/" />
            </Form.Item>
            <Form.Item label="Redirect URI" style={{ marginBottom: 16 }}>
              <Space.Compact style={{ width: '100%' }}>
                <Form.Item name="redirect_url" noStyle>
                  <Input placeholder={`${window.location.origin}/api/v1/auth/callback`} />
                </Form.Item>
                <Button onClick={handleAutoFillRedirect}>自动填写</Button>
              </Space.Compact>
            </Form.Item>
          </div>
        </Card>

        {/* Section 3: 安全与选项 */}
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', marginBottom: 20 }}>
          <Title level={5} style={{ marginBottom: 4 }}>安全与选项</Title>
          <Paragraph type="secondary" style={{ marginBottom: 20 }}>Scope、Claim 字段配置与安全选项</Paragraph>

          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 16 }}>
            <Form.Item name="scopes" label="Scopes" style={{ marginBottom: 16 }}>
              <Input placeholder="openid profile email groups" />
            </Form.Item>
            <Form.Item name="groups_claim" label="Groups Claim 字段名" style={{ marginBottom: 16 }}>
              <Input placeholder="groups" />
            </Form.Item>
            <Form.Item name="groups_source" label="Groups 来源" style={{ marginBottom: 16 }}
              extra="选择从哪里读取用户的 groups claim。ID Token 受 Scope Mapping 表达式控制；Userinfo 可能返回完整组列表。">
              <Select options={[
                { value: 'id_token', label: 'ID Token' },
                { value: 'userinfo', label: 'Userinfo 端点' },
              ]} />
            </Form.Item>
          </div>

          <Divider style={{ margin: '16px 0' }} />

          <Space direction="vertical" size={16}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <Form.Item name="show_on_login_page" valuePropName="checked" noStyle>
                <Switch checkedChildren="开" unCheckedChildren="关" />
              </Form.Item>
              <span>在登录页显示"统一认证入口"按钮</span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <Form.Item name="cookie_secure" valuePropName="checked" noStyle>
                <Switch checkedChildren="开" unCheckedChildren="关" />
              </Form.Item>
              <span>Cookie Secure 模式（仅 HTTPS 环境启用）</span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <Form.Item name="enabled" valuePropName="checked" noStyle>
                <Switch checkedChildren="开" unCheckedChildren="关" />
              </Form.Item>
              <span style={{ fontWeight: 600 }}>启用 SSO 单点登录</span>
            </div>
          </Space>
        </Card>

        {/* Save button */}
        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 12 }}>
          <Button size="large" type="primary" htmlType="submit" loading={saving} style={{ borderRadius: 8, height: 44, paddingInline: 32 }}>
            保存配置
          </Button>
        </div>
      </Form>
    </div>
  );
}
