import React, { useState, useRef, useEffect, useCallback } from 'react';
import { Input, Button, Avatar, Tooltip, Spin } from 'antd';
import { SendOutlined, UserOutlined, RobotOutlined, CopyOutlined, ExperimentOutlined } from '@ant-design/icons';
import { message as antMessage } from 'antd';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { sendChatMessage, sendChatMessageStream } from '@/services/api/chat';
import { getNode } from '@/services/api/nodes';
import type {
  ChatMessage,
  ChatStreamMethodCallEvent,
  ChatStreamSummaryEvent,
  NodeResponse,
  StreamPhase,
  ToolTrace,
} from '@/types/models';



type ThemeMode = 'dark' | 'light';

export interface SessionPanelProps {
  nodeId: string;
  themeMode?: ThemeMode;
  /** AI 每次回复完成后触发，用于通知外部刷新 Memory */
  onAIReply?: () => void;
}

// ── ThinkingBubble ──────────────────────────────────────────────

function ThinkingBubble({
  thinking,
  streaming,
  themeMode,
}: {
  thinking: string;
  streaming?: boolean;
  themeMode: ThemeMode;
}) {
  const [open, setOpen] = useState(false);

  // 流式过程中自动展开
  useEffect(() => {
    if (streaming && thinking) setOpen(true);
  }, [streaming, thinking]);

  if (!thinking) return null;

  return (
    <details
      open={open}
      onToggle={(e) => setOpen((e.target as HTMLDetailsElement).open)}
      className={`chat-thinking ${themeMode}`}
    >
      <summary className="chat-thinking-summary">
        <ExperimentOutlined style={{ marginRight: 6 }} />
        思考过程
        {streaming && <span className="chat-thinking-streaming-dot" />}
      </summary>
      <div className="chat-thinking-content">
        <ReactMarkdown remarkPlugins={[remarkGfm]}>{thinking}</ReactMarkdown>
      </div>
    </details>
  );
}

// ── ToolCallBadge ───────────────────────────────────────────────

function ToolCallBadge({ traces, themeMode }: { traces: ToolTrace[]; themeMode: ThemeMode }) {
  if (!traces || traces.length === 0) return null;

  // 只展示 result / error 状态的工具调用
  const completed = traces.filter((t) => t.call === 'result' || t.call === 'error');
  if (completed.length === 0) return null;

  return (
    <div className={`chat-tool-traces ${themeMode}`}>
      {completed.map((trace, idx) => (
        <details key={`${trace.tool}-${idx}`} className={`chat-tool-item ${trace.call === 'error' ? 'error' : ''} ${themeMode}`}>
          <summary>
            <span className="tool-icon">⚙</span>
            {trace.tool}
            {trace.call === 'error' && <span className="tool-error-badge">失败</span>}
          </summary>
          <div className="tool-detail">
            <div className="tool-section-label">参数</div>
            <pre>{JSON.stringify(trace.args, null, 2)}</pre>
            {trace.result && (
              <>
                <div className="tool-section-label">结果</div>
                <pre>{JSON.stringify(trace.result, null, 2)}</pre>
              </>
            )}
            {trace.error && <pre className="tool-error-text">{trace.error}</pre>}
          </div>
        </details>
      ))}
    </div>
  );
}

// ── MessageBubble ───────────────────────────────────────────────

