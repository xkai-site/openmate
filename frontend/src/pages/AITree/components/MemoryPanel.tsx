import { useQuery } from '@tanstack/react-query';
import { Descriptions, Empty, Spin } from 'antd';
import { DatabaseOutlined } from '@ant-design/icons';
import { getNode } from '@/services/api/nodes';

export interface MemoryPanelProps {
  nodeId: string;
  /** 每次 AI 回复后外部递增，触发重新拉取 */
  refetchKey?: number;
}

function MemoryPanel({ nodeId, refetchKey = 0 }: MemoryPanelProps) {
  // 先获取当前节点的 parent_id
  const nodeQuery = useQuery({
    queryKey: ['node-base', nodeId],
    queryFn: () => getNode(nodeId),
    enabled: !!nodeId,
  });

  const parentId = nodeQuery.data?.parent_id;

  // 拉取父节点 memory，refetchKey 变化时触发
  const parentQuery = useQuery({
    queryKey: ['node-memory', parentId, refetchKey],
    queryFn: () => getNode(parentId!, 'memory'),
    enabled: !!parentId,
  });

  const isLoading = nodeQuery.isLoading || (!!parentId && parentQuery.isLoading);

  if (isLoading) {
    return (
      <div className="h-full flex items-center justify-center p-4">
        <Spin tip="加载 Memory..." />
      </div>
    );
  }

  if (!parentId) {
    return (
      <div className="p-4">
        <Empty description="根节点无父节点 Memory" image={Empty.PRESENTED_IMAGE_SIMPLE} />
      </div>
    );
  }

  const entries = Object.entries(parentQuery.data?.memory ?? {});

  if (entries.length === 0) {
    return (
      <div className="p-4">
        <Empty description="暂无 Memory 数据" image={Empty.PRESENTED_IMAGE_SIMPLE} />
        <p className="text-center text-xs mt-2 opacity-50">Memory 由 AI 在对话中自动写入</p>
      </div>
    );
  }

  return (
    <div className="p-3">
      <div className="flex items-center gap-1.5 mb-2">
        <DatabaseOutlined className="text-amber-400" />
        <span className="text-xs font-semibold opacity-70">父节点 Memory</span>
      </div>
      <Descriptions
        bordered
        size="small"
        column={1}
        items={entries.map(([key, value]) => ({
          key,
          label: key,
          children: typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value),
        }))}
      />
    </div>
  );
}

export default MemoryPanel;
