import { type ReactNode } from 'react';

interface StatsCardProps {
  icon: ReactNode;
  label: string;
  value: string | number;
  color: 'indigo' | 'emerald' | 'amber' | 'red' | 'sky' | 'violet';
  suffix?: string;
}

const colorMap = {
  indigo: {
    bg: 'linear-gradient(135deg, #eef2ff 0%, #ffffff 100%)',
    accent: '#6366f1',
    iconBg: '#e0e7ff',
    text: '#4338ca',
  },
  emerald: {
    bg: 'linear-gradient(135deg, #ecfdf5 0%, #ffffff 100%)',
    accent: '#10b981',
    iconBg: '#d1fae5',
    text: '#065f46',
  },
  amber: {
    bg: 'linear-gradient(135deg, #fffbeb 0%, #ffffff 100%)',
    accent: '#f59e0b',
    iconBg: '#fef3c7',
    text: '#92400e',
  },
  red: {
    bg: 'linear-gradient(135deg, #fef2f2 0%, #ffffff 100%)',
    accent: '#ef4444',
    iconBg: '#fee2e2',
    text: '#991b1b',
  },
  sky: {
    bg: 'linear-gradient(135deg, #f0f9ff 0%, #ffffff 100%)',
    accent: '#0ea5e9',
    iconBg: '#e0f2fe',
    text: '#075985',
  },
  violet: {
    bg: 'linear-gradient(135deg, #f5f3ff 0%, #ffffff 100%)',
    accent: '#8b5cf6',
    iconBg: '#ede9fe',
    text: '#5b21b6',
  },
};

export function StatsCard({ icon, label, value, color, suffix }: StatsCardProps) {
  const scheme = colorMap[color];

  return (
    <div
      style={{
        background: scheme.bg,
        borderRadius: 12,
        border: '1px solid #e5e7eb',
        borderLeft: `4px solid ${scheme.accent}`,
        padding: '24px 28px',
        display: 'flex',
        alignItems: 'center',
        gap: 16,
        transition: 'box-shadow 0.2s',
      }}
    >
      <div
        style={{
          width: 52,
          height: 52,
          borderRadius: 12,
          background: scheme.iconBg,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 24,
          color: scheme.accent,
          flexShrink: 0,
        }}
      >
        {icon}
      </div>
      <div style={{ flex: 1 }}>
        <div style={{ fontSize: 14, color: '#6b7280', marginBottom: 6, fontWeight: 500 }}>
          {label}
        </div>
        <div style={{ fontSize: 32, fontWeight: 700, color: scheme.text, lineHeight: 1.2 }}>
          {value}
          {suffix && <span style={{ fontSize: 15, fontWeight: 500, marginLeft: 4, color: '#9ca3af' }}>{suffix}</span>}
        </div>
      </div>
    </div>
  );
}
