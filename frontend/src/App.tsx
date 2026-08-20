import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ConfigProvider, App as AntdApp } from 'antd';
import { AppLayout } from './components/AppLayout';
import { AuthGuard } from './components/AuthGuard';
import { LoginPage } from './pages/LoginPage';
import { ChangePasswordPage } from './pages/ChangePasswordPage';
import { DashboardPage } from './pages/DashboardPage';
import { DomainListPage } from './pages/DomainListPage';
import { DomainDetailPage } from './pages/DomainDetailPage';
import { DomainFormPage } from './pages/DomainFormPage';
import { CalendarPage } from './pages/CalendarPage';
import { AlertsPage } from './pages/AlertsPage';
import { RegistrarSettingsPage } from './pages/RegistrarSettingsPage';
import { NotificationSettingsPage } from './pages/NotificationSettingsPage';
import { UserManagementPage } from './pages/UserManagementPage';
import { AuditLogPage } from './pages/AuditLogPage';
import { TagsGroupsPage } from './pages/TagsGroupsPage';
import { ExpirationRulesPage } from './pages/ExpirationRulesPage';
import { SsoConfigPage } from './pages/SsoConfigPage';
import { GroupMappingPage } from './pages/GroupMappingPage';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 30000,
    },
  },
});

function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ConfigProvider theme={{
        token: {
          colorPrimary: '#6366f1',
          borderRadius: 10,
          colorBgContainer: '#ffffff',
          colorBgLayout: '#f8fafc',
          fontFamily: "system-ui, -apple-system, 'Segoe UI', sans-serif",
          fontSize: 14,
        },
        components: {
          Menu: {
            itemSelectedBg: '#6366f1',
            itemSelectedColor: '#ffffff',
            itemHoverBg: '#f5f3ff',
            fontSize: 14,
            itemHeight: 44,
            iconSize: 16,
            itemMarginBlock: 4,
          },
          Table: {
            headerBg: '#f8fafc',
            borderColor: '#e5e7eb',
            cellPaddingBlock: 14,
            cellPaddingInline: 16,
            headerSplitColor: '#e5e7eb',
            fontSize: 14,
          },
          Card: {
            headerFontSize: 16,
          },
          Descriptions: {
            fontSize: 14,
          },
        },
      }}>
        <AntdApp>
          <BrowserRouter>
            <Routes>
              {/* Public routes */}
              <Route path="/login" element={<LoginPage />} />

              {/* Protected routes */}
              <Route element={<AuthGuard />}>
                {/* Change password (inside auth but outside layout for must-change flow) */}
                <Route path="/change-password" element={<ChangePasswordPage />} />

                <Route element={<AppLayout />}>
                  <Route path="/dashboard" element={<DashboardPage />} />
                  <Route path="/domains" element={<DomainListPage />} />
                  <Route path="/domains/new" element={<DomainFormPage />} />
                  <Route path="/domains/:id" element={<DomainDetailPage />} />
                  <Route path="/domains/:id/edit" element={<DomainFormPage />} />
                  <Route path="/tags-groups" element={<TagsGroupsPage />} />
                  <Route path="/calendar" element={<CalendarPage />} />
                  <Route path="/alerts" element={<AlertsPage />} />
                  <Route path="/settings/rules" element={<ExpirationRulesPage />} />
                  <Route path="/settings/registrars" element={<RegistrarSettingsPage />} />
                  <Route path="/settings/notifications" element={<NotificationSettingsPage />} />
                  <Route path="/settings/users" element={<UserManagementPage />} />
                  <Route path="/settings/group-mappings" element={<GroupMappingPage />} />
                  <Route path="/settings/audit" element={<AuditLogPage />} />
                  <Route path="/settings/sso" element={<SsoConfigPage />} />
                  <Route path="/" element={<Navigate to="/dashboard" replace />} />
                </Route>
              </Route>
            </Routes>
          </BrowserRouter>
        </AntdApp>
      </ConfigProvider>
    </QueryClientProvider>
  );
}

export default App;
