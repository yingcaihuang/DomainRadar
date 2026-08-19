import { create } from 'zustand';
import type { User } from '../types';
import { authApi } from '../services';

interface AuthState {
  user: User | null;
  loading: boolean;
  error: string | null;
  fetchUser: () => Promise<void>;
  logout: () => Promise<void>;
  hasPermission: (permission: string) => boolean;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  loading: true,
  error: null,

  fetchUser: async () => {
    try {
      set({ loading: true, error: null });
      const response = await authApi.me();
      set({ user: response.data, loading: false });
    } catch (error) {
      set({ user: null, loading: false, error: 'Authentication failed' });
    }
  },

  logout: async () => {
    try {
      await authApi.logout();
    } catch {
      // Ignore logout errors
    }
    set({ user: null });
    window.location.href = '/';
  },

  hasPermission: (permission: string) => {
    const { user } = get();
    if (!user) return false;
    const roles = user.roles || [];
    
    const permissionMatrix: Record<string, string[]> = {
      view_domains: ['viewer', 'operator', 'admin'],
      manage_domains: ['operator', 'admin'],
      view_alerts: ['viewer', 'operator', 'admin'],
      manage_alerts: ['operator', 'admin'],
      configure_integrations: ['admin'],
      manage_users: ['admin'],
      view_audit_logs: ['admin'],
    };

    const allowedRoles = permissionMatrix[permission] || [];
    return roles.some(role => allowedRoles.includes(role));
  },
}));
