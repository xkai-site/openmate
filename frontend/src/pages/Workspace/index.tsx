import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Spin, Button, Tabs, Tree, Segmented, App } from 'antd';
import type { DataNode } from 'antd/es/tree';
import {
  ArrowLeftOutlined,
  SettingOutlined,
  MessageOutlined,
  FolderOutlined,
  FileTextOutlined,
  BulbOutlined,
  MoonOutlined,
  BranchesOutlined,
  LoadingOutlined,
} from '@ant-design/icons';
import { useAITree } from '@/hooks/useAITree';
import { getNode } from '@/services/api/nodes';
import { decomposeNode } from '@/services/api/tree';
import type { Edge, Node } from 'reactflow';
import SessionPanel from '@/pages/AITree/components/SessionPanel';
import MemoryPanel from '@/pages/AITree/components/MemoryPanel';

type ThemeMode = 'dark' | 'light';

interface TreeNodeData {
  id: string;
  name?: string;
  status?: string;
}

function buildTreeFromFlow(nodes: Node<TreeNodeData>[], edges: Edge[]): DataNode[] {
  if (!nodes.length) return [];

  const nodeById = new Map<string, Node<TreeNodeData>>();
  for (const node of nodes) nodeById.set(node.id, node);

  const childrenMap = new Map<string, string[]>();
  const childSet = new Set<string>();

  for (const edge of edges) {
    const source = edge.source;
    const target = edge.target;

    if (!childrenMap.has(source)) childrenMap.set(source, []);
    childrenMap.get(source)!.push(target);
    childSet.add(target);
  }

  const roots = nodes.filter((n) => !childSet.has(n.id));

  const buildNode = (node: Node<TreeNodeData>): DataNode => {
    const childIds = childrenMap.get(node.id) ?? [];
    const isFolder = childIds.length > 0;

    return {
      key: node.id,
      isLeaf: !isFolder,
      title: (
        <span className="tree-node-title">
          {isFolder ? (
            <FolderOutlined className="tree-node-icon tree-folder-icon" aria-hidden="true" />
          ) : (
            <FileTextOutlined className="tree-node-icon tree-file-icon" aria-hidden="true" />
          )}
          <span className="tree-node-text">{node.data?.name || node.id}</span>
        </span>
      ),
      children: isFolder
        ? childIds
            .map((id) => nodeById.get(id))
            .filter(Boolean)
            .map((child) => buildNode(child!))
        : undefined,
    };
  };

  return roots.map(buildNode);
}

function LeftPanel({
  nodes,
  edges,
  activeNodeId,
  onSelectNode,
  themeMode,
}: {
  nodes: Node<TreeNodeData>[];
  edges: Edge[];
  activeNodeId: string;
  onSelectNode: (id: string) => void;
  themeMode: ThemeMode;
}) {
  const treeData = useMemo(() => buildTreeFromFlow(nodes, edges), [nodes, edges]);

  const defaultExpandedKeys = useMemo(() => {
    const keys = new Set<React.Key>();
    for (const edge of edges) keys.add(edge.source);
    return Array.from(keys);
  }, [edges]);

  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);

  useEffect(() => {
    setExpandedKeys(defaultExpandedKeys);
  }, [defaultExpandedKeys]);

  return (
    <aside className={`workspace-sidebar ${themeMode}`}>
      <div className="workspace-sidebar-header">
        <span>Explorer</span>
      </div>

      <div className="workspace-sidebar-content">
        {nodes.length === 0 ? (
          <div className="workspace-empty">暂无节点</div>
        ) : (
          <Tree
            treeData={treeData}
            selectedKeys={activeNodeId ? [activeNodeId] : []}
            expandedKeys={expandedKeys}
            onExpand={(keys) => setExpandedKeys(keys)}
            onSelect={(keys, info) => {
              const id = keys[0] as string | undefined;
              if (!id) return;

              // 父节点同时切换展开状态并触发对话
              if (!info.node.isLeaf) {
                const newExpandedKeys = expandedKeys.includes(id)
                  ? expandedKeys.filter(k => k !== id)
                  : [...expandedKeys, id];
                setExpandedKeys(newExpandedKeys);
              }

              // 所有节点均可进入会话
              onSelectNode(id);
            }}
            blockNode
            className={`workspace-tree ${themeMode}`}
          />
        )}
      </div>
    </aside>
  );
}

