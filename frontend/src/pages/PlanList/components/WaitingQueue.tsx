import { Table } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { PlanListResponse } from '@/types/models';

interface WaitingQueueProps {
  loading: boolean;
  items: PlanListResponse[];
  total: number;
  current: number;
  pageSize: number;
  onPageChange: (page: number, size: number) => void;
}

const columns: ColumnsType<PlanListResponse> = [
  { title: 'ID', dataIndex: 'id', key: 'id' },
  { title: '名称', dataIndex: 'name', key: 'name', render: (val) => val || '-' },
  { title: '状态', dataIndex: 'status', key: 'status' },
  { title: '任务数', dataIndex: 'task_count', key: 'task_count' },
  { title: '创建时间', dataIndex: 'created_at', key: 'created_at' },
];

function WaitingQueue({ loading, items, total, current, pageSize, onPageChange }: WaitingQueueProps) {
  return (
    <Table<PlanListResponse>
      rowKey="id"
      loading={loading}
      columns={columns}
      dataSource={items}
      pagination={{
        total,
        current,
        pageSize,
        showSizeChanger: true,
        onChange: onPageChange,
      }}
    />
  );
}

export default WaitingQueue;