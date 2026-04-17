import { api } from '@/services/api';
import type { AgentStatsResponse, ApiResponse, HealthResponse, QueueStatsResponse } from '@/types/models';

export async function getQueueStats(): Promise<QueueStatsResponse> {
  const response = await api.get<ApiResponse<QueueStatsResponse>>('/stats/queue');
  return response.data;
}

export async function getAgentStats(): Promise<AgentStatsResponse> {
  const response = await api.get<ApiResponse<AgentStatsResponse>>('/stats/agent');
  return response.data;
}

export async function healthCheck(): Promise<HealthResponse> {
  const response = await api.get<ApiResponse<HealthResponse>>('/health');
  return response.data;
}