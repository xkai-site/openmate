export interface ChatErrorPayload {
  code?: string;
  technical_message?: string;
  details?: Record<string, unknown>;
  retryable?: boolean;
  provider_status_code?: number;
}

export class ChatServiceError extends Error {
  code?: string;
  technicalMessage?: string;
  details?: Record<string, unknown>;
  retryable?: boolean;
  providerStatusCode?: number;

  constructor(payload: ChatErrorPayload, fallbackTechnicalMessage: string) {
    const technicalMessage = String(payload.technical_message ?? '').trim() || fallbackTechnicalMessage;
    super(technicalMessage);
    this.name = 'ChatServiceError';
    this.code = String(payload.code ?? '').trim() || undefined;
    this.technicalMessage = technicalMessage;
    this.details = payload.details;
    this.retryable = payload.retryable;
    this.providerStatusCode = payload.provider_status_code;
  }
}

const CHAT_ERROR_MESSAGES: Record<string, string> = {
  insufficient_user_quota: '当前账号额度不足，请充值或更换可用模型后重试。',
  provider_rate_limited: '请求过于频繁，请稍后重试。',
  provider_timeout: '模型响应超时，请稍后重试。',
  provider_unreachable: '模型服务暂时不可用，请稍后重试。',
  provider_invalid_json: '模型返回异常数据，请稍后重试。',
  provider_http_error: '模型服务请求失败，请稍后重试。',
  chat_stream_failed: '本次对话中断，请稍后重试。',
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

export function toChatServiceError(raw: unknown, fallbackTechnicalMessage = 'chat request failed'): ChatServiceError {
  if (raw instanceof ChatServiceError) {
    return raw;
  }
  if (raw instanceof Error) {
    const payload = raw as unknown as ChatErrorPayload;
    return new ChatServiceError(payload, raw.message || fallbackTechnicalMessage);
  }
  if (isRecord(raw)) {
    return new ChatServiceError(raw as ChatErrorPayload, fallbackTechnicalMessage);
  }
  return new ChatServiceError({}, fallbackTechnicalMessage);
}

export function getChatFriendlyErrorMessage(raw: unknown, fallback = '发送失败，请重试'): string {
  const chatError = toChatServiceError(raw, fallback);
  const mapped = chatError.code ? CHAT_ERROR_MESSAGES[chatError.code] : '';
  const userMessage = mapped || fallback;
  if (import.meta.env.DEV && chatError.technicalMessage) {
    return `${userMessage}（${chatError.technicalMessage}）`;
  }
  return userMessage;
}