function MessageBubble({ msg, themeMode }: { msg: ChatMessage; themeMode: ThemeMode }) {
  const isUser = msg.role === 'user';

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(msg.content);
    antMessage.success('已复制');
  }, [msg.content]);

  return (
    <article className={`chat-bubble-row ${isUser ? 'is-user' : 'is-ai'}`}>
      <Avatar
        size={30}
        icon={isUser ? <UserOutlined /> : <RobotOutlined />}
        className={`chat-avatar ${isUser ? 'user' : 'ai'} ${themeMode}`}
      />

      <div className={`chat-bubble-wrap ${isUser ? 'items-end' : 'items-start'}`}>
        <span className={`chat-meta ${themeMode}`}>
          {isUser ? '你' : 'AI 助手'}
          {msg.timestamp ? <span className="ml-2">{new Date(msg.timestamp).toLocaleTimeString()}</span> : null}
          {!isUser && msg.usage?.total_tokens ? (
            <Tooltip title={`Prompt: ${msg.usage.prompt_tokens} | Completion: ${msg.usage.completion_tokens}`}>
              <span className="ml-2 opacity-60 text-[10px]">
                {msg.usage.total_tokens} tokens
              </span>
            </Tooltip>
          ) : null}
        </span>

        {/* 思考过程（仅 AI 消息）*/}
        {!isUser && msg.thinking && (
          <ThinkingBubble thinking={msg.thinking} themeMode={themeMode} />
        )}

        <div className={`chat-bubble ${isUser ? 'user' : 'ai'} ${themeMode}`}>
          {isUser ? (
            <span style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{msg.content}</span>
          ) : (
            <div className="chat-md-content">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{msg.content}</ReactMarkdown>
            </div>
          )}

          <Tooltip title="复制">
            <button
              aria-label="复制消息"
              onClick={handleCopy}
              className={`chat-copy-btn ${themeMode}`}
            >
              <CopyOutlined />
            </button>
          </Tooltip>
        </div>

        {/* Tool Call 轨迹 */}
        {!isUser && msg.tool_traces && msg.tool_traces.length > 0 && (
          <ToolCallBadge traces={msg.tool_traces} themeMode={themeMode} />
        )}

        {/* 旧版 method_traces 兼容 */}
        {!isUser && msg.method_traces && msg.method_traces.length > 0 ? (
          <div className={`chat-method-traces ${themeMode}`}>
            {msg.method_traces.map((trace, idx) => (
              <details key={`${trace.method}-${idx}`} className={`chat-method-item ${themeMode}`}>
                <summary>{trace.method}</summary>
                <pre>{JSON.stringify(trace.request, null, 2)}</pre>
                <pre>{JSON.stringify(trace.response, null, 2)}</pre>
              </details>
            ))}
          </div>
        ) : null}
      </div>
    </article>
  );
}

// ── TypingBubble ────────────────────────────────────────────────

function TypingBubble({ themeMode }: { themeMode: ThemeMode }) {
  return (
    <div className="chat-bubble-row is-ai">
      <Avatar size={30} icon={<RobotOutlined />} className={`chat-avatar ai ${themeMode}`} />
      <div className="chat-bubble-wrap items-start">
        <span className={`chat-meta ${themeMode}`}>AI 助手</span>
        <div className={`chat-bubble ai ${themeMode}`}>
          <div className="flex items-center gap-1">
            <span className="chat-dot" style={{ animationDelay: '0ms' }} />
            <span className="chat-dot" style={{ animationDelay: '120ms' }} />
            <span className="chat-dot" style={{ animationDelay: '240ms' }} />
          </div>
        </div>
      </div>
    </div>
  );
}

// ── Live Streaming Area ─────────────────────────────────────────

