import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Spin, Tag, Card } from 'antd';
import FullCalendar from '@fullcalendar/react';
import dayGridPlugin from '@fullcalendar/daygrid';
import { domainApi } from '../services';
import type { CalendarEntry } from '../types';

const severityColors: Record<string, string> = {
  informational: '#6366f1',
  warning: '#f59e0b',
  critical: '#ef4444',
  expired: '#6b7280',
};

export function CalendarPage() {
  const [year, setYear] = useState(() => new Date().getFullYear());
  const [month, setMonth] = useState(() => new Date().getMonth() + 1);

  const { data, isLoading } = useQuery({
    queryKey: ['calendar', year, month],
    queryFn: () => domainApi.calendar({ year: String(year), month: String(month) }),
  });

  const events = data?.data?.map((entry: CalendarEntry) => ({
    title: `${entry.domain_name} (${entry.days_remaining < 0 ? '已过期' + Math.abs(entry.days_remaining) + '天' : entry.days_remaining + '天后'})`,
    date: entry.expiration_date,
    backgroundColor: severityColors[entry.severity] || '#6366f1',
    borderColor: severityColors[entry.severity] || '#6366f1',
    extendedProps: entry,
  })) || [];

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          到期日历
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>直观查看域名与证书的到期时间</p>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
        <div style={{ marginBottom: 16 }}>
          <Tag color={severityColors.informational} style={{ borderRadius: 6 }}>信息 (30+ 天)</Tag>
          <Tag color={severityColors.warning} style={{ borderRadius: 6 }}>警告 (8-30 天)</Tag>
          <Tag color={severityColors.critical} style={{ borderRadius: 6 }}>严重 (0-7 天)</Tag>
          <Tag color={severityColors.expired} style={{ borderRadius: 6 }}>已过期</Tag>
        </div>
        {isLoading ? <Spin /> : (
          <FullCalendar
            plugins={[dayGridPlugin as any]}
            initialView="dayGridMonth"
            events={events}
            height="auto"
            datesSet={(arg) => {
              // Use the midpoint of the visible range to determine the displayed month
              const mid = new Date((arg.start.getTime() + arg.end.getTime()) / 2);
              setYear(mid.getFullYear());
              setMonth(mid.getMonth() + 1);
            }}
            eventContent={(arg) => (
              <div style={{ padding: '2px 4px', fontSize: 11, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', cursor: 'pointer' }}>
                {arg.event.title}
              </div>
            )}
          />
        )}
      </Card>
    </div>
  );
}
