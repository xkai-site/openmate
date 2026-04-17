import { useEffect } from 'react';
import { Button, Space, Typography, Spin } from 'antd';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { useAITree } from '@/hooks/useAITree';
import TreeCanvas from './components/TreeCanvas';
import { ArrowLeftOutlined, ReloadOutlined } from '@ant-design/icons';

function AITreePage() {
  const [searchParams] = useSearchParams();
  // rootId 是根节点 ID（如 tree_xxx_root），由 Home 页生成树后传入
  const rootId = searchParams.get('rootId') || undefined;
  const { treeQuery, nodes, edges } = useAITree(rootId);
  const navigate = useNavigate();

  return (
    <div className="h-screen w-full flex flex-col bg-slate-50">
      {/* 顶部导航栏 */}
      <div className="h-14 bg-white border-b border-slate-200 flex items-center justify-between px-6 shadow-sm z-10">
        <div className="flex items-center space-x-4">
          <Button 
            type="text" 
            icon={<ArrowLeftOutlined />} 
            onClick={() => navigate('/')}
            className="text-slate-500 hover:text-slate-800 hover:bg-slate-100"
          />
          <div className="flex items-center space-x-2">
            <div className="w-6 h-6 rounded bg-gradient-to-tr from-blue-500 to-indigo-500 flex items-center justify-center">
              <span className="text-white font-bold text-xs italic">A</span>
            </div>
            <Typography.Title level={5} className="!mb-0 !mt-0 text-slate-700">AITree Overview</Typography.Title>
          </div>
        </div>
        <Space>
          <Button 
            type="default" 
            icon={<ReloadOutlined />} 
            onClick={() => void treeQuery.refetch()}
            className="border-slate-300 text-slate-600 hover:text-blue-600 hover:border-blue-500"
          >
            Refresh
          </Button>
        </Space>
      </div>

      {/* 画布区域 */}
      <div className="flex-1 relative bg-slate-50/50">
        {(treeQuery.isLoading || treeQuery.isFetching) && nodes.length === 0 ? (
          <div className="absolute inset-0 flex items-center justify-center bg-white/50 backdrop-blur-sm z-10">
            <Spin size="large" tip="Loading tree structure..." />
          </div>
        ) : null}
        
        <TreeCanvas
          nodes={nodes}
          edges={edges}
          onNodeClick={(nodeId) => {
            navigate(`/workspace/${nodeId}`);
          }}
        />
      </div>
    </div>
  );
}

export default AITreePage;