function StreamingArea({
  thinkingText,
  streamingText,
  livePhase,
  liveToolCalls,
  themeMode,
}: {
  thinkingText: string;
  streamingText: string;
  livePhase: StreamPhase | null;
  liveToolCalls: Array<{ tool: string; call: string; args?: Record<string, unknown>; result?: Record<string, unknown>; error?: string }>;
  themeMode: ThemeMode;
}) {
  return (
    <>
      <TypingBubble themeMode={themeMode} />

      <div className={`chat-stream-status ${themeMode}`}>
        {livePhase ? `阶段：${livePhase}` : '阶段：处理中'}
      </div>

      {/* 实时思考内容 */}
      {thinkingText && (
        <div className={`chat-stream-thinking ${themeMode}`}>
          <ThinkingBubble thinking={thinkingText} streaming themeMode={themeMode} />
        </div>
      )}

      {/* 实时工具调用 */}
      {liveToolCalls.length > 0 && (
        <div className={`chat-tool-traces ${themeMode}`} style={{ marginLeft: 40 }}>
          {liveToolCalls.map((tc, idx) => (
            <details key={`live-${tc.tool}-${idx}`} className={`chat-tool-item ${themeMode}`} open>
              <summary>
                <span className="tool-icon">⚙</span>
                {tc.tool}
                <span style={{ opacity: 0.6, marginLeft: 6, fontSize: 11 }}>({tc.call})</span>
                {tc.call !== 'result' && tc.call !== 'error' && (
                  <span className="chat-thinking-streaming-dot" style={{ marginLeft: 6 }} />
                )}
              </summary>
              {tc.args && (
                <div className="tool-detail">
                  <pre>{JSON.stringify(tc.args, null, 2)}</pre>
                  {tc.result && <pre>{JSON.stringify(tc.result, null, 2)}</pre>}
                  {tc.error && <pre className="tool-error-text">{tc.error}</pre>}
                </div>
              )}
            </details>
          ))}
        </div>
      )}

      {/* 实时文本流 */}
      {streamingText && (
        <div className={`chat-stream-preview ${themeMode}`}>
          <div className="chat-md-content">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{streamingText}</ReactMarkdown>
          </div>
        </div>
      )}
    </>
  );
}

// ── SessionPanel ────────────────────────────────────────────────

