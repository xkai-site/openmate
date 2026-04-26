import { api } from '@/services/api';
import type {
  ApiResponse,
  ToolMonitorEvent,
  ToolMonitorQuery,
  ToolMonitorSummaryItem,
} from '@/types/models';

function validatePositiveInteger(value: number | undefined, fieldName: 'limit' | 'window_minutes'): void {
  if (value === undefined) {
    return;
  }
  if (!Number.isInteger(value) || value <= 0) {
    throw new Error(`${fieldName} must be a positive integer`);
  }
}

function buildToolMonitorParams(query?: ToolMonitorQuery): ToolMonitorQuery | undefined {
  if (!query) {
    return undefined;
  }
  validatePositiveInteger(query.limit, 'limit');
  validatePositiveInteger(query.window_minutes, 'window_minutes');
  return query;
}

export async function listToolMonitorEvents(query?: ToolMonitorQuery): Promise<ToolMonitorEvent[]> {
  const response = await api.get<ApiResponse<ToolMonitorEvent[]>>('/tools/monitor/events', {
    params: buildToolMonitorParams(query),
  });
  return response.data ?? [];
}

export async function listToolMonitorSummary(query?: ToolMonitorQuery): Promise<ToolMonitorSummaryItem[]> {
  const response = await api.get<ApiResponse<ToolMonitorSummaryItem[]>>('/tools/monitor/summary', {
    params: buildToolMonitorParams(query),
  });
  return response.data ?? [];
}
