import { API_BASE_URL, api } from '@/services/api';
import type {
  ApiResponse,
  ChatRequest,
  ChatResultResponse,
  ChatResponse,
  ChatStreamFatalEvent,
  ChatStreamInvocationEvent,
  ChatStreamMethodCallEvent,
  ChatStreamRequest,
  ChatStreamToolCallEvent,
  ChatStreamSummaryEvent,
  StreamPhase,
} from '@/types/models';

export interface ChatStreamHandlers {
  onInvocation?: (payload: ChatStreamInvocationEvent) => void;
  onPhase?: (payload: { phase: StreamPhase; [k: string]: unknown }) => void;
  onMethodCall?: (payload: ChatStreamMethodCallEvent) => void;
  onToolCall?: (payload: ChatStreamToolCallEvent) => void;
  onAssistantThinkingDelta?: (delta: string) => void;
  onAssistantDelta?: (delta: string) => void;
  onAssistantDone?: (reply: string) => void;
  onSummary?: (payload: ChatStreamSummaryEvent) => void;
  onFatal?: (payload: ChatStreamFatalEvent) => void;
}

/**
 * 非流式对话（兼容）
 */
export async function sendChatMessage(payload: ChatRequest): Promise<ChatResponse> {
  const response = await api.post<ApiResponse<ChatResponse>>('/chat', payload, {
    timeout: 0,
  });
  return response.data;
}

export async function getChatResult(
  invocationID: string,
  signal?: AbortSignal,
): Promise<ChatResultResponse> {
  const response = await api.get<ApiResponse<ChatResultResponse>>('/chat/result', {
    params: { invocation_id: invocationID },
    timeout: 0,
    signal,
  });
  return response.data;
}

export interface WaitChatResultOptions {
  intervalMs?: number;
  signal?: AbortSignal;
}

function abortError(): DOMException {
  return new DOMException('Aborted', 'AbortError');
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw abortError();
  }
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  if (!signal) {
    return new Promise((resolve) => {
      setTimeout(resolve, ms);
    });
  }
  if (signal.aborted) {
    return Promise.reject(abortError());
  }
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      signal.removeEventListener('abort', onAbort);
      resolve();
    };
    signal.addEventListener('abort', onAbort);
  });
}

export async function waitChatResult(
  invocationID: string,
  options: WaitChatResultOptions = {},
): Promise<ChatResultResponse> {
  const intervalMs = Math.max(200, options.intervalMs ?? 1500);
  const { signal } = options;

  while (true) {
    throwIfAborted(signal);
    const latest = await getChatResult(invocationID, signal);
    if (latest.status !== 'running') {
      return latest;
    }
    await sleep(intervalMs, signal);
    throwIfAborted(signal);
  }
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

function parseSSEChunk(
  chunk: string,
  state: { buffer: string },
  handlers: ChatStreamHandlers,
): { fatal?: Error } {
  state.buffer += chunk;
  const blocks = state.buffer.split(/\r?\n\r?\n/);
  state.buffer = blocks.pop() || '';

  for (const block of blocks) {
    const lines = block.split(/\r?\n/);
    let eventType = 'message';
    const dataLines: string[] = [];

    for (const line of lines) {
      if (line.startsWith('event:')) {
        eventType = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trim());
      }
    }

    if (dataLines.length === 0) continue;

    let payload: Record<string, unknown>;
    try {
      payload = JSON.parse(dataLines.join('\n')) as Record<string, unknown>;
    } catch {
      continue;
    }

    switch (eventType) {
      case 'invocation':
        handlers.onInvocation?.(payload as unknown as ChatStreamInvocationEvent);
        break;
      case 'phase':
        handlers.onPhase?.(payload as { phase: StreamPhase; [k: string]: unknown });
        break;
      case 'method_call':
        handlers.onMethodCall?.(payload as unknown as ChatStreamMethodCallEvent);
        break;
      case 'tool_call':
        handlers.onToolCall?.(payload as unknown as ChatStreamToolCallEvent);
        break;
      case 'assistant_thinking_delta':
        handlers.onAssistantThinkingDelta?.(String(payload.delta ?? ''));
        break;
      case 'assistant_delta':
        handlers.onAssistantDelta?.(String(payload.delta ?? ''));
        break;
      case 'assistant_done':
        handlers.onAssistantDone?.(String(payload.reply ?? ''));
        break;
      case 'summary':
        handlers.onSummary?.(payload as unknown as ChatStreamSummaryEvent);
        break;
      case 'fatal':
        handlers.onFatal?.(payload as unknown as ChatStreamFatalEvent);
        return { fatal: new Error(String(payload.message ?? '流式对话失败')) };
      default:
        break;
    }
  }

  return {};
}

export async function sendChatMessageStream(
  payload: ChatStreamRequest,
  handlers: ChatStreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const response = await fetch(resolveApiUrl('/chat/stream'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
    signal,
  });


  if (!response.ok) {
    let detail = '';
    try {
      const rawText = await response.text();
      if (rawText.trim()) {
        const parsed = JSON.parse(rawText) as ApiResponse<unknown>;
        if (parsed?.message) {
          detail = `: ${parsed.message}`;
        } else {
          detail = `: ${rawText}`;
        }
      }
    } catch {
      // ignore parse errors and fallback to status only
    }
    throw new Error(`流式请求失败: ${response.status}${detail}`);
  }

  if (!response.body) {
    throw new Error('浏览器不支持流式响应');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  const state = { buffer: '' };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    const chunk = decoder.decode(value, { stream: true });
    const parsed = parseSSEChunk(chunk, state, handlers);
    if (parsed.fatal) {
      throw parsed.fatal;
    }
  }

  if (state.buffer.trim()) {
    const parsed = parseSSEChunk('\n\n', state, handlers);
    if (parsed.fatal) {
      throw parsed.fatal;
    }
  }
}


