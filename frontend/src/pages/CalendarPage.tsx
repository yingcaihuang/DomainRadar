import { useState, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Spin, Tag, Card, Segmented } from 'antd';
import FullCalendar from '@fullcalendar/react';
import dayGridPlugin from '@fullcalendar/daygrid';
import multiMonthPlugin from '@fullcalendar/multimonth';
import { domainApi } from '../services';
import type { CalendarEntry } from '../types';

const severityColors: Record<string, string> = {
  informational: '#6366f1',
  warning: '#f59e0b',
  critical: '#ef4444',
  expired: '#6b7280',
};

type ViewMode = 'year' | 'quarter' | 'month';

export function CalendarPage() {
  const [viewMode, setViewMode] = useState<ViewMode>('year');
  const [dateRange, setDateRange] = useState<{ start: string; end: string }>(() => {
    const now = new Date();
    return { start: `${now.getFullYear()}-01-01`, end: `${now.getFullYear()}-12-31` };
  });
  const calendarRef = useRef<any>(null);

  const { data, isLoading } = useQuery({
    queryKey: ['calendar', dateRange.start, dateRange.end],
    queryFn: () => domainApi.calendar({ start: dateRange.start, end: dateRange.end }),
  });

  const events = data?.data?.map((entry: CalendarEntry) => ({
    title: `${entry.domain_name} (${entry.days_remaining < 0 ? '已过期' + Math.abs(entry.days_remaining) + '天' : entry.days_remaining + '天'})`,
    date: entry.expiration_date,
    backgroundColor: severityColors[entry.severity] || '#6366f1',
    borderColor: severityColors[entry.severity] || '#6366f1',
    extendedProps: entry,
  })) || [];

  const getCalendarView = (mode: ViewMode) => {
    switch (mode) {
      case 'year': return 'multiMonthYear';
      case 'quarter': return 'dayGridMonth'; // 3-month with duration
      case 'month': return 'dayGridMonth';
    }
  };

  const handleViewChange = (mode: ViewMode) => {
    setViewMode(mode);
    const api = calendarRef.current?.getApi();
    if (!api) return;

    switch (mode) {
      case 'year':
        api.changeView('multiMonthYear');
        break;
      case 'quarter':
        api.changeView('dayGridMonth', { duration: { months: 3 } });
        break;
      case 'month':
        api.changeView('dayGridMonth');
        break;
    }
  };

  const handleDatesSet = (arg: any) => {
    const start = arg.start.toISOString().slice(0, 10);
    const end = arg.end.toISOString().slice(0, 10);
    setDateRange({ start, end });
  };

  return (
    <div>
      <div style={{ marginBottom: 24 }}>
        <h1 style={{ fontSize: 32, fontWeight: 700, margin: 0, background: 'linear-gradient(135deg, #6366f1, #8b5cf6)', WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }}>
          到期日历
        </h1>
        <p style={{ fontSize: 15, color: '#6b7280', marginTop: 4, marginBottom: 0 }}>直观查看域名与证书的到期时间</p>
      </div>

      <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb' }}>
        <div style={{ marginBottom: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: 12 }}>
          <div>
            <Tag color={severityColors.informational} style={{ borderRadius: 6 }}>信息 (30+ 天)</Tag>
            <Tag color={severityColors.warning} style={{ borderRadius: 6 }}>警告 (8-30 天)</Tag>
            <Tag color={severityColors.critical} style={{ borderRadius: 6 }}>严重 (0-7 天)</Tag>
            <Tag color={severityColors.expired} style={{ borderRadius: 6 }}>已过期</Tag>
          </div>
          <Segmented
            value={viewMode}
            onChange={(v) => handleViewChange(v as ViewMode)}
            options={[
              { label: '年', value: 'year' },
              { label: '季度', value: 'quarter' },
              { label: '月', value: 'month' },
            ]}
          />
        </div>
        {isLoading ? <Spin /> : (
          <FullCalendar
            ref={calendarRef}
            plugins={[dayGridPlugin as any, multiMonthPlugin as any]}
            initialView={getCalendarView(viewMode)}
            events={events}
            height="auto"
            datesSet={handleDatesSet}
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
