import { useState } from 'react';
import { Button, Card, Space, Table, Tabs, Typography, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { PlanListCreate, PlanListResponse } from '@/types/models';
import { usePlanList } from '@/hooks/usePlanList';
import PlanListForm from './components/PlanListForm';
import WaitingQueue from './components/WaitingQueue';

const columns: ColumnsType<PlanListResponse> = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '名称', dataIndex: 'name', key: 'name', render: (val) => val || '-' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '优先级', dataIndex: 'priority', key: 'priority' },
  { title: '任务总数', dataIndex: 'task_count', key: 'task_count' },
  { title: '已完成', dataIndex: 'completed_count', key: 'completed_count' },
  { title: '失败', dataIndex: 'failed_count', key: 'failed_count' },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
];

function PlanListPage() {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { allPagination, waitingPagination, allQuery, waitingQuery, allItems, waitingItems, createMutation } = usePlanList();

  const handleCreate = async (payload: PlanListCreate) => {
    try {
      await createMutation.mutateAsync(payload);
      message.success('PlanList 创建成功');
      setDrawerOpen(false);
    } catch {
      message.error('PlanList 创建失败');
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Typography.Title level={4} className="!mb-0">PlanList</Typography.Title>
        <Space>
          <Button onClick={() => { void allQuery.refetch(); void waitingQuery.refetch(); }}>刷新</Button>
          <Button type="primary" onClick={() => setDrawerOpen(true)}>新建 PlanList</Button>
        </Space>
      </div>

      <Card>
        <Tabs
          items={[
            {
              key: 'all',
              label: '全部列表',
              children: (
                <Table<PlanListResponse>
                  rowKey="id"
                  loading={allQuery.isLoading || allQuery.isFetching}
                  columns={columns}
                  dataSource={allItems}
                  pagination={{
                    total: allQuery.data?.total ?? 0,
                    current: allPagination.current,
                    pageSize: allPagination.pageSize,
                    showSizeChanger: true,
                    onChange: allPagination.onChange,
                  }}
                />
              ),
            },
            {
              key: 'waiting',
              label: '等待队列',
              children: (
                <WaitingQueue
                  loading={waitingQuery.isLoading || waitingQuery.isFetching}
                  items={waitingItems}
                  total={waitingQuery.data?.total ?? 0}
                  current={waitingPagination.current}
                  pageSize={waitingPagination.pageSize}
                  onPageChange={waitingPagination.onChange}
                />
              ),
            },
          ]}
        />
      </Card>

      <PlanListForm
        open={drawerOpen}
        loading={createMutation.isPending}
        onClose={() => setDrawerOpen(false)}
        onSubmit={handleCreate}
      />
    </div>
  );
}

export default PlanListPage;