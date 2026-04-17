import { api } from '@/services/api';
import type {
  ApiResponse,
  ExecutionResultResponse,
  PaginatedResponse,
  TaskLogResponse,
  TaskResponse,
  TaskResultsResponse,
  TopicStatusResponse,
} from '@/types/models';

export async function listTopics(limit = 20, offset = 0): Promise<PaginatedResponse<TopicStatusResponse>> {
  const response = await api.get<ApiResponse<PaginatedResponse<TopicStatusResponse>>>('/topic', {
    params: { limit, offset },
  });
  return response.data;
}

export async function getTopicById(topicId: string): Promise<TopicStatusResponse> {
  const response = await api.get<ApiResponse<TopicStatusResponse>>(`/topic/${topicId}`);
  return response.data;
}

export async function executeNextTask(topicId: string): Promise<ExecutionResultResponse> {
  const response = await api.post<ApiResponse<ExecutionResultResponse>>(`/topic/${topicId}/execute`);
  return response.data;
}

export async function executeAllTasks(topicId: string): Promise<ExecutionResultResponse[]> {
  const response = await api.post<ApiResponse<ExecutionResultResponse[]>>(`/topic/${topicId}/execute-all`);
  return response.data;
}

export async function pauseTopic(topicId: string): Promise<null> {
  const response = await api.post<ApiResponse<null>>(`/topic/${topicId}/pause`);
  return response.data;
}

export async function resumeTopic(topicId: string): Promise<null> {
  const response = await api.post<ApiResponse<null>>(`/topic/${topicId}/resume`);
  return response.data;
}

export async function deleteTopic(topicId: string): Promise<null> {
  const response = await api.delete<ApiResponse<null>>(`/topic/${topicId}`);
  return response.data;
}

export async function getTopicTask(topicId: string, taskId: string): Promise<TaskResponse> {
  const response = await api.get<ApiResponse<TaskResponse>>(`/topic/${topicId}/task/${taskId}`);
  return response.data;
}

export async function retryTask(topicId: string, taskId: string): Promise<{ success: boolean; task_id: string; message: string }> {
  const response = await api.post<ApiResponse<{ success: boolean; task_id: string; message: string }>>(
    `/topic/${topicId}/task/${taskId}/retry`,
  );
  return response.data;
}

export async function getTopicResults(topicId: string): Promise<TaskResultsResponse> {
  const response = await api.get<ApiResponse<TaskResultsResponse>>(`/topic/${topicId}/results`);
  return response.data;
}

export async function getTopicLogs(topicId: string): Promise<TaskLogResponse[]> {
  const response = await api.get<ApiResponse<TaskLogResponse[]>>(`/topic/${topicId}/logs`);
  return response.data;
}