import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Alert, Button, Card, Empty, Input, InputNumber, Select, Space, Table, Tabs, Tag, Tooltip, Typography, App } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ReloadOutlined } from '@ant-design/icons';
import { listToolMonitorEvents, listToolMonitorSummary } from '@/services/api/toolMonitor';
import type { ToolMonitorEvent, ToolMonitorQuery, ToolMonitorSource, ToolMonitorSummaryItem } from '@/types/models';

const AUTO_REFRESH_MS = 30_000;
const WINDOW_PRESETS = [15, 60, 1440] as const;
const SOURCE_OPTIONS: Array<{ label: string; value: ToolMonitorSource }> = [
  { label: 'model', value: 'model' },
  { label: 'cli', value: 'cli' },
  { label: 'http', value: 'http' },
  { label: 'unknown', value: 'unknown' },
];

function formatLocalTime(raw: string): string {
  const parsed = new Date(raw);
  if (Number.isNaN(parsed.getTime())) {
    return raw;
  }
  return parsed.toLocaleString('zh-CN', { hour12: false });
}

function formatPercent(value: number): string {
  return `${(value * 100).toFixed(1)}%`;
}

function formatDuration(value: number): string {
  return Number.isFinite(value) ? value.toFixed(1) : '-';
}

interface QueryState {
  toolName: string;
  nodeId: string;
  source?: ToolMonitorSource;
  success?: boolean;
  limit?: number;
  windowMinutes?: number;
}

function buildQuery(state: QueryState): { query?: ToolMonitorQuery; error?: string } {
  if (state.limit !== undefined && (!Number.isInteger(state.limit) || state.limit <= 0)) {
    return { error: 'limit 必须为正整数' };
  }
  if (state.windowMinutes !== undefined && (!Number.isInteger(state.windowMinutes) || state.windowMinutes <= 0)) {
    return { error: 'window_minutes 必须为正整数' };
  }
  return {
    query: {
      tool_name: state.toolName.trim() || undefined,
      node_id: state.nodeId.trim() || undefined,
      source: state.source,
      success: state.success,
      limit: state.limit,
      window_minutes: state.windowMinutes,
    },
  };
}

