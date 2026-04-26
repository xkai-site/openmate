import { API_BASE_URL, api } from '@/services/api';
import type {
  ApiResponse,
  ExecutionResultResponse,
  PaginatedResponse,
  TaskLogResponse,
  TaskResponse,
  TaskResultsResponse,
  TopicStatusResponse,
  TopicWorkspaceBinding,
  TopicWorkspaceUpdateRequest,
} from '@/types/models';

export class TopicWorkspaceUnavailableError extends Error {
  constructor(message = 'Topic workspace backend endpoint is unavailable') {
    super(message);
    this.name = 'TopicWorkspaceUnavailableError';
  }
}

export class TopicWorkspaceRequestError extends Error {
  status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = 'TopicWorkspaceRequestError';
    this.status = status;
  }
}

interface ApiEnvelope<T> {
  code?: number;
  message?: string;
  data?: T;
}

function resolveApiUrl(pathname: string): string {
  const base = API_BASE_URL || '/api/v1';
  if (/^https?:\/\//.test(base)) {
    return `${base.replace(/\/$/, '')}${pathname.startsWith('/') ? pathname : `/${pathname}`}`;
  }
  const normalizedBase = base.startsWith('/') ? base : `/${base}`;
  const normalizedPath = pathname.startsWith('/') ? pathname : `/${pathname}`;
  return `${normalizedBase.replace(/\/$/, '')}${normalizedPath}`;
}

function toMaybeJSON(raw: string): ApiEnvelope<unknown> | null {
  if (!raw.trim()) {
    return null;
  }
  try {
    return JSON.parse(raw) as ApiEnvelope<unknown>;
  } catch {
    return null;
  }
}

async function requestTopicWorkspace<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(resolveApiUrl(path), init);
  const text = await response.text();
  const envelope = toMaybeJSON(text);
  const businessCode = envelope?.code;
  const businessMessage = envelope?.message;

  if (response.status === 404 || response.status === 501 || businessCode === 404 || businessCode === 501) {
    throw new TopicWorkspaceUnavailableError();
  }

  if (!response.ok) {
    throw new TopicWorkspaceRequestError(
      businessMessage || `Topic workspace request failed with status ${response.status}`,
      response.status,
    );
  }

  if (typeof businessCode === 'number' && businessCode !== 200) {
    throw new TopicWorkspaceRequestError(
      businessMessage || `Topic workspace business error ${businessCode}`,
      businessCode,
    );
  }

  return (envelope?.data as T) ?? (null as T);
}

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

export async function getTopicWorkspaceBinding(topicId: string): Promise<TopicWorkspaceBinding | null> {
  return requestTopicWorkspace<TopicWorkspaceBinding | null>(`/topics/${topicId}/workspace`);
}

export async function updateTopicWorkspaceBinding(
  topicId: string,
  payload: TopicWorkspaceUpdateRequest,
): Promise<TopicWorkspaceBinding | null> {
  return requestTopicWorkspace<TopicWorkspaceBinding | null>(`/topics/${topicId}/workspace`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}