function SessionPanel({ nodeId, themeMode = 'dark', onAIReply }: SessionPanelProps) {
  const [history, setHistory] = useState<ChatMessage[]>([]);
  const [nodeData, setNodeData] = useState<NodeResponse | null>(null);
  const [inputValue, setInputValue] = useState('');
  const [sessionLoading, setSessionLoading] = useState(false);

  const [isLoading, setIsLoading] = useState(false);
  const [livePhase, setLivePhase] = useState<StreamPhase | null>(null);
  const [streamingText, setStreamingText] = useState('');
  const [thinkingText, setThinkingText] = useState('');
  const [liveMethodCalls, setLiveMethodCalls] = useState<ChatStreamMethodCallEvent[]>([]);
  const [liveToolCalls, setLiveToolCalls] = useState<Array<{ tool: string; call: string; args?: Record<string, unknown>; result?: Record<string, unknown>; error?: string }>>([]);
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const streamAbortRef = useRef<AbortController | null>(null);



  // 切换节点时：加载持久化 session 历史 + 节点元数据
  useEffect(() => {
    let cancelled = false;
    setHistory([]);
    setInputValue('');
    setSessionLoading(true);
    setLivePhase(null);
    setStreamingText('');
    setThinkingText('');
    setLiveMethodCalls([]);
    setLiveToolCalls([]);


    getNode(nodeId, 'session').then((data) => {
      if (cancelled) return;
      setNodeData(data);
      const loaded: ChatMessage[] = (data.session ?? []).map((msg) => ({
        role: msg.role,
        content: msg.content,
        timestamp: msg.timestamp,
      }));
      setHistory(loaded);
    }).catch((err) => {
      console.error('Failed to fetch node session:', err);
    }).finally(() => {
      if (!cancelled) setSessionLoading(false);
    });

    return () => {
      cancelled = true;
      streamAbortRef.current?.abort();
      streamAbortRef.current = null;
    };
  }, [nodeId]);



  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [history, isLoading, streamingText, thinkingText, livePhase, liveToolCalls.length]);


  const handleSend = useCallback(async () => {
    const text = inputValue.trim();
    if (!text || isLoading) return;

    const userMsg: ChatMessage = {
      role: 'user',
      content: text,
      timestamp: new Date().toISOString(),
    };

    setHistory((prev) => [...prev, userMsg]);
    setInputValue('');
    setIsLoading(true);
    setStreamingText('');
    setThinkingText('');
    setLiveMethodCalls([]);
    setLiveToolCalls([]);
    setLivePhase('reading_node');

    streamAbortRef.current?.abort();
    const controller = new AbortController();
    streamAbortRef.current = controller;

    let assistantReply = '';
    let assistantThinking = '';
    let summary: ChatStreamSummaryEvent | null = null;

    try {

      try {
        await sendChatMessageStream(
          {
            node_id: nodeId,
            message: text,
            history,
          },
          {
            onPhase: (payload) => {
              if (payload.phase) setLivePhase(payload.phase as StreamPhase);
            },
            onMethodCall: (payload) => {
              setLiveMethodCalls((prev) => [...prev, payload]);
            },
            onToolCall: (payload) => {
              setLiveToolCalls((prev) => {
                // 更新已有的同名工具调用，或追加新的
                const existing = prev.findIndex(
                  (t) => t.tool === payload.tool && t.call === 'start',
                );
                if (existing >= 0 && (payload.call === 'result' || payload.call === 'error')) {
                  const updated = [...prev];
                  updated[existing] = {
                    tool: payload.tool,
                    call: payload.call,
                    args: (payload.args as Record<string, unknown> | undefined) ?? updated[existing].args,
                    result: payload.result as Record<string, unknown> | undefined,
                    error: payload.error,
                  };
                  return updated;
                }
                return [...prev, {
                  tool: payload.tool,
                  call: payload.call,
                  args: payload.args as Record<string, unknown> | undefined,
                }];
              });
            },
            onAssistantThinkingDelta: (delta) => {
              assistantThinking += delta;
              setThinkingText((prev) => prev + delta);
            },
            onAssistantDelta: (delta) => {
              assistantReply += delta;
              setStreamingText((prev) => prev + delta);
            },
            onAssistantDone: (reply) => {
              assistantReply = reply || assistantReply;
              setStreamingText(assistantReply);
            },
            onSummary: (payload) => {
              summary = payload;
            },
          },
          controller.signal,
        );

      } catch (streamErr) {
        if (streamErr instanceof DOMException && streamErr.name === 'AbortError') {
          return;
        }
        console.warn('流式对话失败，自动降级为非流式:', streamErr);
        const fallback = await sendChatMessage({

          node_id: nodeId,
          message: text,
          history,
        });
        assistantReply = fallback.reply;
        summary = {
          event_id: 'fallback',
          ts: new Date().toISOString(),
          turn_id: 'fallback',
          node_id: nodeId,
          usage: fallback.usage,
          memory_written: fallback.memory_written ?? null,
          method_traces: fallback.method_traces ?? null,
          model: fallback.model,
          provider: fallback.provider,
        };
      }

      // 汇总 tool_traces
      const toolTracesFromSummary = (summary?.tool_traces ?? []) as import('@/types/models').ToolTrace[];

      setHistory((prev) => [
        ...prev,
        {
          role: 'assistant',
          content: assistantReply || '（无输出）',
          thinking: assistantThinking || undefined,
          timestamp: new Date().toISOString(),
          usage: summary?.usage,
          method_traces: summary?.method_traces ?? undefined,
          tool_traces: toolTracesFromSummary.length > 0 ? toolTracesFromSummary : undefined,
        },
      ]);


      const updatedNode = await getNode(nodeId);
      setNodeData(updatedNode);
      onAIReply?.();
    } finally {
      if (streamAbortRef.current === controller) {
        streamAbortRef.current = null;
      }
      setIsLoading(false);
      setLivePhase(null);
      setStreamingText('');
      setThinkingText('');
      setLiveMethodCalls([]);
      setLiveToolCalls([]);
      setTimeout(() => inputRef.current?.focus(), 20);
    }

  }, [inputValue, isLoading, nodeId, history, onAIReply]);



  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  const isEmpty = history.length === 0 && !sessionLoading;

  return (
    <section className={`chat-panel ${themeMode}`}>
      {nodeData?.token_usage && (
        <div className={`chat-token-status ${themeMode}`}>
          <Tooltip title={`Prompt: ${nodeData.token_usage.prompt_tokens} | Completion: ${nodeData.token_usage.completion_tokens}`}>
            <span>节点累积消耗: {nodeData.token_usage.total_tokens} tokens</span>
          </Tooltip>
        </div>
      )}
      <div className="chat-messages" aria-live="polite">

        {sessionLoading ? (
          <div className="chat-empty-state">
            <Spin size="large" />
          </div>
        ) : isEmpty && !isLoading ? (
          <div className="chat-empty-state">
            <RobotOutlined className="text-5xl mb-4 opacity-60" aria-hidden="true" />
            <p className="text-base font-semibold">有什么可以帮你的？</p>
            <p className="text-xs mt-1">当前节点：{nodeId}</p>
          </div>
        ) : (
          <>
            {history.map((msg, idx) => (
              <MessageBubble key={`${nodeId}-${idx}`} msg={msg} themeMode={themeMode} />
            ))}
            {isLoading ? (
              <StreamingArea
                thinkingText={thinkingText}
                streamingText={streamingText}
                livePhase={livePhase}
                liveToolCalls={liveToolCalls}
                themeMode={themeMode}
              />
            ) : null}
          </>
        )}

        <div ref={messagesEndRef} />
      </div>

      <div className={`chat-input-bar ${themeMode}`}>
        <div className="chat-input-inner">
          <Input.TextArea
            ref={inputRef}
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="输入消息，Enter 发送，Shift+Enter 换行…"
            autoSize={{ minRows: 1, maxRows: 6 }}
            disabled={isLoading}
            name="workspace-chat-input"
            aria-label="对话输入框"
            className={`chat-textarea ${themeMode}`}
          />
          <Button
            type="primary"
            icon={<SendOutlined />}
            aria-label="发送消息"
            onClick={handleSend}
            disabled={!inputValue.trim()}
            loading={isLoading}
            className="chat-send-btn"
          >
            发送
          </Button>
        </div>
        <p className="chat-tip">Enter 发送 · Shift+Enter 换行</p>
      </div>

      <style>{`
        .chat-panel {
          height: 100%;
          display: flex;
          flex-direction: column;
          position: relative;
        }

        .chat-token-status {
          position: absolute;
          top: 10px;
          right: 20px;
          font-size: 11px;
          padding: 2px 8px;
          border-radius: 4px;
          z-index: 10;
          opacity: 0.7;
          pointer-events: auto;
        }

        .chat-token-status.dark {
          background: rgba(0, 0, 0, 0.3);
          color: #94a3b8;
        }

        .chat-token-status.light {
          background: rgba(255, 255, 255, 0.5);
          color: #64748b;
        }


        .chat-panel.dark {
          background: linear-gradient(180deg, rgba(10,16,30,.72), rgba(10,16,30,.5));
          color: #eaf2ff;
        }

        .chat-panel.light {
          background: linear-gradient(180deg, rgba(255,255,255,.55), rgba(248,250,255,.78));
          color: #1f2937;
        }

        .chat-messages {
          flex: 1;
          min-height: 0;
          overflow-y: auto;
          padding: 16px 22px;
          content-visibility: auto;
        }

        .chat-empty-state {
          height: 100%;
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          opacity: .72;
          text-align: center;
        }

        .chat-bubble-row {
          display: flex;
          gap: 10px;
          margin-bottom: 18px;
        }

        .chat-bubble-row.is-user { flex-direction: row-reverse; }
        .chat-bubble-row.is-ai { flex-direction: row; }

        .chat-avatar.user.dark { background: #2563eb; }
        .chat-avatar.user.light { background: #1d4ed8; }
        .chat-avatar.ai.dark { background: #475569; }
        .chat-avatar.ai.light { background: #64748b; }

        .chat-bubble-wrap {
          display: flex;
          flex-direction: column;
          max-width: 76%;
        }

        .chat-meta {
          font-size: 11px;
          margin: 0 6px 4px;
          opacity: .8;
        }

        .chat-meta.dark { color: #bfd2ff; }
        .chat-meta.light { color: #475569; }

        .chat-bubble {
          position: relative;
          padding: 10px 14px;
          border-radius: 14px;
          line-height: 1.56;
          font-size: 13px;
          word-break: break-word;
          transition: transform .08s ease, background-color .12s ease;
        }

        .chat-bubble.user.dark {
          background: linear-gradient(135deg, #2563eb, #3b82f6);
          color: #ffffff;
          border-top-right-radius: 4px;
        }
        .chat-bubble.user.light {
          background: linear-gradient(135deg, #2563eb, #3b82f6);
          color: #ffffff;
          border-top-right-radius: 4px;
        }

        .chat-bubble.ai.dark {
          background: rgba(15, 23, 42, .92);
          border: 1px solid rgba(96, 165, 250, .36);
          color: #e2ecff;
          border-top-left-radius: 4px;
        }

        .chat-bubble.ai.light {
          background: rgba(255,255,255,.92);
          border: 1px solid rgba(30, 64, 175, .14);
          color: #1f2937;
          border-top-left-radius: 4px;
        }

        /* ── Markdown 内容样式 ── */
        .chat-md-content { line-height: 1.65; }
        .chat-md-content p { margin: 0 0 6px; }
        .chat-md-content p:last-child { margin-bottom: 0; }
        .chat-md-content h1,.chat-md-content h2,.chat-md-content h3 { font-weight: 700; margin: 10px 0 4px; }
        .chat-md-content ul,.chat-md-content ol { padding-left: 18px; margin: 4px 0; }
        .chat-md-content li { margin-bottom: 2px; }
        .chat-md-content code { padding: 1px 5px; border-radius: 4px; font-size: 12px; font-family: monospace; }
        .chat-bubble.ai.dark .chat-md-content code { background: rgba(96,165,250,.18); color: #93c5fd; }
        .chat-bubble.ai.light .chat-md-content code { background: rgba(30,64,175,.1); color: #1d4ed8; }
        .chat-md-content pre { border-radius: 8px; padding: 10px 12px; overflow-x: auto; margin: 8px 0; font-size: 12px; }
        .chat-bubble.ai.dark .chat-md-content pre { background: rgba(7,13,24,.88); border: 1px solid rgba(96,165,250,.2); }
        .chat-bubble.ai.light .chat-md-content pre { background: rgba(240,245,255,.9); border: 1px solid rgba(30,64,175,.12); }
        .chat-md-content blockquote { border-left: 3px solid; padding: 4px 10px; margin: 6px 0; opacity: .85; }
        .chat-bubble.ai.dark .chat-md-content blockquote { border-color: rgba(96,165,250,.5); background: rgba(96,165,250,.06); }
        .chat-bubble.ai.light .chat-md-content blockquote { border-color: rgba(30,64,175,.4); background: rgba(30,64,175,.04); }
        .chat-md-content table { border-collapse: collapse; width: 100%; margin: 8px 0; }
        .chat-md-content th,.chat-md-content td { padding: 4px 8px; border: 1px solid; text-align: left; }
        .chat-bubble.ai.dark .chat-md-content th,.chat-bubble.ai.dark .chat-md-content td { border-color: rgba(96,165,250,.25); }
        .chat-bubble.ai.dark .chat-md-content th { background: rgba(96,165,250,.12); }
        .chat-bubble.ai.light .chat-md-content th,.chat-bubble.ai.light .chat-md-content td { border-color: rgba(30,64,175,.16); }
        .chat-bubble.ai.light .chat-md-content th { background: rgba(30,64,175,.06); }
        .chat-md-content a { text-decoration: underline; opacity: .9; }
        .chat-bubble.ai.dark .chat-md-content a { color: #93c5fd; }
        .chat-bubble.ai.light .chat-md-content a { color: #1d4ed8; }

        /* ── Thinking 区域 ── */
        .chat-thinking {
          margin-bottom: 6px;
          border-radius: 10px;
          border: 1px solid;
          font-size: 12px;
          overflow: hidden;
        }
        .chat-thinking.dark {
          border-color: rgba(234,179,8,.3);
          background: rgba(254,252,232,.04);
          color: #fef9c3;
        }
        .chat-thinking.light {
          border-color: rgba(202,138,4,.25);
          background: rgba(254,252,232,.6);
          color: #713f12;
        }
        .chat-thinking-summary {
          padding: 5px 10px;
          cursor: pointer;
          display: flex;
          align-items: center;
          font-weight: 600;
          list-style: none;
          user-select: none;
        }
        .chat-thinking-summary::-webkit-details-marker { display: none; }
        .chat-thinking-content {
          padding: 6px 12px 8px;
          border-top: 1px solid;
          opacity: .88;
          max-height: 320px;
          overflow-y: auto;
          line-height: 1.55;
        }
        .chat-thinking.dark .chat-thinking-content { border-top-color: rgba(234,179,8,.2); }
        .chat-thinking.light .chat-thinking-content { border-top-color: rgba(202,138,4,.18); }
        .chat-thinking-content p { margin: 0 0 4px; }
        .chat-thinking-content p:last-child { margin-bottom: 0; }

        .chat-thinking-streaming-dot {
          display: inline-block;
          width: 7px;
          height: 7px;
          border-radius: 50%;
          background: currentColor;
          margin-left: 6px;
          opacity: .7;
          animation: chatBounce .8s infinite ease-in-out;
        }

        /* 流式 thinking 区域 */
        .chat-stream-thinking {
          margin: 0 0 6px 40px;
        }

        /* ── Tool Traces ── */
        .chat-tool-traces {
          margin-top: 6px;
          display: flex;
          flex-direction: column;
          gap: 4px;
        }
        .chat-tool-item {
          border: 1px solid;
          border-radius: 8px;
          font-size: 12px;
          overflow: hidden;
        }
        .chat-tool-item.dark {
          border-color: rgba(52,211,153,.28);
          background: rgba(6,22,14,.56);
          color: #a7f3d0;
        }
        .chat-tool-item.light {
          border-color: rgba(5,150,105,.2);
          background: rgba(236,253,245,.7);
          color: #065f46;
        }
        .chat-tool-item.error.dark {
          border-color: rgba(248,113,113,.3);
          background: rgba(30,5,5,.56);
          color: #fca5a5;
        }
        .chat-tool-item.error.light {
          border-color: rgba(220,38,38,.2);
          background: rgba(254,242,242,.7);
          color: #991b1b;
        }
        .chat-tool-item summary {
          padding: 5px 10px;
          cursor: pointer;
          display: flex;
          align-items: center;
          gap: 5px;
          font-weight: 600;
          list-style: none;
          user-select: none;
        }
        .chat-tool-item summary::-webkit-details-marker { display: none; }
        .tool-icon { font-style: normal; }
        .tool-error-badge {
          margin-left: auto;
          font-size: 10px;
          padding: 1px 5px;
          border-radius: 4px;
          background: rgba(220,38,38,.2);
        }
        .tool-detail {
          padding: 4px 10px 8px;
          border-top: 1px solid rgba(255,255,255,.08);
        }
        .tool-section-label {
          font-size: 10px;
          opacity: .6;
          margin: 4px 0 2px;
          text-transform: uppercase;
          letter-spacing: .05em;
        }
        .tool-detail pre {
          margin: 0;
          white-space: pre-wrap;
          word-break: break-word;
          line-height: 1.45;
          font-size: 11px;
        }
        .tool-error-text { color: #f87171; }

        .chat-stream-status {
          margin: 2px 0 6px 40px;
          font-size: 12px;
          opacity: .82;
        }

        .chat-stream-preview {
          margin: 0 0 8px 40px;
          border-radius: 10px;
          border: 1px solid;
          padding: 8px 10px;
          font-size: 12px;
          line-height: 1.5;
        }

        .chat-stream-preview.dark {
          border-color: rgba(96, 165, 250, .28);
          background: rgba(15, 23, 42, .56);
          color: #e2ecff;
        }

        .chat-stream-preview.light {
          border-color: rgba(30, 64, 175, .2);
          background: rgba(255, 255, 255, .74);
          color: #1f2937;
        }

        .chat-method-traces {
          margin-top: 6px;
          display: flex;
          flex-direction: column;
          gap: 6px;
        }

        .chat-method-item {
          border: 1px solid;
          border-radius: 10px;
          padding: 6px 8px;
          font-size: 12px;
        }

        .chat-method-item.dark {
          border-color: rgba(96, 165, 250, .28);
          background: rgba(15, 23, 42, .56);
        }

        .chat-method-item.light {
          border-color: rgba(30, 64, 175, .2);
          background: rgba(255, 255, 255, .74);
        }

        .chat-method-item summary {
          cursor: pointer;
          font-weight: 600;
        }

        .chat-method-item pre {
          margin: 8px 0 0;
          white-space: pre-wrap;
          word-break: break-word;
          line-height: 1.45;
        }

        .chat-copy-btn {
          position: absolute;
          top: -8px;
          right: -8px;
          width: 22px;
          height: 22px;
          border: none;
          border-radius: 9999px;
          display: inline-flex;
          align-items: center;
          justify-content: center;
          cursor: pointer;
          opacity: 0;
          transition: opacity .08s ease, transform .06s ease;
        }

        .chat-bubble:hover .chat-copy-btn { opacity: 1; }
        .chat-copy-btn:active { transform: scale(.95); }
        .chat-copy-btn.dark { background: rgba(96,165,250,.24); color: #eaf2ff; }
        .chat-copy-btn.light { background: rgba(30,64,175,.14); color: #1e3a8a; }

        .chat-dot {
          width: 8px;
          height: 8px;
          border-radius: 9999px;
          background: #94a3b8;
          animation: chatBounce .8s infinite ease-in-out;
        }

        @keyframes chatBounce {
          0%, 80%, 100% { transform: translateY(0); opacity: .6; }
          40% { transform: translateY(-3px); opacity: 1; }
        }

        .chat-input-bar {
          border-top: 1px solid;
          padding: 10px 12px 8px;
          backdrop-filter: blur(10px);
        }

        .chat-input-bar.dark {
          border-color: rgba(59, 130, 246, .2);
          background: rgba(7, 13, 24, .72);
        }

        .chat-input-bar.light {
          border-color: rgba(30, 64, 175, .12);
          background: rgba(255,255,255,.82);
        }

        .chat-input-inner {
          max-width: 980px;
          margin: 0 auto;
          display: flex;
          align-items: flex-end;
          gap: 8px;
        }

        .chat-textarea {
          border-radius: 12px !important;
          font-size: 13px;
        }

        .chat-textarea.dark {
          background: rgba(15, 23, 42, .92) !important;
          border-color: rgba(96,165,250,.38) !important;
          color: #eef4ff !important;
        }

        .chat-textarea.light {
          background: rgba(255,255,255,.96) !important;
          border-color: rgba(30,64,175,.2) !important;
          color: #111827 !important;
        }

        .chat-send-btn {
          height: 36px;
          border-radius: 10px !important;
          transition: transform .06s ease, filter .08s ease !important;
        }

        .chat-send-btn:active {
          transform: scale(.95);
          filter: brightness(.96);
        }

        .chat-tip {
          text-align: center;
          font-size: 11px;
          opacity: .65;
          margin-top: 6px;
        }
      `}</style>
    </section>
  );
}

export default SessionPanel;