export default function ToolMonitorPanel() {
  const [filters, setFilters] = useState<QueryState>({
    toolName: '',
    nodeId: '',
    source: undefined,
    success: undefined,
    limit: 100,
    windowMinutes: 60,
  });
  const [events, setEvents] = useState<ToolMonitorEvent[]>([]);
  const [summary, setSummary] = useState<ToolMonitorSummaryItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [errorText, setErrorText] = useState<string | null>(null);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<string | null>(null);
  const { message } = App.useApp();
  const lastValidationRef = useRef<string | null>(null);

  const queryResult = useMemo(() => buildQuery(filters), [filters]);

  const loadData = useCallback(
    async (nextQuery: ToolMonitorQuery, manual = false) => {
      if (manual) {
        setRefreshing(true);
      } else {
        setLoading(true);
      }
      setErrorText(null);
      try {
        const [eventData, summaryData] = await Promise.all([
          listToolMonitorEvents(nextQuery),
          listToolMonitorSummary(nextQuery),
        ]);
        setEvents(eventData);
        setSummary(summaryData);
        setLastUpdatedAt(new Date().toISOString());
      } catch (error) {
        const text = error instanceof Error ? error.message : '加载工具监控数据失败';
        setErrorText(text);
      } finally {
        if (manual) {
          setRefreshing(false);
        } else {
          setLoading(false);
        }
      }
    },
    [],
  );

  const handleRefresh = useCallback(() => {
    if (!queryResult.query) {
      const err = queryResult.error ?? '筛选参数不合法';
      message.warning(err);
      return;
    }
    void loadData(queryResult.query, true);
  }, [loadData, message, queryResult.error, queryResult.query]);

  useEffect(() => {
    if (!queryResult.query) {
      if (queryResult.error && lastValidationRef.current !== queryResult.error) {
        lastValidationRef.current = queryResult.error;
        message.warning(queryResult.error);
      }
      return;
    }
    lastValidationRef.current = null;
    void loadData(queryResult.query);
  }, [loadData, message, queryResult.error, queryResult.query]);

  useEffect(() => {
    if (!queryResult.query) {
      return;
    }
    const timer = window.setInterval(() => {
      void loadData(queryResult.query as ToolMonitorQuery, true);
    }, AUTO_REFRESH_MS);
    return () => {
      window.clearInterval(timer);
    };
  }, [loadData, queryResult.query]);

  const summaryColumns = useMemo<ColumnsType<ToolMonitorSummaryItem>>(
    () => [
      {
        title: '工具名',
        dataIndex: 'tool_name',
        key: 'tool_name',
      },
      {
        title: '调用次数',
        dataIndex: 'count',
        key: 'count',
        width: 120,
      },
      {
        title: '成功率',
        dataIndex: 'success_rate',
        key: 'success_rate',
        width: 140,
        render: (value: number) => formatPercent(value),
      },
      {
        title: '平均耗时(ms)',
        dataIndex: 'avg_duration_ms',
        key: 'avg_duration_ms',
        width: 160,
        render: (value: number) => formatDuration(value),
      },
      {
        title: 'P95耗时(ms)',
        dataIndex: 'p95_duration_ms',
        key: 'p95_duration_ms',
        width: 160,
        render: (value: number) => formatDuration(value),
      },
    ],
    [],
  );

  const eventColumns = useMemo<ColumnsType<ToolMonitorEvent>>(
    () => [
      { title: 'event_id', dataIndex: 'event_id', key: 'event_id', width: 180, ellipsis: true },
      { title: 'phase', dataIndex: 'phase', key: 'phase', width: 90 },
      {
        title: 'ts',
        dataIndex: 'ts',
        key: 'ts',
        width: 170,
        render: (value: string) => (
          <Tooltip title={value}>
            <span>{formatLocalTime(value)}</span>
          </Tooltip>
        ),
      },
      { title: 'node_id', dataIndex: 'node_id', key: 'node_id', width: 150, ellipsis: true },
      { title: 'tool_name', dataIndex: 'tool_name', key: 'tool_name', width: 130 },
      { title: 'source', dataIndex: 'source', key: 'source', width: 90 },
      {
        title: 'is_safe',
        dataIndex: 'is_safe',
        key: 'is_safe',
        width: 90,
        render: (value: boolean) => (value ? 'true' : 'false'),
      },
      {
        title: 'is_read_only',
        dataIndex: 'is_read_only',
        key: 'is_read_only',
        width: 110,
        render: (value: boolean) => (value ? 'true' : 'false'),
      },
      { title: 'request_id', dataIndex: 'request_id', key: 'request_id', width: 160, ellipsis: true },
      {
        title: 'success',
        dataIndex: 'success',
        key: 'success',
        width: 90,
        render: (value: boolean | undefined, record) => {
          if (record.phase === 'before') {
            return '-';
          }
          if (value === undefined) {
            return '-';
          }
          return value ? <Tag color="success">true</Tag> : <Tag color="error">false</Tag>;
        },
      },
      {
        title: 'error_code',
        dataIndex: 'error_code',
        key: 'error_code',
        width: 120,
        render: (value: string | undefined, record) => (record.phase === 'before' ? '-' : value || '-'),
      },
      {
        title: 'duration_ms',
        dataIndex: 'duration_ms',
        key: 'duration_ms',
        width: 110,
        render: (value: number | undefined, record) => (record.phase === 'before' ? '-' : value ?? '-'),
      },
    ],
    [],
  );

  return (
    <div className="tool-monitor-root">
      <Card className="tool-monitor-filter" size="small">
        <div className="tool-monitor-filter-grid">
          <Input
            placeholder="tool_name"
            value={filters.toolName}
            onChange={(e) => setFilters((prev) => ({ ...prev, toolName: e.target.value }))}
          />
          <Input
            placeholder="node_id"
            value={filters.nodeId}
            onChange={(e) => setFilters((prev) => ({ ...prev, nodeId: e.target.value }))}
          />
          <Select
            allowClear
            placeholder="source"
            value={filters.source}
            options={SOURCE_OPTIONS}
            onChange={(value) => setFilters((prev) => ({ ...prev, source: value }))}
          />
          <Select
            placeholder="success"
            value={filters.success === undefined ? 'all' : String(filters.success)}
            options={[
              { label: '全部', value: 'all' },
              { label: 'true', value: 'true' },
              { label: 'false', value: 'false' },
            ]}
            onChange={(value) =>
              setFilters((prev) => ({
                ...prev,
                success: value === 'all' ? undefined : value === 'true',
              }))
            }
          />
          <InputNumber
            min={1}
            precision={0}
            placeholder="limit"
            style={{ width: '100%' }}
            value={filters.limit}
            onChange={(value) => setFilters((prev) => ({ ...prev, limit: value === null ? undefined : Number(value) }))}
          />
          <div className="tool-monitor-window">
            <Space size={6} wrap>
              {WINDOW_PRESETS.map((windowMinute) => (
                <Button
                  key={windowMinute}
                  size="small"
                  type={filters.windowMinutes === windowMinute ? 'primary' : 'default'}
                  onClick={() => setFilters((prev) => ({ ...prev, windowMinutes: windowMinute }))}
                >
                  {windowMinute}m
                </Button>
              ))}
              <InputNumber
                min={1}
                precision={0}
                placeholder="自定义分钟"
                value={filters.windowMinutes}
                onChange={(value) =>
                  setFilters((prev) => ({ ...prev, windowMinutes: value === null ? undefined : Number(value) }))
                }
              />
            </Space>
          </div>
        </div>
        <div className="tool-monitor-actions">
          <Button icon={<ReloadOutlined />} onClick={handleRefresh} loading={refreshing}>
            刷新
          </Button>
          <Typography.Text type="secondary">自动刷新：30 秒</Typography.Text>
          <Typography.Text type="secondary">
            {lastUpdatedAt ? `最近更新：${formatLocalTime(lastUpdatedAt)}` : '最近更新：--'}
          </Typography.Text>
        </div>
      </Card>

      {queryResult.error && <Alert type="warning" showIcon message={queryResult.error} />}
      {errorText && <Alert type="error" showIcon message={errorText} />}

      <Tabs
        items={[
          {
            key: 'summary',
            label: '汇总表',
            children: (
              <Card size="small">
                <Table<ToolMonitorSummaryItem>
                  rowKey={(record) => record.tool_name}
                  loading={loading}
                  columns={summaryColumns}
                  dataSource={summary}
                  locale={{ emptyText: <Empty description="暂无汇总数据" /> }}
                  pagination={false}
                  scroll={{ x: 720 }}
                />
              </Card>
            ),
          },
          {
            key: 'events',
            label: '事件表',
            children: (
              <Card size="small">
                <Table<ToolMonitorEvent>
                  rowKey={(record) => `${record.event_id}-${record.phase}`}
                  loading={loading}
                  columns={eventColumns}
                  dataSource={events}
                  locale={{ emptyText: <Empty description="暂无事件数据" /> }}
                  pagination={false}
                  scroll={{ x: 1600, y: 500 }}
                />
              </Card>
            ),
          },
        ]}
      />

      <style>{`
        .tool-monitor-root {
          height: 100%;
          padding: 16px 20px;
          display: flex;
          flex-direction: column;
          gap: 12px;
          overflow: auto;
        }
        .tool-monitor-filter-grid {
          display: grid;
          grid-template-columns: repeat(6, minmax(0, 1fr));
          gap: 8px;
        }
        .tool-monitor-window {
          grid-column: span 2;
        }
        .tool-monitor-actions {
          margin-top: 10px;
          display: flex;
          align-items: center;
          gap: 10px;
        }
        @media (max-width: 1200px) {
          .tool-monitor-filter-grid {
            grid-template-columns: repeat(2, minmax(0, 1fr));
          }
          .tool-monitor-window {
            grid-column: auto;
          }
        }
      `}</style>
    </div>
  );
}
