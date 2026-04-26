import React, { useEffect, useState, useCallback, useRef } from 'react';
import { Spin, Dropdown, Modal, message } from 'antd';
import type { MenuProps } from 'antd';
import { FolderOutlined, ReloadOutlined, MoreOutlined, EditOutlined, DeleteOutlined, PlusOutlined, LoadingOutlined } from '@ant-design/icons';
import { listRootNodes } from '@/services/api/tree';
import { updateNode, deleteNode } from '@/services/api/nodes';
import type { RootNodeSummary } from '@/types/models';

export type HomePanelSection = 'history' | 'tool_monitor';

const STATUS_COLORS: Record<string, string> = {
  pending: '#f59e0b',
  running: '#3b82f6',
  completed: '#10b981',
  failed: '#ef4444',
  waiting: '#8b5cf6',
};

function getStatusColor(status?: string): string {
  return status ? (STATUS_COLORS[status] ?? '#60a5fa') : '#60a5fa';
}

function formatTime(isoString: string): string {
  try {
    const date = new Date(isoString);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return '刚刚';
    if (diffMins < 60) return `${diffMins} 分钟前`;
    if (diffHours < 24) return `${diffHours} 小时前`;
    if (diffDays < 7) return `${diffDays} 天前`;
    return date.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' });
  } catch {
    return '';
  }
}

const STATUS_OPTIONS = [
  { key: 'pending', label: '待处理', color: '#f59e0b' },
  { key: 'running', label: '进行中', color: '#3b82f6' },
  { key: 'completed', label: '已完成', color: '#10b981' },
  { key: 'failed', label: '失败', color: '#ef4444' },
];

interface ProjectItemProps {
  project: RootNodeSummary;
  active: boolean;
  onClick: (project: RootNodeSummary) => void;
  onDeleted: (id: string) => void;
  onRenamed: (id: string, newName: string) => void;
  onStatusChanged: (id: string, newStatus: string) => void;
}

