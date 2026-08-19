import { useEffect, useState } from 'react';
import { Outlet } from 'react-router-dom';
import { Spin, Result, Button } from 'antd';
import { useAuthStore } from '../store';

export function AuthGuard() {
  const { user, loading, error, fetchUser } = useAuthStore();
  const [authMode, setAuthMode] = useState<string | null>(null);
  const [devLoading, setDevLoading] = useState(false);

  useEffect(() => {
    fetchUser();
    // Check auth mode
    fetch('/api/v1/auth/mode')
      .then(r => r.json())
      .then(data => setAuthMode(data.mode))
      .catch(() => setAuthMode('sso'));
  }, [fetchUser]);

  const handleDevLogin = async () => {
    setDevLoading(true);
    try {
      const res = await fetch('/api/v1/auth/dev-login', { method: 'POST' });
      if (res.ok) {
        // Refresh user state after dev login
        await fetchUser();
      }
    } catch {
      // ignore
    }
    setDevLoading(false);
  };

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}>
        <Spin size="large" tip="Loading..." />
      </div>
    );
  }

  if (error || !user) {
    return (
      <Result
        status="403"
        title="Authentication Required"
        subTitle="Please log in to access DomainRadar."
        extra={
          authMode === 'dev' ? (
            <Button type="primary" loading={devLoading} onClick={handleDevLogin}>
              Dev Login (Admin)
            </Button>
          ) : (
            <Button type="primary" onClick={() => window.location.href = '/api/v1/auth/login'}>
              Log In with SSO
            </Button>
          )
        }
      />
    );
  }

  return <Outlet />;
}
