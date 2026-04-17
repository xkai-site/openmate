import { useQuery } from '@tanstack/react-query';
import { Button, Card, Col, Empty, Progress, Row, Space, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useNavigate } from 'react-router-dom';
import StatusTag from '@/components/StatusTag';
import { getTopicLogs, getTopicResults } from '@/services/api/topic';
import type { ExecutionResultResponse } from '@/types/models';
import { usePollingTopic } from '@/hooks/usePollingTopic';
import ControlButtons from './ControlButtons';
import TaskTable from './TaskTable';

interface TopicDetailProps {
  topicId?: string;
  onChanged?: () => void;
}

function TopicDetail({ topicId, onChanged }: TopicDetailProps) {
  const navigate = useNavigate();
  const detailQuery = usePollingTopic(topicId);

  const openNodeInAITree = (nodeId: string) => {
    navigate(`/aitree?nodeId=${encodeURIComponent(nodeId)}`);
  };

  const resultColumns: ColumnsType<ExecutionResultResponse> = [
    { title: 'Task ID', dataIndex: 'task_id', key: 'task_id' },
    {
      title: 'Node ID',
      dataIndex: 'node_id',
      key: 'node_id',
      render: (value: string) => (
        <Button type="link" className="!px-0" onClick={() => openNodeInAITree(value)}>
          {value}
        </Button>
      ),
    },
    {
      title: '结果',
      dataIndex: 'success',
      key: 'success',
      render: (v: boolean) => (v ? '成功' : '失败'),
    },
    { title: '错误信息', dataIndex: 'error', key: 'error', render: (v) => v || '-' },
  ];

  const logsQuery = useQuery({
    queryKey: ['topic', 'logs', topicId],
    queryFn: () => getTopicLogs(topicId as string),
    enabled: Boolean(topicId),
  });

  const resultsQuery = useQuery({
    queryKey: ['topic', 'results', topicId],
    queryFn: () => getTopicResults(topicId as string),
    enabled: Boolean(topicId),
  });

  if (!topicId) {
    return <Empty description="请先在左侧选择 Topic" />;
  }

  return (
    <div className="space-y-4">
      <Card title="Topic 状态">
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <Typography.Text type="secondary">Topic ID:</Typography.Text>
            <Typography.Text code>{detailQuery.data?.id || topicId}</Typography.Text>
            <StatusTag status={String(detailQuery.data?.status || 'unknown')} />
          </div>
          <Progress percent={Math.round(detailQuery.data?.progress_percent ?? 0)} />
          <Row gutter={[12, 12]}>
            <Col span={6}><Card size="small">pending: {detailQuery.data?.pending_tasks ?? 0}</Card></Col>
            <Col span={6}><Card size="small">running: {detailQuery.data?.running_tasks ?? 0}</Card></Col>
            <Col span={6}><Card size="small">completed: {detailQuery.data?.completed_tasks ?? 0}</Card></Col>
            <Col span={6}><Card size="small">failed: {detailQuery.data?.failed_tasks ?? 0}</Card></Col>
          </Row>
          <Space>
            <ControlButtons
              topicId={topicId}
              disabled={detailQuery.isLoading}
              onActionDone={() => {
                void detailQuery.refetch();
                void logsQuery.refetch();
                void resultsQuery.refetch();
                onChanged?.();
              }}
            />
          </Space>
        </div>
      </Card>

      <Card title="任务日志（支持 Retry）">
        <TaskTable
          topicId={topicId}
          loading={logsQuery.isLoading || logsQuery.isFetching}
          items={logsQuery.data ?? []}
          onOpenNode={openNodeInAITree}
          onRetryDone={() => {
            void logsQuery.refetch();
            void detailQuery.refetch();
          }}
        />
      </Card>

      <Card title="执行结果">
        <Table<ExecutionResultResponse>
          rowKey="task_id"
          loading={resultsQuery.isLoading || resultsQuery.isFetching}
          columns={resultColumns}
          dataSource={resultsQuery.data?.results ?? []}
          pagination={false}
        />
      </Card>
    </div>
  );
}

export default TopicDetail;