function ProjectItem({ project, active, onClick, onDeleted, onRenamed, onStatusChanged }: ProjectItemProps) {
  const [hovered, setHovered] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editName, setEditName] = useState(project.name || '');
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleStartEdit = useCallback((e: React.MouseEvent) => {
    e.stopPropagation();
    setEditName(project.name || '');
    setEditing(true);
    setTimeout(() => inputRef.current?.select(), 0);
  }, [project.name]);

  const handleSaveEdit = useCallback(async () => {
    const newName = editName.trim();
    if (!newName || newName === project.name) {
      setEditing(false);
      return;
    }
    setSaving(true);
    try {
      await updateNode(project.id, { name: newName });
      onRenamed(project.id, newName);
      setEditing(false);
      message.success('重命名成功');
    } catch (err) {
      console.error('重命名失败:', err);
      message.error('重命名失败');
    } finally {
      setSaving(false);
    }
  }, [editName, project.id, project.name, onRenamed]);

  const handleCancelEdit = useCallback((e?: React.MouseEvent) => {
    e?.stopPropagation();
    setEditing(false);
    setEditName(project.name || '');
  }, [project.name]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      void handleSaveEdit();
    } else if (e.key === 'Escape') {
      handleCancelEdit();
    }
  }, [handleSaveEdit, handleCancelEdit]);

  const handleDelete = useCallback((e: React.MouseEvent) => {
    e.stopPropagation();
    Modal.confirm({
      title: '确认删除',
      content: `确定要删除项目「${project.name || project.id}」吗？此操作不可恢复。`,
      okText: '删除',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        try {
          await deleteNode(project.id);
          onDeleted(project.id);
          message.success('删除成功');
        } catch (err) {
          console.error('删除失败:', err);
          message.error('删除失败');
        }
      },
    });
  }, [project.id, project.name, onDeleted]);

  const handleStatusChange = useCallback(async (newStatus: string) => {
    if (newStatus === project.status) return;
    try {
      await updateNode(project.id, { status: newStatus });
      onStatusChanged(project.id, newStatus);
      message.success('状态已更新');
    } catch (err) {
      console.error('状态更新失败:', err);
      message.error('状态更新失败');
    }
  }, [project.id, project.status, onStatusChanged]);

  const menuItems: NonNullable<MenuProps['items']> = [
    {
      key: 'rename',
      icon: <EditOutlined />,
      label: '重命名',
      onClick: ({ domEvent }) => {
        domEvent.stopPropagation();
        void handleStartEdit(domEvent as unknown as React.MouseEvent);
      },
    },
    {
      key: 'status',
      label: '修改状态',
        children: STATUS_OPTIONS.map((option) => ({
          key: option.key,
          label: (
            <span style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
              <span style={{ 
                width: 10, 
                height: 10, 
                borderRadius: '50%', 
                backgroundColor: option.color,
                display: 'inline-block',
                flexShrink: 0
              }} />
              <span style={{ flex: 1 }}>{option.label}</span>
              {project.status === option.key && <span style={{ opacity: 0.6, marginLeft: 'auto' }}>✓</span>}
            </span>
          ),
          onClick: (e) => {
            e.domEvent.stopPropagation();
            void handleStatusChange(option.key);
          },
        })) as NonNullable<MenuProps['items']>,
    },
    {
      key: 'delete',
      icon: <DeleteOutlined />,
      label: '删除',
      danger: true,
      onClick: ({ domEvent }) => {
        domEvent.stopPropagation();
        void handleDelete(domEvent as unknown as React.MouseEvent);
      },
    },
  ];

  return (
    <div
      className={`project-item ${editing ? 'editing' : ''} ${active ? 'active' : ''}`}
      style={{ borderLeftColor: hovered ? getStatusColor(project.status) : 'transparent' }}
      onClick={() => !editing && onClick(project)}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
    >
      <div className="project-item-left">
        <FolderOutlined className="project-icon" />
        <div className="project-info">
          {editing ? (
            <input
              ref={inputRef}
              className="project-name-input"
              value={editName}
              onChange={(e) => setEditName(e.target.value)}
              onKeyDown={handleKeyDown}
              onBlur={handleSaveEdit}
              onClick={(e) => e.stopPropagation()}
              maxLength={50}
            />
          ) : (
            <span className="project-name">{project.name || project.id}</span>
          )}
          {!editing && <span className="project-time">{formatTime(project.updated_at)}</span>}
        </div>
      </div>
      <div className="project-item-right">
        {editing ? (
          <div className="project-edit-actions">
            <button
              className="project-edit-btn save"
              onClick={(e) => { e.stopPropagation(); void handleSaveEdit(); }}
              disabled={saving}
              title="保存"
            >
              ✓
            </button>
            <button
              className="project-edit-btn cancel"
              onClick={(e) => { e.stopPropagation(); handleCancelEdit(e); }}
              title="取消"
            >
              ✕
            </button>
          </div>
        ) : (
          <Dropdown
            menu={{ items: menuItems }}
            trigger={['click']}
            placement="bottomRight"
            getPopupContainer={() => document.body}
          >
            <button
              className="project-more-btn"
              onClick={(e) => e.stopPropagation()}
              title="更多操作"
            >
              <MoreOutlined />
            </button>
          </Dropdown>
        )}
      </div>
    </div>
  );
}

interface ProjectPanelProps {
  activeNodeId?: string | null;
  onProjectSelect?: (project: RootNodeSummary) => void;
  onNewConversation?: () => void;
  creatingConversation?: boolean;
  activeSection?: HomePanelSection;
  onSectionChange?: (section: HomePanelSection) => void;
}