function RightPanel({ activeNodeId, themeMode, memoryRefetchKey }: { activeNodeId: string; themeMode: ThemeMode; memoryRefetchKey: number }) {
  const items = [
    {
      key: 'memory',
      label: (
        <span className="flex items-center gap-1.5">
          <SettingOutlined aria-hidden="true" />
          <span>Memory</span>
        </span>
      ),
      children: <MemoryPanel nodeId={activeNodeId} refetchKey={memoryRefetchKey} />,
    },
  ];

  return (
    <aside className={`workspace-right ${themeMode}`}>
      <Tabs items={items} className={`workspace-tabs ${themeMode}`} tabBarStyle={{ marginBottom: 0, paddingLeft: 8 }} />
    </aside>
  );
}

export default function WorkspacePage() {
  const { nodeId } = useParams<{ nodeId: string }>();
  const navigate = useNavigate();
  const { message } = App.useApp();

  // 追溯当前节点所在树的根节点 ID
  const [rootId, setRootId] = useState<string | undefined>(undefined);

  useEffect(() => {
    if (!nodeId) return;
    let cancelled = false;

    const findRoot = async (id: string): Promise<string> => {
      const node = await getNode(id);
      if (!node.parent_id) return id;
      return findRoot(node.parent_id);
    };

    findRoot(nodeId).then((id) => {
      if (!cancelled) setRootId(id);
    }).catch(() => {
      if (!cancelled) setRootId(nodeId);
    });

    return () => { cancelled = true; };
  }, [nodeId]);

  const { treeQuery, nodes, edges } = useAITree(rootId);

  const [activeNodeId, setActiveNodeId] = useState<string>(nodeId || '');
  const [themeMode, setThemeMode] = useState<ThemeMode>(() => {
    const cached = localStorage.getItem('workspace-theme');
    return cached === 'light' ? 'light' : 'dark';
  });
  const [memoryRefetchKey, setMemoryRefetchKey] = useState(0);
  const [isDecomposing, setIsDecomposing] = useState(false);

  // 判断是否为"单节点"状态（无子节点，可触发拆解）
  const isSingleNode = useMemo(() => nodes.length <= 1, [nodes]);

  useEffect(() => {
    if (nodeId) setActiveNodeId(nodeId);
  }, [nodeId]);

  useEffect(() => {
    localStorage.setItem('workspace-theme', themeMode);
  }, [themeMode]);

  const activeNode = useMemo(() => nodes.find((n) => n.id === activeNodeId), [nodes, activeNodeId]);
  const activeNodeName = activeNode?.data?.name || activeNodeId || '未选择节点';

  const handleSelectNode = useCallback(
    (id: string) => {
      setActiveNodeId(id);
      navigate(`/workspace/${id}`);
    },
    [navigate],
  );

  const handleDecompose = useCallback(async () => {
    if (!rootId || isDecomposing) return;
    setIsDecomposing(true);
    const loadingMsg = message.loading('AI 正在分析对话内容并生成任务树…', 0);
    try {
      const result = await decomposeNode(rootId);
      loadingMsg();
      message.success(`任务树生成成功，包含 ${result.nodes.length} 个子节点`);
      void treeQuery.refetch();
    } catch (err) {
      loadingMsg();
      console.error('生成任务树失败:', err);
      message.error(err instanceof Error ? `生成失败: ${err.message}` : '生成失败，请重试');
    } finally {
      setIsDecomposing(false);
    }
  }, [rootId, isDecomposing, message]);

  if (treeQuery.isLoading && !nodes.length) {
    return (
      <div className="h-screen flex items-center justify-center bg-slate-950">
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div className={`workspace-root ${themeMode}`}>
      <header className="workspace-topbar">
        <div className="workspace-topbar-left">
          <Button
            type="text"
            size="small"
            icon={<ArrowLeftOutlined />}
            aria-label="返回 AITree"
            onClick={() => navigate(rootId ? `/aitree?rootId=${rootId}` : '/aitree')}
            className="workspace-action-btn"
          />

          <span className="workspace-crumb">
            <span className="workspace-brand">AITree</span>
            <span className="workspace-divider">/</span>
            <span className="workspace-current">{activeNodeName}</span>
          </span>
        </div>

        <div className="workspace-topbar-right">
          {isSingleNode && rootId && (
            <Button
              size="small"
              icon={isDecomposing ? <LoadingOutlined /> : <BranchesOutlined />}
              onClick={() => void handleDecompose()}
              disabled={isDecomposing}
              className="workspace-decompose-btn"
            >
              生成任务树
            </Button>
          )}
          <Segmented
            value={themeMode}
            onChange={(v) => setThemeMode(v as ThemeMode)}
            size="small"
            options={[
              { value: 'dark', icon: <MoonOutlined />, label: '暗' },
              { value: 'light', icon: <BulbOutlined />, label: '亮' },
            ]}
          />
        </div>
      </header>

      <main className="workspace-main">
        <LeftPanel
          nodes={nodes}
          edges={edges}
          activeNodeId={activeNodeId}
          onSelectNode={handleSelectNode}
          themeMode={themeMode}
        />

        <section className="workspace-center">
          <div className="workspace-center-header">
            <div className="workspace-center-title">
              <MessageOutlined aria-hidden="true" />
              <span>{activeNodeName}</span>
            </div>
          </div>

          <div className="workspace-center-content">
            {activeNodeId ? (
              <SessionPanel
                nodeId={activeNodeId}
                themeMode={themeMode}
                onAIReply={() => setMemoryRefetchKey((k) => k + 1)}
              />
            ) : (
              <div className="workspace-empty">请在左侧选择一个节点</div>
            )}
          </div>
        </section>

        {activeNodeId && <RightPanel activeNodeId={activeNodeId} themeMode={themeMode} memoryRefetchKey={memoryRefetchKey} />}
      </main>

      <style>{`
        .workspace-root {
          height: 100vh;
          width: 100%;
          display: flex;
          flex-direction: column;
          overflow: hidden;
          transition: background-color .15s ease;
        }

        .workspace-root.dark {
          background: radial-gradient(1200px 680px at 30% 0%, #1b2d52 0%, #0b1224 52%, #070b16 100%);
          color: #eaf2ff;
        }

        .workspace-root.light {
          background: radial-gradient(900px 460px at 30% 0%, #dbeafe 0%, #f8fbff 45%, #f5f8ff 100%);
          color: #1f2937;
        }

        .workspace-topbar {
          height: 44px;
          display: flex;
          align-items: center;
          justify-content: space-between;
          padding: 0 10px;
          border-bottom: 1px solid;
          backdrop-filter: saturate(150%) blur(10px);
        }

        .workspace-root.dark .workspace-topbar {
          background: rgba(12, 18, 34, .88);
          border-color: rgba(129, 166, 255, .28);
        }

        .workspace-root.light .workspace-topbar {
          background: rgba(255, 255, 255, .88);
          border-color: rgba(30, 64, 175, .12);
        }

        .workspace-topbar-left {
          min-width: 0;
          display: flex;
          align-items: center;
          gap: 10px;
        }

        .workspace-crumb {
          min-width: 0;
          display: inline-flex;
          align-items: center;
          gap: 8px;
          font-size: 13px;
          font-weight: 500;
        }

        .workspace-brand { color: #60a5fa; }
        .workspace-divider { opacity: .55; }
        .workspace-current {
          min-width: 0;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .workspace-root.dark .workspace-current { color: #eaf2ff; }
        .workspace-root.dark .workspace-action-btn {
          color: #d5e5ff !important;
        }
        .workspace-root.dark .workspace-action-btn:hover {
          background: rgba(96, 165, 250, .2) !important;
          color: #ffffff !important;
        }
        .workspace-root.dark .workspace-center-title { color: #dbe8ff; }

        .workspace-action-btn {
          border-radius: 8px !important;
          transition: transform .06s ease, background-color .08s ease !important;
        }
        .workspace-action-btn:active {
          transform: scale(.95);
        }

        .workspace-topbar-right {
          display: flex;
          align-items: center;
          gap: 8px;
        }

        .workspace-decompose-btn {
          font-size: 12px !important;
          font-weight: 600 !important;
          border-radius: 8px !important;
          transition: transform .08s ease !important;
        }
        .workspace-root.dark .workspace-decompose-btn {
          background: linear-gradient(90deg, rgba(59,130,246,.8), rgba(99,102,241,.8)) !important;
          border-color: transparent !important;
          color: #fff !important;
        }
        .workspace-root.dark .workspace-decompose-btn:not(:disabled):hover {
          transform: translateY(-1px) !important;
          box-shadow: 0 4px 12px rgba(99,102,241,.4) !important;
        }
        .workspace-root.light .workspace-decompose-btn {
          background: linear-gradient(90deg, #3b82f6, #6366f1) !important;
          border-color: transparent !important;
          color: #fff !important;
        }

        .workspace-main {
          flex: 1;
          min-height: 0;
          display: grid;
          grid-template-columns: 248px minmax(0,1fr) 300px;
          overflow: hidden;
        }

        .workspace-sidebar {
          min-width: 0;
          border-right: 1px solid;
          display: flex;
          flex-direction: column;
          backdrop-filter: blur(8px);
        }

        .workspace-sidebar.dark {
          background: rgba(12, 18, 34, .86);
          border-color: rgba(129, 166, 255, .24);
        }

        .workspace-sidebar.light {
          background: rgba(255, 255, 255, .8);
          border-color: rgba(30, 64, 175, .12);
        }

        .workspace-sidebar-header {
          height: 34px;
          display: flex;
          align-items: center;
          padding: 0 12px;
          font-size: 11px;
          font-weight: 700;
          letter-spacing: .12em;
          text-transform: uppercase;
          border-bottom: 1px solid;
        }

        .workspace-sidebar.dark .workspace-sidebar-header {
          color: #bfd2ff;
          border-color: rgba(129, 166, 255, .2);
        }

        .workspace-sidebar.light .workspace-sidebar-header {
          color: #1d4ed8;
          border-color: rgba(30, 64, 175, .1);
        }

        .workspace-sidebar-content {
          flex: 1;
          overflow: auto;
          padding-top: 6px;
        }

        .workspace-tree {
          background: transparent !important;
          font-size: 13px;
        }

        .workspace-tree .ant-tree-treenode {
          align-items: center;
          padding: 2px 4px !important;
        }

        .workspace-tree .ant-tree-node-content-wrapper {
          height: 26px;
          line-height: 26px;
          border-radius: 8px !important;
          padding: 0 8px !important;
          transition: background-color .08s ease;
        }

        .workspace-tree .ant-tree-switcher {
          width: 18px !important;
          line-height: 26px !important;
        }

        .workspace-tree .ant-tree-indent-unit {
          width: 14px !important;
        }

        .tree-node-title {
          min-width: 0;
          display: inline-flex;
          align-items: center;
          gap: 7px;
          vertical-align: middle;
        }

        .tree-node-icon {
          font-size: 13px;
          line-height: 1;
          display: inline-flex;
          align-items: center;
          justify-content: center;
        }

        .tree-folder-icon { color: #fbbf24; }
        .tree-file-icon { color: #38bdf8; }

        .tree-node-text {
          min-width: 0;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }

        .workspace-tree.dark .ant-tree-node-content-wrapper { color: #e4edff; }
        .workspace-tree.dark .ant-tree-node-content-wrapper:hover { background: rgba(96, 165, 250, .22) !important; }
        .workspace-tree.dark .ant-tree-node-content-wrapper.ant-tree-node-selected {
          background: linear-gradient(90deg, rgba(59,130,246,.5), rgba(59,130,246,.24)) !important;
          color: #ffffff !important;
        }
        .workspace-tree.dark .ant-tree-switcher { color: #a8c5ff !important; }

        .workspace-tree.light .ant-tree-node-content-wrapper { color: #1e3a8a; }
        .workspace-tree.light .ant-tree-node-content-wrapper:hover { background: rgba(30, 64, 175, .1) !important; }
        .workspace-tree.light .ant-tree-node-content-wrapper.ant-tree-node-selected {
          background: linear-gradient(90deg, rgba(37,99,235,.18), rgba(37,99,235,.08)) !important;
          color: #1e40af !important;
        }
        .workspace-tree.light .ant-tree-switcher { color: #3b82f6 !important; }

        .workspace-center {
          min-width: 0;
          display: flex;
          flex-direction: column;
          overflow: hidden;
          border-right: 1px solid;
          border-left: 1px solid;
        }

        .workspace-root.dark .workspace-center {
          border-color: rgba(129, 166, 255, .2);
          background: rgba(11, 18, 34, .62);
        }

        .workspace-root.light .workspace-center {
          border-color: rgba(30, 64, 175, .1);
          background: rgba(255, 255, 255, .65);
        }

        .workspace-center-header {
          height: 38px;
          padding: 0 14px;
          display: flex;
          align-items: center;
          border-bottom: 1px solid;
          backdrop-filter: blur(8px);
        }

        .workspace-root.dark .workspace-center-header {
          border-color: rgba(129, 166, 255, .24);
          background: rgba(15, 24, 44, .82);
        }

        .workspace-root.light .workspace-center-header {
          border-color: rgba(30, 64, 175, .1);
          background: rgba(255, 255, 255, .76);
        }

        .workspace-center-title {
          display: inline-flex;
          align-items: center;
          gap: 8px;
          font-size: 13px;
          font-weight: 600;
          min-width: 0;
        }

        .workspace-center-content {
          min-width: 0;
          min-height: 0;
          flex: 1;
          overflow: hidden;
        }

        .workspace-right {
          min-width: 0;
          display: flex;
          flex-direction: column;
          border-left: 1px solid;
          backdrop-filter: blur(8px);
        }

        .workspace-right.dark {
          background: rgba(12, 18, 34, .84);
          border-color: rgba(129, 166, 255, .2);
        }

        .workspace-right.light {
          background: rgba(255, 255, 255, .82);
          border-color: rgba(30, 64, 175, .1);
        }

        .workspace-tabs .ant-tabs-nav {
          margin: 0 !important;
          border-bottom: 1px solid;
        }

        .workspace-tabs.dark .ant-tabs-nav {
          background: rgba(15, 24, 44, .88);
          border-color: rgba(129, 166, 255, .2);
        }

        .workspace-tabs.light .ant-tabs-nav {
          background: rgba(250, 252, 255, .95);
          border-color: rgba(30, 64, 175, .1);
        }

        .workspace-tabs .ant-tabs-tab {
          padding: 8px 12px !important;
          transition: transform .06s ease;
        }

        .workspace-tabs .ant-tabs-tab:active {
          transform: scale(.97);
        }

        .workspace-empty {
          height: 100%;
          display: flex;
          align-items: center;
          justify-content: center;
          opacity: .65;
          font-size: 12px;
        }
      `}</style>
    </div>
  );
}
