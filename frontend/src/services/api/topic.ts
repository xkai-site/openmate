import { API_BASE_URL, api } from '@/services/api';
import type {
  ApiResponse,
  NodeResponse,
  PaginatedResponse,
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
  const response = await api.get<ApiResponse<TopicStatusResponse[]>>('/topics');
  const items = response.data ?? [];
  return {
    items: items.slice(offset, offset + limit),
    total: items.length,
    limit,
    offset,
  };
}

export async function getTopicById(topicId: string): Promise<TopicStatusResponse> {
  const response = await api.get<ApiResponse<TopicStatusResponse>>(`/topics/${topicId}`);
  return response.data;
}

export async function listTopicNodes(topicId: string): Promise<NodeResponse[]> {
  const response = await api.get<ApiResponse<NodeResponse[]>>(`/topics/${topicId}/nodes`);
  return response.data;
}

export async function deleteTopic(topicId: string): Promise<unknown> {
  const response = await api.delete<ApiResponse<unknown>>(`/topics/${topicId}`);
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