export default function ProjectPanel({
  activeNodeId = null,
  onProjectSelect,
  onNewConversation,
  creatingConversation = false,
  activeSection = 'history',
  onSectionChange,
}: ProjectPanelProps) {
  const [projects, setProjects] = useState<RootNodeSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  const fetchProjects = useCallback(async () => {
    setLoading(true);
    setError(false);
    try {
      const data = await listRootNodes();
      setProjects(data ?? []);
    } catch (err) {
      console.error('加载项目列表失败:', err);
      setError(true);
      setProjects([]);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeSection !== 'history') {
      return;
    }
    void fetchProjects();
  }, [activeSection, fetchProjects]);

  const handleProjectClick = useCallback(
    (project: RootNodeSummary) => {
      onProjectSelect?.(project);
    },
    [onProjectSelect],
  );

  return (
    <aside className="project-panel">
      <div className="project-panel-header">
        <div className="project-panel-sections">
          <button
            className={`project-section-tab ${activeSection === 'history' ? 'active' : ''}`}
            onClick={() => onSectionChange?.('history')}
            type="button"
          >
            我的历史
          </button>
          <button
            className={`project-section-tab ${activeSection === 'tool_monitor' ? 'active' : ''}`}
            onClick={() => onSectionChange?.('tool_monitor')}
            type="button"
          >
            工具监控
          </button>
        </div>
        {activeSection === 'history' ? (
          <div className="project-panel-actions">
            <button
              className="project-new-btn"
              onClick={() => onNewConversation?.()}
              disabled={creatingConversation}
              aria-label="开启新会话"
              title="开启新会话"
            >
              {creatingConversation ? <LoadingOutlined /> : <PlusOutlined />}
              <span>开启新会话</span>
            </button>
            <button
              className="project-refresh-btn"
              onClick={() => void fetchProjects()}
              disabled={loading}
              aria-label="刷新"
            >
              <ReloadOutlined spin={loading} />
            </button>
          </div>
        ) : null}
      </div>

      <div className="project-panel-content">
        {activeSection !== 'history' ? (
          <div className="project-empty">
            <FolderOutlined className="project-empty-icon" />
            <span>已切换到工具监控</span>
            <span className="project-empty-hint">请在右侧主区域查看监控数据</span>
          </div>
        ) : loading && projects.length === 0 ? (
          <div className="project-loading">
            <Spin size="small" />
            <span>加载中...</span>
          </div>
        ) : error ? (
          <div className="project-error">
            <span>加载失败</span>
            <button onClick={() => void fetchProjects()}>重试</button>
          </div>
        ) : projects.length === 0 ? (
          <div className="project-empty">
            <FolderOutlined className="project-empty-icon" />
            <span>暂无项目</span>
            <span className="project-empty-hint">生成任务树后将在此显示</span>
          </div>
        ) : (
          <div className="project-list">
            {projects.map((project) => (
              <ProjectItem
                key={project.id}
                project={project}
                active={activeNodeId === project.id}
                onClick={handleProjectClick}
                onDeleted={(id) => {
                  setProjects((prev) => prev.filter((p) => p.id !== id));
                }}
                onRenamed={(id, newName) => {
                  setProjects((prev) =>
                    prev.map((p) => (p.id === id ? { ...p, name: newName } : p))
                  );
                }}
                onStatusChanged={(id, newStatus) => {
                  setProjects((prev) =>
                    prev.map((p) => (p.id === id ? { ...p, status: newStatus as RootNodeSummary['status'] } : p))
                  );
                }}
              />
            ))}
          </div>
        )}
      </div>

      <style>{`
        .project-panel {
          width: 260px;
          min-width: 260px;
          height: 100%;
          display: flex;
          flex-direction: column;
          background: #1d1d1f;
          border-right: 1px solid rgba(255, 255, 255, 0.08);
        }

        /* Header section */
        .project-panel-header {
          min-height: 56px;
          display: flex;
          align-items: flex-start;
          justify-content: space-between;
          padding: 10px 12px;
          border-bottom: 1px solid rgba(255, 255, 255, 0.08);
          flex-shrink: 0;
          gap: 8px;
        }

        .project-panel-sections {
          display: inline-flex;
          align-items: center;
          gap: 6px;
          flex-wrap: wrap;
        }

        .project-section-tab {
          border: 1px solid rgba(255, 255, 255, 0.16);
          background: transparent;
          color: rgba(255, 255, 255, 0.65);
          border-radius: 999px;
          height: 30px;
          padding: 0 10px;
          font-size: 12px;
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .project-section-tab:hover {
          border-color: rgba(255, 255, 255, 0.4);
          color: rgba(255, 255, 255, 0.95);
        }

        .project-section-tab.active {
          background: rgba(255, 255, 255, 0.12);
          color: rgba(255, 255, 255, 0.95);
          border-color: rgba(255, 255, 255, 0.5);
        }

        .project-panel-actions {
          display: inline-flex;
          align-items: center;
          gap: 8px;
          margin-left: auto;
        }

        .project-new-btn,
        .project-refresh-btn {
          width: 28px;
          height: 28px;
          display: flex;
          align-items: center;
          justify-content: center;
          border: none;
          border-radius: 8px;
          background: transparent;
          color: rgba(255, 255, 255, 0.5);
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .project-new-btn {
          width: auto;
          min-width: 96px;
          padding: 0 10px;
          gap: 6px;
          background: rgba(255, 255, 255, 0.1);
          color: rgba(255, 255, 255, 0.85);
          font-size: 12px;
          font-weight: 500;
        }

        .project-new-btn:hover:not(:disabled),
        .project-refresh-btn:hover:not(:disabled) {
          background: rgba(255, 255, 255, 0.1);
          color: rgba(255, 255, 255, 0.8);
        }

        .project-new-btn:focus-visible,
        .project-refresh-btn:focus-visible {
          outline: 2px solid rgba(0, 113, 227, 0.8);
          outline-offset: 2px;
        }

        .project-new-btn:disabled,
        .project-refresh-btn:disabled {
          opacity: 0.4;
          cursor: not-allowed;
        }

        .project-panel-content {
          flex: 1;
          overflow-y: auto;
          padding: 12px 0;
        }

        .project-loading,
        .project-error,
        .project-empty {
          display: flex;
          flex-direction: column;
          align-items: center;
          justify-content: center;
          gap: 12px;
          height: 160px;
          color: rgba(255, 255, 255, 0.4);
          font-size: 13px;
          padding: 0 20px;
        }

        .project-error {
          color: #ff453a;
        }

        .project-error button {
          padding: 8px 16px;
          border: 1px solid rgba(255, 69, 58, 0.4);
          border-radius: 8px;
          background: rgba(255, 69, 58, 0.1);
          color: #ff453a;
          font-size: 13px;
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .project-error button:hover {
          background: rgba(255, 69, 58, 0.2);
        }

        .project-empty-icon {
          font-size: 32px;
          color: rgba(255, 255, 255, 0.2);
          margin-bottom: 4px;
        }

        .project-empty-hint {
          font-size: 12px;
          color: rgba(255, 255, 255, 0.3);
          text-align: center;
        }

        .project-list {
          display: flex;
          flex-direction: column;
          gap: 2px;
          padding: 0 10px;
        }

        .project-item {
          height: 44px;
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 0 12px;
          padding-left: 10px;
          border-radius: 10px;
          border-left: 3px solid transparent;
          cursor: pointer;
          transition: background 0.12s ease, border-color 0.15s ease;
        }

        .project-item:hover {
          background: rgba(255, 255, 255, 0.08);
        }

        .project-item.active {
          background: rgba(255, 255, 255, 0.12);
          border-left-color: rgba(96, 165, 250, 0.9) !important;
        }

        .project-item:active:not(.editing) {
          background: rgba(255, 255, 255, 0.12);
        }

        .project-item.editing {
          background: rgba(255, 255, 255, 0.06);
          cursor: default;
          border-left-color: rgba(255, 255, 255, 0.2);
        }

        .project-item-left {
          display: flex;
          align-items: center;
          gap: 12px;
          min-width: 0;
          flex: 1;
        }

        .project-icon {
          font-size: 16px;
          color: rgba(255, 255, 255, 0.9);
          flex-shrink: 0;
        }

        .project-info {
          display: flex;
          flex-direction: column;
          min-width: 0;
          gap: 2px;
        }

        .project-name {
          font-size: 14px;
          font-weight: 500;
          color: rgba(255, 255, 255, 0.9);
          white-space: nowrap;
          overflow: hidden;
          text-overflow: ellipsis;
          max-width: 140px;
        }

        .project-name-input {
          width: 140px;
          height: 26px;
          padding: 0 10px;
          border: 1px solid rgba(255, 255, 255, 0.2);
          border-radius: 6px;
          background: rgba(255, 255, 255, 0.08);
          color: rgba(255, 255, 255, 0.9);
          font-size: 14px;
          font-weight: 500;
          outline: none;
        }

        .project-name-input:focus {
          border-color: rgba(0, 113, 227, 0.8);
          box-shadow: 0 0 0 3px rgba(0, 113, 227, 0.2);
        }

        .project-time {
          font-size: 11px;
          color: rgba(255, 255, 255, 0.4);
        }

        .project-item-right {
          display: flex;
          align-items: center;
          gap: 8px;
          flex-shrink: 0;
        }

        .project-more-btn {
          width: 24px;
          height: 24px;
          display: flex;
          align-items: center;
          justify-content: center;
          border: none;
          border-radius: 6px;
          background: transparent;
          color: rgba(255, 255, 255, 0.5);
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .project-more-btn:hover {
          background: rgba(255, 255, 255, 0.1);
          color: rgba(255, 255, 255, 0.9);
        }

        .project-more-btn:focus-visible {
          outline: 2px solid rgba(0, 113, 227, 0.8);
          outline-offset: 2px;
        }

        /* 下拉菜单样式 */
        .ant-dropdown-menu {
          background: #1f1f1f !important;
          border-radius: 10px !important;
          padding: 6px !important;
          box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4) !important;
          min-width: 160px !important;
        }

        .ant-dropdown-menu-item {
          border-radius: 6px !important;
          padding: 10px 12px !important;
          color: rgba(255, 255, 255, 0.9) !important;
          font-size: 14px !important;
        }

        .ant-dropdown-menu-item:hover {
          background: rgba(255, 255, 255, 0.1) !important;
        }

        .ant-dropdown-menu-item .anticon {
          color: rgba(255, 255, 255, 0.7) !important;
          margin-right: 10px !important;
        }

        .ant-dropdown-menu-submenu-title {
          border-radius: 6px !important;
          padding: 10px 12px !important;
          color: rgba(255, 255, 255, 0.9) !important;
          font-size: 14px !important;
        }

        .ant-dropdown-menu-submenu-title:hover {
          background: rgba(255, 255, 255, 0.1) !important;
        }

        .ant-dropdown-menu-submenu .ant-dropdown-menu {
          background: #1f1f1f !important;
          border-radius: 10px !important;
          padding: 6px !important;
          box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4) !important;
        }

        .ant-dropdown-menu-item-danger {
          color: #ff453a !important;
        }

        .ant-dropdown-menu-item-danger:hover {
          background: rgba(255, 69, 58, 0.15) !important;
        }

        .ant-dropdown-menu-item-danger .anticon {
          color: #ff453a !important;
        }

        .project-edit-actions {
          display: flex;
          align-items: center;
          gap: 4px;
        }

        .project-edit-btn {
          width: 22px;
          height: 22px;
          display: flex;
          align-items: center;
          justify-content: center;
          border: none;
          border-radius: 6px;
          background: transparent;
          font-size: 12px;
          cursor: pointer;
          transition: all 0.15s ease;
        }

        .project-edit-btn.save {
          color: #34c759;
        }

        .project-edit-btn.save:hover {
          background: rgba(52, 199, 89, 0.2);
        }

        .project-edit-btn.cancel {
          color: rgba(255, 255, 255, 0.5);
        }

        .project-edit-btn.cancel:hover {
          background: rgba(255, 69, 58, 0.2);
          color: #ff453a;
        }

        .project-edit-btn:disabled {
          opacity: 0.4;
          cursor: not-allowed;
        }

        .project-arrow {
          font-size: 11px;
          color: rgba(255, 255, 255, 0.3);
        }

        /* Scrollbar */
        .project-panel-content::-webkit-scrollbar {
          width: 4px;
        }

        .project-panel-content::-webkit-scrollbar-track {
          background: transparent;
        }

        .project-panel-content::-webkit-scrollbar-thumb {
          background: rgba(255, 255, 255, 0.15);
          border-radius: 2px;
        }

        .project-panel-content::-webkit-scrollbar-thumb:hover {
          background: rgba(255, 255, 255, 0.25);
        }

        /* Reduced motion */
        @media (prefers-reduced-motion: reduce) {
          .project-item,
          .project-refresh-btn,
          .project-more-btn,
          .project-edit-btn {
            transition: none !important;
          }
        }
      `}</style>
    </aside>
  );
}
