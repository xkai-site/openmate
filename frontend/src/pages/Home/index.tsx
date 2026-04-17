import React, { useState, useRef, useEffect, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, App } from 'antd';
import { SendOutlined, BranchesOutlined, LoadingOutlined, BulbOutlined, ExperimentOutlined, ThunderboltOutlined } from '@ant-design/icons';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { sendChatMessage, sendChatMessageStream } from '@/services/api/chat';
import { decomposeNode } from '@/services/api/tree';
import type {
  ChatStreamMethodCallEvent,
  ChatStreamSummaryEvent,
  SessionMessage,
  MethodTrace,
  StreamPhase,
} from '@/types/models';
import ProjectPanel from './components/ProjectPanel';

interface ChatBubble {
  role: 'user' | 'assistant';
  content: string;
  method_traces?: MethodTrace[];
}

// 功能标签配置
const FEATURE_TAGS = [
  { icon: <BulbOutlined />, label: 'Deep Search', color: '#0071e3' },
  { icon: <ExperimentOutlined />, label: 'Think', color: '#bf5af2' },
  { icon: <ThunderboltOutlined />, label: 'Fast', color: '#34c759' },
];

export default function HomePage() {
  const [messages, setMessages] = useState<ChatBubble[]>([
    {
      role: 'assistant',
      content: '你好！我是 AITree，你的智能协作伙伴。\n\n你可以先跟我聊聊你的想法或项目背景，当你觉得想法足够清晰时，点击「生成任务树」，我会将我们的对话转化为一棵可执行的任务树。',
    },
  ]);
  const [input, setInput] = useState('');
  const [nodeId, setNodeId] = useState<string | null>(null);
  const [isSending, setIsSending] = useState(false);
  const [isDecomposing, setIsDecomposing] = useState(false);
  const [livePhase, setLivePhase] = useState<StreamPhase | null>(null);
  const [streamingText, setStreamingText] = useState('');
  const [liveMethodCalls, setLiveMethodCalls] = useState<ChatStreamMethodCallEvent[]>([]);
  const [projectPanelKey, setProjectPanelKey] = useState(0);

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const streamAbortRef = useRef<AbortController | null>(null);

  const navigate = useNavigate();
  const { message } = App.useApp();

  // 自动滚动到底部
  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [messages, streamingText, livePhase, liveMethodCalls.length, scrollToBottom]);

  useEffect(() => {
    return () => {
      streamAbortRef.current?.abort();
      streamAbortRef.current = null;
    };
  }, []);

  // 自动调整输入框高度
  useEffect(() => {
    const textarea = inputRef.current;
    if (!textarea) return;

    const minHeight = 45; // 单行高度
    const maxHeight = 135; // 三倍高度

    const adjustHeight = () => {
      textarea.style.height = 'auto';
      const scrollHeight = textarea.scrollHeight;
      textarea.style.height = `${Math.min(Math.max(scrollHeight, minHeight), maxHeight)}px`;
    };

    textarea.addEventListener('input', adjustHeight);
    adjustHeight(); // 初始化

    return () => {
      textarea.removeEventListener('input', adjustHeight);
    };
  }, [input]);

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || isSending) return;

    setInput('');
    setMessages((prev) => [...prev, { role: 'user', content: text }]);
    setIsSending(true);
    setLivePhase('reading_node');
    setStreamingText('');
    setLiveMethodCalls([]);

    try {
      const history: SessionMessage[] = messages
        .slice(1)
        .map((m) => ({ role: m.role, content: m.content }));

      streamAbortRef.current?.abort();
      const controller = new AbortController();
      streamAbortRef.current = controller;

      let assistantReply = '';
      let summary: ChatStreamSummaryEvent | null = null;

      try {
        await sendChatMessageStream(
          {
            node_id: nodeId ?? undefined,
            message: text,
            history,
            save_session: true,
          },
          {
            onPhase: (payload) => {
              if (payload.phase) setLivePhase(payload.phase);
            },
            onMethodCall: (payload) => {
              setLiveMethodCalls((prev) => [...prev, payload]);
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
              if (payload.node_id && !nodeId) {
                setNodeId(payload.node_id);
              }
              setProjectPanelKey((k) => k + 1);
            },
          },
          controller.signal,
        );
      } catch (streamErr) {
        if (streamErr instanceof DOMException && streamErr.name === 'AbortError') {
          return;
        }
        const shouldFallback = streamErr instanceof TypeError;
        if (!shouldFallback) {
          throw streamErr;
        }
        console.warn('流式对话网络失败，自动降级为非流式:', streamErr);

        const fallback = await sendChatMessage({
          node_id: nodeId,
          message: text,
          history,
          save_session: true,
        });
        assistantReply = fallback.reply;
        summary = {
          event_id: 'fallback',
          ts: new Date().toISOString(),
          turn_id: 'fallback',
          node_id: fallback.node_id ?? nodeId ?? undefined,
          usage: fallback.usage,
          memory_written: fallback.memory_written ?? null,
          method_traces: fallback.method_traces ?? null,
          model: fallback.model,
          provider: fallback.provider,
        };
        if (fallback.node_id && !nodeId) {
          setNodeId(fallback.node_id);
        }
        setProjectPanelKey((k) => k + 1);
      }

      setMessages((prev) => [
        ...prev,
        {
          role: 'assistant',
          content: assistantReply || '（无输出）',
          method_traces: summary?.method_traces ?? undefined,
        },
      ]);
    } catch (err) {
      console.error('发送消息失败:', err);
      message.error('发送失败，请重试');
      setMessages((prev) => prev.slice(0, -1));
      setInput(text);
    } finally {
      streamAbortRef.current = null;
      setIsSending(false);
      setLivePhase(null);
      setStreamingText('');
      setLiveMethodCalls([]);
    }
  }, [input, isSending, messages, nodeId, message]);

  const handleDecompose = useCallback(async () => {
    if (isDecomposing) return;

    if (!nodeId) {
      message.info('请先和我聊聊你的想法，再生成任务树');
      inputRef.current?.focus();
      return;
    }

    setIsDecomposing(true);
    const loadingMsg = message.loading('AI 正在分析对话内容并生成任务树…', 0);

    try {
      const result = await decomposeNode(nodeId);
      loadingMsg();
      message.success(`任务树生成成功，包含 ${result.nodes.length} 个子节点`);
      navigate(`/workspace/${nodeId}`);
    } catch (err) {
      loadingMsg();
      console.error('生成任务树失败:', err);
      message.error(err instanceof Error ? `生成失败: ${err.message}` : '生成失败，请重试');
    } finally {
      setIsDecomposing(false);
    }
  }, [isDecomposing, nodeId, message, navigate]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      void handleSend();
    }
  }, [handleSend]);

  const hasConversation = messages.length > 1;

  // 问候语和功能标签 - 仅在无对话时显示
  const showWelcome = !hasConversation;

  return (
    <div className="home-root">
      {/* 左侧项目管理面板 */}
      <ProjectPanel key={projectPanelKey} />

      {/* 右侧主内容区 */}
      <div className="home-main">
        {/* 顶部导航栏 */}
        <header className="home-header">
          <div className="home-logo">
            <span className="home-logo-text">AITree</span>
          </div>
          {hasConversation && (
            <Button
              type="primary"
              icon={isDecomposing ? <LoadingOutlined /> : <BranchesOutlined />}
              onClick={handleDecompose}
              disabled={isDecomposing || isSending}
              className="home-decompose-btn"
            >
              生成任务树
            </Button>
          )}
        </header>

        {/* 居中内容容器 */}
        <main className="home-content">
          {/* 装饰背景 */}
          <div className="home-bg-pattern" />
          <div className="home-particles">
            <div className="home-particle" />
            <div className="home-particle" />
            <div className="home-particle" />
            <div className="home-particle" />
            <div className="home-particle" />
          </div>
          
          <div className="home-container">
            <div className="home-container-inner">
            {/* 欢迎区域 - 仅无对话时显示 */}
            {showWelcome && (
              <div className="home-welcome">
                <div className="home-logo-large">
                  <div className="home-logo-glow" />
                  <img 
                    src="/images/logo.png" 
                    alt="AITree Logo" 
                    className="home-logo-img" 
                  />
                </div>
                <h1 className="home-title">How can I help today?</h1>
                <p className="home-subtitle">Your AI companion for planning and execution</p>
                
                {/* 功能标签 */}
                <div className="home-tags">
                  {FEATURE_TAGS.map((tag) => (
                    <button key={tag.label} className="home-tag" aria-label={tag.label}>
                      <span className="home-tag-icon" style={{ color: tag.color }}>
                        {tag.icon}
                      </span>
                      <span className="home-tag-label">{tag.label}</span>
                    </button>
                  ))}
                </div>
              </div>
            )}

            {/* 对话区域 */}
            <div className="home-chat-area">
              {/* 对话消息列表 */}
              <div className="home-messages">
                {messages.map((msg, idx) => (
                  <React.Fragment key={idx}>
                    <div
                      className={`home-bubble home-bubble--${msg.role}`}
                    >
                      {msg.role === 'assistant' && (
                        <img 
                          src="/images/logo.png" 
                          alt="AI" 
                          className="home-bubble-avatar-img" 
                        />
                      )}
                      <div className="home-bubble-content">
                        {msg.role === 'assistant' ? (
                          <div className="home-md">
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>
                              {msg.content}
                            </ReactMarkdown>
                          </div>
                        ) : (
                          msg.content.split('\n').map((line, i) => (
                            <React.Fragment key={i}>
                              {line}
                              {i < msg.content.split('\n').length - 1 && <br />}
                            </React.Fragment>
                          ))
                        )}
                      </div>
                      {msg.role === 'user' && (
                        <div className="home-bubble-avatar home-bubble-avatar--user">你</div>
                      )}
                    </div>
                    {msg.role === 'assistant' && msg.method_traces && msg.method_traces.length > 0 && (
                      <div className="home-method-traces">
                        {msg.method_traces.map((trace: MethodTrace, traceIdx: number) => (
                          <details key={traceIdx} className="home-method-item">
                            <summary>{trace.method}</summary>
                            <pre>{JSON.stringify(trace.request, null, 2)}</pre>
                            <pre>{JSON.stringify(trace.response, null, 2)}</pre>
                          </details>
                        ))}
                      </div>
                    )}
                  </React.Fragment>
                ))}

                {isSending && (
                  <div className="home-bubble home-bubble--assistant">
                    <img 
                      src="/images/logo.png" 
                      alt="AI" 
                      className="home-bubble-avatar-img" 
                    />
                    <div className="home-bubble-content">
                      {streamingText ? (
                        <div className="home-md">
                          <ReactMarkdown remarkPlugins={[remarkGfm]}>
                            {streamingText}
                          </ReactMarkdown>
                        </div>
                      ) : (
                        <div className="home-typing">
                          <span /><span /><span />
                        </div>
                      )}
                    </div>
                  </div>
                )}
                
                {isSending && liveMethodCalls.length > 0 && (
                  <div className="home-method-traces">
                    {liveMethodCalls.map((trace, traceIdx) => (
                      <details key={`live-${traceIdx}`} className="home-method-item" open>
                        <summary>{trace.method} · {trace.call} · #{trace.attempt ?? 1}</summary>
                        <pre>{JSON.stringify(trace.request, null, 2)}</pre>
                        {trace.response ? <pre>{JSON.stringify(trace.response, null, 2)}</pre> : null}
                        {trace.reason ? <pre>{trace.reason}</pre> : null}
                        {trace.error ? <pre>{trace.error}</pre> : null}
                      </details>
                    ))}
                  </div>
                )}

                <div ref={messagesEndRef} />
              </div>
            </div>
            </div>
          </div>
          
          {/* 输入区域 - 固定在底部 */}
          <div className="home-input-section">
            <div className="home-input-container">
              <div className="home-input-wrap">
                <textarea
                  ref={inputRef}
                  value={input}
                  onChange={(e) => setInput(e.target.value)}
                  onKeyDown={handleKeyDown}
                  placeholder="Message AITree…"
                  rows={1}
                  className="home-input"
                  disabled={isSending || isDecomposing}
                  aria-label="输入消息"
                />
                <button
                  onClick={() => void handleSend()}
                  disabled={!input.trim() || isSending || isDecomposing}
                  className="home-send-btn"
                  aria-label="发送消息"
                >
                  <SendOutlined />
                </button>
              </div>
              
              {/* 底部提示 */}
              <div className="home-footer-hints">
                {showWelcome ? (
                  <>
                    <span className="home-hint-text">Try asking about:</span>
                    <button
                      className="home-hint-tag"
                      onClick={() => setInput('我想做一个支持暗黑模式的个人博客系统')}
                    >
                      博客系统
                    </button>
                    <button
                      className="home-hint-tag"
                      onClick={() => setInput('帮我规划一个电商网站项目')}
                    >
                      电商网站
                    </button>
                    <button
                      className="home-hint-tag"
                      onClick={() => setInput('做一个公司内部知识库')}
                    >
                      知识库
                    </button>
                  </>
                ) : (
                  <>
                    <span className="home-trust-badge">
                      <span className="home-trust-dot" />
                      Available 24/7
                    </span>
                    <span className="home-trust-badge">
                      <span className="home-trust-dot home-trust-dot--green" />
                      Securely Encrypted
                    </span>
                  </>
                )}
              </div>
            </div>
          </div>
        </main>
      </div>

      <style>{`
        /* CSS Variables */
        :root {
          --home-bg-primary: #ffffff;
          --home-bg-secondary: #f5f5f7;
          --home-bg-tertiary: #fafafa;
          --home-text-primary: #0a0a0a;
          --home-text-secondary: #86868b;
          --home-text-tertiary: #1d1d1f;
          --home-border: rgba(0, 0, 0, 0.06);
          --home-shadow: 0 4px 24px rgba(0, 0, 0, 0.08);
          --home-shadow-hover: 0 8px 32px rgba(0, 0, 0, 0.12);
          --home-shadow-glow: 0 0 0 4px rgba(0, 113, 227, 0.15);
          --home-accent: #0071e3;
          --home-radius-lg: 24px;
          --home-radius-full: 9999px;
        }

        /* Reduced motion */
        @media (prefers-reduced-motion: reduce) {
          .home-bubble,
          .home-tag,
          .home-hint-tag,
          .home-decompose-btn,
          .home-send-btn,
          .home-input-wrap,
          .home-logo-icon,
          .home-logo-icon-large,
          .home-logo-glow {
            animation: none !important;
            transition: none !important;
          }
        }

        .home-root {
          height: 100vh;
          display: flex;
          overflow: hidden;
          background: var(--home-bg-secondary);
          color: var(--home-text-primary);
          font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
        }

        /* Main area */
        .home-main {
          flex: 1;
          min-width: 0;
          display: flex;
          flex-direction: column;
          overflow: hidden;
          background: var(--home-bg-primary);
          position: relative;
        }

        /* Subtle gradient overlay on main */
        .home-main::before {
          content: '';
          position: absolute;
          top: 0;
          left: 0;
          right: 0;
          height: 300px;
          background: radial-gradient(ellipse at 50% 0%, rgba(0, 113, 227, 0.03) 0%, transparent 70%);
          pointer-events: none;
          z-index: 0;
        }

        /* Header */
        .home-header {
          height: 56px;
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 0 24px;
          border-bottom: 1px solid var(--home-border);
          background: var(--home-bg-primary);
          flex-shrink: 0;
          position: relative;
          z-index: 1;
        }

        .home-logo {
          display: flex;
          align-items: center;
          gap: 10px;
        }

        .home-logo-icon {
          width: 32px;
          height: 32px;
          border-radius: 50%;
          background: linear-gradient(135deg, #1d1d1f, #3a3a3c);
          display: flex;
          align-items: center;
          justify-content: center;
          font-weight: 700;
          font-style: italic;
          font-size: 16px;
          color: white;
          box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
          animation: logoFloat 3s ease-in-out infinite;
        }

        .home-logo-icon-img {
          width: 32px;
          height: 32px;
          border-radius: 50%;
          object-fit: cover;
          animation: logoFloat 3s ease-in-out infinite;
        }

        @keyframes logoFloat {
          0%, 100% { transform: translateY(0); }
          50% { transform: translateY(-2px); }
        }

        .home-logo-text {
          font-size: 18px;
          font-weight: 700;
          color: var(--home-text-primary);
          letter-spacing: -0.01em;
        }

        .home-decompose-btn {
          background: var(--home-text-primary) !important;
          border: none !important;
          font-weight: 500 !important;
          border-radius: var(--home-radius-full) !important;
          padding: 8px 20px !important;
          height: auto !important;
          transition: transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1), 
                      box-shadow 0.2s ease, 
                      opacity 0.15s ease !important;
        }
        .home-decompose-btn:not(:disabled):hover {
          transform: scale(1.03) translateY(-1px);
          box-shadow: 0 6px 16px rgba(0, 0, 0, 0.18);
        }
        .home-decompose-btn:not(:disabled):active {
          transform: scale(0.97);
        }
        .home-decompose-btn:focus-visible {
          outline: 2px solid var(--home-accent);
          outline-offset: 2px;
        }

        /* Content area */
        .home-content {
          flex: 1;
          min-height: 0;
          display: flex;
          flex-direction: column;
          position: relative;
          z-index: 1;
          overflow: hidden;
        }

        .home-container {
          flex: 1;
          min-height: 0;
          overflow-y: auto;
          padding: 0 24px;
        }

        .home-container-inner {
          width: 100%;
          max-width: 720px;
          margin: 0 auto;
          display: flex;
          flex-direction: column;
          padding: 40px 0;
        }

        /* Welcome section */
        .home-welcome {
          display: flex;
          flex-direction: column;
          align-items: center;
          text-align: center;
          padding: 40px 0 32px;
          animation: fadeInUp 0.6s cubic-bezier(0.16, 1, 0.3, 1);
        }

        @keyframes fadeInUp {
          from {
            opacity: 0;
            transform: translateY(20px);
          }
          to {
            opacity: 1;
            transform: translateY(0);
          }
        }

        .home-logo-large {
          margin-bottom: 24px;
          position: relative;
        }

        /* Logo glow effect */
        .home-logo-glow {
          position: absolute;
          top: 50%;
          left: 50%;
          transform: translate(-50%, -50%);
          width: 80px;
          height: 80px;
          background: radial-gradient(circle, rgba(0, 113, 227, 0.2) 0%, transparent 70%);
          border-radius: 50%;
          animation: logoGlow 2s ease-in-out infinite;
          z-index: -1;
        }

        @keyframes logoGlow {
          0%, 100% { opacity: 0.6; transform: translate(-50%, -50%) scale(1); }
          50% { opacity: 1; transform: translate(-50%, -50%) scale(1.1); }
        }

        .home-logo-icon-large {
          width: 56px;
          height: 56px;
          border-radius: 50%;
          background: linear-gradient(135deg, #1d1d1f, #3a3a3c);
          display: flex;
          align-items: center;
          justify-content: center;
          font-weight: 700;
          font-style: italic;
          font-size: 28px;
          color: white;
          box-shadow: 0 8px 24px rgba(0, 0, 0, 0.15);
          animation: logoFloat 3s ease-in-out infinite;
        }

        .home-logo-img {
          width: 56px;
          height: 56px;
          border-radius: 50%;
          object-fit: cover;
          animation: logoFloat 3s ease-in-out infinite;
        }

        .home-title {
          font-size: 32px;
          font-weight: 700;
          color: var(--home-text-primary);
          margin: 0 0 8px;
          letter-spacing: -0.02em;
          text-wrap: balance;
          animation: fadeInUp 0.6s cubic-bezier(0.16, 1, 0.3, 1) 0.1s both;
        }

        .home-subtitle {
          font-size: 15px;
          color: var(--home-text-secondary);
          margin: 0 0 32px;
          font-weight: 400;
          animation: fadeInUp 0.6s cubic-bezier(0.16, 1, 0.3, 1) 0.15s both;
        }

        /* Feature tags */
        .home-tags {
          display: flex;
          gap: 12px;
          flex-wrap: wrap;
          justify-content: center;
        }

        .home-tag {
          display: flex;
          align-items: center;
          gap: 8px;
          padding: 10px 18px;
          border: 1px solid var(--home-border);
          border-radius: var(--home-radius-full);
          background: var(--home-bg-primary);
          cursor: pointer;
          transition: all 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
          font-size: 14px;
          color: var(--home-text-tertiary);
          animation: fadeInUp 0.6s cubic-bezier(0.16, 1, 0.3, 1) 0.2s both;
        }
        .home-tag:nth-child(2) { animation-delay: 0.25s; }
        .home-tag:nth-child(3) { animation-delay: 0.3s; }
        
        .home-tag:hover {
          background: var(--home-bg-secondary);
          border-color: rgba(0, 0, 0, 0.12);
          transform: translateY(-2px);
          box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
        }
        
        .home-tag:hover .home-tag-icon {
          transform: scale(1.1);
        }
        
        .home-tag:active {
          transform: translateY(0) scale(0.98);
        }
        
        .home-tag:focus-visible {
          outline: 2px solid var(--home-accent);
          outline-offset: 2px;
        }

        .home-tag-icon {
          font-size: 14px;
          display: flex;
          align-items: center;
          transition: transform 0.2s ease;
        }

        .home-tag-label {
          font-weight: 500;
        }

        /* Chat area */
        .home-chat-area {
          flex: 1;
          min-height: 0;
          display: flex;
          flex-direction: column;
        }

        .home-messages {
          display: flex;
          flex-direction: column;
          gap: 20px;
          padding: 24px 0;
        }

        .home-bubble {
          display: flex;
          align-items: flex-start;
          gap: 12px;
          animation: bubbleIn 0.35s cubic-bezier(0.16, 1, 0.3, 1);
        }

        @keyframes bubbleIn {
          from { 
            opacity: 0; 
            transform: translateY(12px) scale(0.98); 
          }
          to { 
            opacity: 1; 
            transform: translateY(0) scale(1); 
          }
        }

        .home-bubble--user {
          flex-direction: row-reverse;
        }

        .home-bubble-avatar {
          width: 32px;
          height: 32px;
          border-radius: 50%;
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 12px;
          font-weight: 600;
          flex-shrink: 0;
          background: linear-gradient(135deg, #1d1d1f, #3a3a3c);
          color: white;
          animation: avatarIn 0.3s cubic-bezier(0.34, 1.56, 0.64, 1) 0.1s both;
        }

        @keyframes avatarIn {
          from { 
            opacity: 0; 
            transform: scale(0.5); 
          }
          to { 
            opacity: 1; 
            transform: scale(1); 
          }
        }

        .home-bubble-avatar--user {
          background: linear-gradient(135deg, #86868b, #a1a1a6);
        }

        .home-bubble-avatar-img {
          width: 32px;
          height: 32px;
          border-radius: 50%;
          object-fit: cover;
          animation: avatarIn 0.3s cubic-bezier(0.34, 1.56, 0.64, 1) 0.1s both;
        }

        .home-bubble-content {
          max-width: calc(100% - 80px);
          padding: 12px 16px;
          border-radius: 18px;
          font-size: 15px;
          line-height: 1.6;
          word-break: break-word;
          transition: background-color 0.2s ease;
        }

        .home-bubble--assistant .home-bubble-content {
          background: var(--home-bg-secondary);
          color: var(--home-text-primary);
          border-top-left-radius: 6px;
        }

        .home-bubble--user .home-bubble-content {
          background: var(--home-bg-tertiary);
          color: var(--home-text-primary);
          text-align: right;
          border-top-right-radius: 6px;
        }

        /* Streaming text animation */
        .home-bubble--assistant .home-bubble-content:has(~ .home-typing),
        .home-bubble-content.is-streaming {
          background: linear-gradient(
            90deg,
            var(--home-bg-secondary) 0%,
            rgba(0, 113, 227, 0.05) 50%,
            var(--home-bg-secondary) 100%
          );
          background-size: 200% 100%;
          animation: streamingBg 1.5s ease infinite;
        }

        @keyframes streamingBg {
          0% { background-position: 200% 0; }
          100% { background-position: -200% 0; }
        }

        /* Markdown styles */
        .home-md { all: unset; display: block; color: inherit; font-size: inherit; line-height: inherit; }
        .home-md p { margin: 0 0 8px; }
        .home-md p:last-child { margin-bottom: 0; }
        .home-md h1, .home-md h2, .home-md h3 { margin: 12px 0 8px; font-weight: 600; }
        .home-md h1 { font-size: 1.2em; }
        .home-md h2 { font-size: 1.1em; }
        .home-md h3 { font-size: 1em; }
        .home-md ul, .home-md ol { margin: 8px 0; padding-left: 20px; }
        .home-md li { margin-bottom: 4px; }
        .home-md code { background: rgba(0, 0, 0, 0.06); border-radius: 4px; padding: 2px 6px; font-family: 'SF Mono', Consolas, monospace; font-size: 0.88em; }
        .home-md pre { background: rgba(0, 0, 0, 0.04); border-radius: 8px; padding: 12px 16px; margin: 8px 0; overflow-x: auto; }
        .home-md pre code { background: none; padding: 0; }
        .home-md blockquote { border-left: 3px solid rgba(0, 0, 0, 0.15); margin: 8px 0; padding-left: 12px; color: var(--home-text-secondary); }
        .home-md table { border-collapse: collapse; width: 100%; margin: 8px 0; font-size: 0.9em; }
        .home-md th, .home-md td { border: 1px solid var(--home-border); padding: 8px 12px; }
        .home-md th { background: var(--home-bg-secondary); font-weight: 600; }
        .home-md strong { font-weight: 600; }
        .home-md a { color: var(--home-accent); text-decoration: none; }
        .home-md a:hover { text-decoration: underline; }
        .home-md hr { border: none; border-top: 1px solid var(--home-border); margin: 12px 0; }

        /* Method traces */
        .home-method-traces {
          margin-top: 8px;
          margin-left: 44px;
          display: flex;
          flex-direction: column;
          gap: 8px;
        }

        .home-method-item {
          border: 1px solid var(--home-border);
          border-radius: 10px;
          background: var(--home-bg-secondary);
          padding: 8px 12px;
          font-size: 12px;
        }

        .home-method-item summary {
          cursor: pointer;
          color: var(--home-text-secondary);
          font-weight: 500;
          outline: none;
        }

        .home-method-item pre {
          margin: 8px 0 0;
          white-space: pre-wrap;
          word-break: break-word;
          font-size: 11px;
          line-height: 1.5;
          color: var(--home-text-secondary);
        }

        /* Typing indicator */
        .home-typing {
          display: flex !important;
          align-items: center;
          gap: 5px;
          padding: 14px !important;
        }

        .home-typing span {
          width: 8px;
          height: 8px;
          border-radius: 50%;
          background: var(--home-text-secondary);
          display: inline-block;
          animation: typingDot 1.2s ease-in-out infinite;
        }
        .home-typing span:nth-child(2) { animation-delay: 0.2s; }
        .home-typing span:nth-child(3) { animation-delay: 0.4s; }

        @keyframes typingDot {
          0%, 60%, 100% { opacity: 0.3; transform: scale(1); }
          30% { opacity: 1; transform: scale(1.2); }
        }

        /* Input section - 固定在底部，与整体融合 */
        .home-input-section {
          flex-shrink: 0;
          padding: 12px 24px 16px;
        }

        .home-input-container {
          max-width: 720px;
          margin: 0 auto;
        }

        .home-input-wrap {
          display: flex;
          align-items: flex-end;
          gap: 12px;
          background: var(--home-bg-secondary);
          border: 1px solid var(--home-border);
          border-radius: var(--home-radius-lg);
          padding: 12px 12px 12px 20px;
          transition: border-color 0.25s ease, 
                      box-shadow 0.25s cubic-bezier(0.34, 1.56, 0.64, 1);
        }
        .home-input-wrap:focus-within {
          border-color: rgba(0, 0, 0, 0.12);
          box-shadow: 0 0 0 4px rgba(0, 113, 227, 0.08);
        }

        .home-input {
          flex: 1;
          background: transparent;
          border: none;
          outline: none;
          color: var(--home-text-primary);
          font-size: 15px;
          line-height: 1.5;
          resize: none;
          overflow-y: auto;
          font-family: inherit;
          transition: opacity 0.2s ease;
          /* 初始单行高度约 45px，最大三倍约 135px */
          height: 45px;
          min-height: 45px;
          max-height: 135px;
        }
        .home-input::placeholder { color: var(--home-text-secondary); }
        .home-input:disabled { opacity: 0.6; cursor: not-allowed; }
        .home-input::-webkit-scrollbar { width: 4px; }
        .home-input::-webkit-scrollbar-track { background: transparent; }
        .home-input::-webkit-scrollbar-thumb { background: rgba(0, 0, 0, 0.2); border-radius: 2px; }

        .home-send-btn {
          width: 36px;
          height: 36px;
          border-radius: var(--home-radius-full);
          border: none;
          background: var(--home-text-primary);
          color: white;
          cursor: pointer;
          display: flex;
          align-items: center;
          justify-content: center;
          font-size: 14px;
          flex-shrink: 0;
          transition: transform 0.2s cubic-bezier(0.34, 1.56, 0.64, 1), 
                      opacity 0.15s ease,
                      box-shadow 0.15s ease;
        }
        .home-send-btn:not(:disabled):hover {
          transform: scale(1.08);
          box-shadow: 0 4px 12px rgba(0, 0, 0, 0.2);
        }
        .home-send-btn:not(:disabled):active {
          transform: scale(0.95);
        }
        .home-send-btn:disabled { opacity: 0.3; cursor: not-allowed; }
        .home-send-btn:focus-visible {
          outline: 2px solid var(--home-accent);
          outline-offset: 2px;
        }

        /* Send button pulse animation when ready */
        .home-send-btn:not(:disabled) {
          animation: btnPulse 2s ease-in-out infinite;
        }

        @keyframes btnPulse {
          0%, 100% { box-shadow: 0 0 0 0 rgba(26, 26, 26, 0.3); }
          50% { box-shadow: 0 0 0 6px rgba(26, 26, 26, 0); }
        }

        /* Footer hints */
        .home-footer-hints {
          display: flex;
          align-items: center;
          justify-content: center;
          gap: 10px;
          margin-top: 16px;
          font-size: 12px;
          color: var(--home-text-secondary);
          flex-wrap: wrap;
        }

        .home-hint-text {
          color: var(--home-text-secondary);
        }

        .home-hint-tag {
          padding: 6px 14px;
          border-radius: var(--home-radius-full);
          border: 1px solid var(--home-border);
          background: var(--home-bg-primary);
          color: var(--home-text-secondary);
          font-size: 12px;
          cursor: pointer;
          transition: all 0.2s cubic-bezier(0.34, 1.56, 0.64, 1);
        }
        .home-hint-tag:hover {
          background: var(--home-bg-secondary);
          color: var(--home-text-primary);
          transform: translateY(-1px);
          box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);
        }
        .home-hint-tag:focus-visible {
          outline: 2px solid var(--home-accent);
          outline-offset: 2px;
        }

        /* Trust badges */
        .home-trust-badge {
          display: flex;
          align-items: center;
          gap: 6px;
          color: var(--home-text-secondary);
        }

        .home-trust-dot {
          width: 6px;
          height: 6px;
          border-radius: 50%;
          background: var(--home-accent);
          animation: dotPulse 2s ease-in-out infinite;
        }

        @keyframes dotPulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.5; }
        }

        .home-trust-dot--green {
          background: #34c759;
        }

        /* Scrollbar */
        .home-content::-webkit-scrollbar,
        .home-messages::-webkit-scrollbar {
          width: 6px;
        }

        .home-content::-webkit-scrollbar-track,
        .home-messages::-webkit-scrollbar-track {
          background: transparent;
        }

        .home-content::-webkit-scrollbar-thumb,
        .home-messages::-webkit-scrollbar-thumb {
          background: rgba(0, 0, 0, 0.15);
          border-radius: 3px;
        }

        .home-content::-webkit-scrollbar-thumb:hover,
        .home-messages::-webkit-scrollbar-thumb:hover {
          background: rgba(0, 0, 0, 0.25);
        }

        .home-input::-webkit-scrollbar {
          width: 4px;
        }

        .home-input::-webkit-scrollbar-track {
          background: transparent;
        }

        .home-input::-webkit-scrollbar-thumb {
          background: rgba(0, 0, 0, 0.2);
          border-radius: 2px;
        }

        /* Responsive */
        @media (max-width: 768px) {
          .home-content {
            padding: 0 16px;
          }

          .home-title {
            font-size: 26px;
          }

          .home-tags {
            gap: 8px;
          }

          .home-tag {
            padding: 8px 14px;
            font-size: 13px;
          }

          .home-logo-icon-large,
          .home-logo-img {
            width: 48px;
            height: 48px;
            font-size: 24px;
          }

          .home-footer-hints {
            gap: 8px;
          }
        }

        /* Decorative background pattern */
        .home-bg-pattern {
          position: absolute;
          top: 0;
          left: 0;
          right: 0;
          bottom: 0;
          background-image: 
            radial-gradient(circle at 20% 20%, rgba(0, 113, 227, 0.02) 0%, transparent 50%),
            radial-gradient(circle at 80% 80%, rgba(191, 90, 242, 0.02) 0%, transparent 50%);
          pointer-events: none;
          z-index: 0;
        }

        /* Floating particles effect */
        .home-particles {
          position: absolute;
          top: 0;
          left: 0;
          right: 0;
          bottom: 0;
          overflow: hidden;
          pointer-events: none;
          z-index: 0;
        }

        .home-particle {
          position: absolute;
          width: 4px;
          height: 4px;
          border-radius: 50%;
          background: var(--home-accent);
          opacity: 0.1;
          animation: particleFloat 15s ease-in-out infinite;
        }

        .home-particle:nth-child(1) { left: 10%; top: 20%; animation-delay: 0s; animation-duration: 18s; }
        .home-particle:nth-child(2) { left: 20%; top: 60%; animation-delay: 2s; animation-duration: 15s; }
        .home-particle:nth-child(3) { left: 80%; top: 30%; animation-delay: 4s; animation-duration: 20s; }
        .home-particle:nth-child(4) { left: 70%; top: 70%; animation-delay: 6s; animation-duration: 17s; }
        .home-particle:nth-child(5) { left: 50%; top: 40%; animation-delay: 8s; animation-duration: 22s; }

        @keyframes particleFloat {
          0%, 100% {
            transform: translate(0, 0) scale(1);
            opacity: 0.1;
          }
          25% {
            transform: translate(10px, -20px) scale(1.2);
            opacity: 0.2;
          }
          50% {
            transform: translate(-5px, -40px) scale(1);
            opacity: 0.15;
          }
          75% {
            transform: translate(15px, -20px) scale(1.1);
            opacity: 0.2;
          }
        }

        /* Loading shimmer effect */
        .home-shimmer {
          position: relative;
          overflow: hidden;
        }

        .home-shimmer::after {
          content: '';
          position: absolute;
          top: 0;
          left: -100%;
          width: 100%;
          height: 100%;
          background: linear-gradient(
            90deg,
            transparent,
            rgba(255, 255, 255, 0.3),
            transparent
          );
          animation: shimmer 1.5s ease-in-out;
        }

        @keyframes shimmer {
          0% { left: -100%; }
          100% { left: 100%; }
        }

        /* Success animation */
        @keyframes successPop {
          0% { transform: scale(1); }
          50% { transform: scale(1.1); }
          100% { transform: scale(1); }
        }

        .home-success {
          animation: successPop 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
        }

        /* Method traces enhanced */
        .home-method-traces {
          margin-top: 12px;
          margin-left: 44px;
          display: flex;
          flex-direction: column;
          gap: 8px;
          animation: fadeInUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
        }

        .home-method-item {
          border: 1px solid var(--home-border);
          border-radius: 12px;
          background: var(--home-bg-secondary);
          padding: 12px 16px;
          font-size: 12px;
          transition: box-shadow 0.2s ease;
        }

        .home-method-item:hover {
          box-shadow: 0 2px 8px rgba(0, 0, 0, 0.04);
        }

        .home-method-item summary {
          cursor: pointer;
          color: var(--home-text-secondary);
          font-weight: 500;
          outline: none;
          transition: color 0.15s ease;
        }

        .home-method-item summary:hover {
          color: var(--home-text-primary);
        }

        .home-method-item summary:focus-visible {
          outline: 2px solid var(--home-accent);
          outline-offset: 2px;
          border-radius: 4px;
        }

        .home-method-item pre {
          margin: 12px 0 0;
          white-space: pre-wrap;
          word-break: break-word;
          font-size: 11px;
          line-height: 1.6;
          color: var(--home-text-secondary);
          background: rgba(0, 0, 0, 0.02);
          padding: 12px;
          border-radius: 8px;
        }

        /* Phase indicator animation */
        .home-phase-indicator {
          display: inline-flex;
          align-items: center;
          gap: 8px;
          padding: 6px 12px;
          background: var(--home-bg-secondary);
          border-radius: var(--home-radius-full);
          font-size: 12px;
          color: var(--home-text-secondary);
          margin-bottom: 16px;
          animation: fadeInUp 0.3s cubic-bezier(0.16, 1, 0.3, 1);
        }

        .home-phase-dot {
          width: 8px;
          height: 8px;
          border-radius: 50%;
          background: var(--home-accent);
          animation: phaseDot 1s ease-in-out infinite;
        }

        @keyframes phaseDot {
          0%, 100% { transform: scale(1); opacity: 1; }
          50% { transform: scale(1.3); opacity: 0.7; }
        }
      `}</style>
    </div>
  );
}
