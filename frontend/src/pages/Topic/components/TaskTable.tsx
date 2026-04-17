import { useMutation } from '@tanstack/react-query';
import { Button, Space, Table, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { retryTask } from '@/services/api/topic';
import type { TaskLogResponse } from '@/types/models';

interface TaskTableProps {
  topicId?: string;
  loading?: boolean;
  items: TaskLogResponse[];
  onRetryDone?: () => void;
  onOpenNode?: (nodeId: string) => void;
}

function TaskTable({ topicId, loading = false, items, onRetryDone, onOpenNode }: TaskTableProps) {
  const retryMutation = useMutation({
    mutationFn: async (taskId: string) => {
      if (!topicId) throw new Error('缺少 topicId');
      return retryTask(topicId, taskId);
    },
    onSuccess: () => {
      message.success('任务已重新入队');
      onRetryDone?.();
    },
  });

  const columns: ColumnsType<TaskLogResponse> = [
    { title: 'Task ID', dataIndex: 'task_id', key: 'task_id' },
    {
      title: 'Node ID',
      dataIndex: 'node_id',
      key: 'node_id',
      render: (value: string) => (
        <Button type="link" className="!px-0" onClick={() => onOpenNode?.(value)}>
          {value}
        </Button>
      ),
    },
    { title: '步骤数', dataIndex: 'total_steps', key: 'total_steps' },
    { title: '进度计数', dataIndex: 'total_progress', key: 'total_progress' },
    {
      title: '操作',
      key: 'actions',
      render: (_, record) => (
        <Space>
          <Button size="small" disabled={!topicId} loading={retryMutation.isPending} onClick={() => retryMutation.mutate(record.task_id)}>
            Retry
          </Button>
        </Space>
      ),
    },
  ];

  return <Table rowKey="task_id" loading={loading} columns={columns} dataSource={items} pagination={false} />;
}

export default TaskTable;