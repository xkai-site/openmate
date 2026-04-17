import { useMemo } from 'react';
import ReactFlow, { Background, Controls, MiniMap, Handle, Position, type NodeProps } from 'reactflow';
import 'reactflow/dist/style.css';
import type { Edge, Node } from 'reactflow';

interface TreeNodeData {
  id: string;
  name?: string;
  status?: string;
  updated_at?: string;
}

interface TreeCanvasProps {
  nodes: Node<TreeNodeData>[];
  edges: Edge[];
  onNodeClick: (nodeId: string) => void;
}

// 节点状态映射颜色
const statusColors: Record<string, { bg: string, border: string, text: string }> = {
  pending: { bg: 'bg-white', border: 'border-slate-200', text: 'text-slate-400' },
  running: { bg: 'bg-blue-50', border: 'border-blue-400', text: 'text-blue-600' },
  completed: { bg: 'bg-emerald-50', border: 'border-emerald-400', text: 'text-emerald-700' },
  failed: { bg: 'bg-rose-50', border: 'border-rose-400', text: 'text-rose-600' },
  unknown: { bg: 'bg-slate-50', border: 'border-slate-200', text: 'text-slate-400' },
};

function TreeNode({ data }: NodeProps<TreeNodeData>) {
  const status = data.status?.toLowerCase() || 'unknown';
  const colors = statusColors[status] || statusColors.unknown;

  // 格式化时间：仅保留 时:分:秒 或 日期
  const formattedTime = useMemo(() => {
    if (!data.updated_at) return '';
    try {
      const date = new Date(data.updated_at);
      return date.toLocaleString('zh-CN', { 
        month: '2-digit', 
        day: '2-digit', 
        hour: '2-digit', 
        minute: '2-digit',
        hour12: false 
      });
    } catch (e) {
      return '';
    }
  }, [data.updated_at]);

  return (
    <div className={`w-[240px] rounded-xl border-2 shadow-sm transition-all duration-300 hover:shadow-md cursor-pointer ${colors.bg} ${colors.border}`}>
      <Handle type="target" position={Position.Left} className="w-2 h-4 rounded-sm !bg-slate-300 border-none" />
      
      <div className="p-4 flex flex-col space-y-1">
        <div className="flex items-start justify-between">
          <span className="font-semibold text-slate-800 line-clamp-2 leading-tight" title={data.name || data.id}>
            {data.name || 'Untitled Node'}
          </span>
          <div className="flex items-center pt-1">
            <span className={`relative flex h-2.5 w-2.5`}>
              {status === 'running' && (
                <span className={`animate-ping absolute inline-flex h-full w-full rounded-full opacity-75 ${colors.text.replace('text', 'bg')}`}></span>
              )}
              <span className={`relative inline-flex rounded-full h-2.5 w-2.5 ${colors.text.replace('text', 'bg')}`}></span>
            </span>
          </div>
        </div>
        
        <div className="text-[10px] text-slate-400 font-medium">
          {formattedTime}
        </div>
      </div>

      <Handle type="source" position={Position.Right} className="w-2 h-4 rounded-sm !bg-slate-300 border-none" />
    </div>
  );
}

function TreeCanvas({ nodes, edges, onNodeClick }: TreeCanvasProps) {
  const nodeTypes = useMemo(() => ({ treeNode: TreeNode }), []);

  return (
    <div className="w-full h-full">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        minZoom={0.1}
        maxZoom={1.5}
        onNodeClick={(_, node) => onNodeClick(node.id)}
        proOptions={{ hideAttribution: true }}
      >
        <Background color="#cbd5e1" gap={24} size={1} />
        <Controls showInteractive={false} className="bg-white/80 backdrop-blur border border-slate-200 shadow-sm rounded-lg" />
        <MiniMap 
          pannable 
          zoomable 
          nodeColor={(n) => {
            const status = n.data?.status?.toLowerCase() || 'unknown';
            if (status === 'running') return '#60a5fa'; // blue-400
            if (status === 'completed') return '#34d399'; // emerald-400
            if (status === 'failed') return '#fb7185'; // rose-400
            return '#cbd5e1'; // slate-300
          }}
          className="border border-slate-200 rounded-lg shadow-sm"
        />
      </ReactFlow>
    </div>
  );
}

export default TreeCanvas;