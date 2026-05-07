import { useMutation, useQuery } from '@tanstack/react-query';
import { Button, Card, Descriptions, Empty, Popconfirm, Space, Table, Tag, Typography, message } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { useNavigate } from 'react-router-dom';
import StatusTag from '@/components/StatusTag';
import { deleteTopic, listTopicNodes } from '@/services/api/topic';
import type { NodeResponse } from '@/types/models';
import { usePollingTopic } from '@/hooks/usePollingTopic';

interface TopicDetailProps {
  topicId?: string;
  onChanged?: (topicId: string) => void;
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : '-';
}

function TopicDetail({ topicId, onChanged }: TopicDetailProps) {
  const navigate = useNavigate();
  const detailQuery = usePollingTopic(topicId);
  const topic = detailQuery.data;
  const rootNodeId = topic?.root_node_id;

  const nodesQuery = useQuery({
    queryKey: ['topic', 'nodes', topicId],
    queryFn: () => listTopicNodes(topicId as string),
    enabled: Boolean(topicId),
  });

  const deleteMutation = useMutation({
    mutationFn: (deleteTopicId: string) => deleteTopic(deleteTopicId),
    onSuccess: (_, deletedTopicId) => {
      message.success('Topic 已删除');
      onChanged?.(deletedTopicId);
    },
  });

  const openTopicInAITree = (rootNodeId: string) => {
    navigate(`/aitree?rootId=${encodeURIComponent(rootNodeId)}`);
  };

  const openNodeWorkspace = (nodeId: string) => {
    navigate(`/workspace/${encodeURIComponent(nodeId)}`);
  };

  const nodeColumns: ColumnsType<NodeResponse> = [
    {
      title: '节点名称',
      dataIndex: 'name',
      key: 'name',
      ellipsis: true,
      render: (value: string, record) => (
        <Button
          type="link"
          className="!px-0"
          disabled={!rootNodeId}
          onClick={() => rootNodeId && openTopicInAITree(rootNodeId)}
        >
          {value || record.id}
        </Button>
      ),
    },
    {
      title: 'Node ID',
      dataIndex: 'id',
      key: 'id',
      ellipsis: true,
      render: (value: string) => <Typography.Text code>{value}</Typography.Text>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      render: (value: string) => <StatusTag status={String(value)} />,
    },
    {
      title: '子节点',
      dataIndex: 'children_ids',
      key: 'children_ids',
      render: (value?: string[]) => value?.length ?? 0,
    },
    {
      title: '更新时间',
      dataIndex: 'updated_at',
      key: 'updated_at',
      render: (value: string) => formatTime(value),
    },
    {
      title: '入口',
      key: 'actions',
      render: (_, record) => (
        <Space>
          <Button
            size="small"
            disabled={!rootNodeId}
            onClick={() => rootNodeId && openTopicInAITree(rootNodeId)}
          >
            AITree
          </Button>
          <Button size="small" onClick={() => openNodeWorkspace(record.id)}>
            Workspace
          </Button>
        </Space>
      ),
    },
  ];

  if (!topicId) {
    return <Empty description="请先在左侧选择 Topic" />;
  }
  return (
    <div className="space-y-4">
      <Card
        title="Topic 详情"
        extra={
          <Popconfirm
            title="删除 Topic"
            description="将删除该 Topic 及其节点，确认继续？"
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={() => topicId && deleteMutation.mutate(topicId)}
          >
            <Button danger loading={deleteMutation.isPending}>
              删除
            </Button>
          </Popconfirm>
        }
      >
        <Descriptions column={1} size="small" bordered>
          <Descriptions.Item label="Topic ID">
            <Typography.Text code>{topic?.id || topicId}</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="名称">{topic?.name || '-'}</Descriptions.Item>
          <Descriptions.Item label="Root Node">
            {topic?.root_node_id ? (
              <Button type="link" className="!px-0" onClick={() => openTopicInAITree(topic.root_node_id)}>
                {topic.root_node_id}
              </Button>
            ) : (
              '-'
            )}
          </Descriptions.Item>
          <Descriptions.Item label="Workspace">{topic?.workspace || '-'}</Descriptions.Item>
          <Descriptions.Item label="标签">
            {topic?.tags?.length ? topic.tags.map((tag) => <Tag key={tag}>{tag}</Tag>) : '-'}
          </Descriptions.Item>
          <Descriptions.Item label="描述">{topic?.description || '-'}</Descriptions.Item>
          <Descriptions.Item label="创建时间">{formatTime(topic?.created_at)}</Descriptions.Item>
          <Descriptions.Item label="更新时间">{formatTime(topic?.updated_at)}</Descriptions.Item>
        </Descriptions>
      </Card>

      <Card title="Topic 节点">
        <Table<NodeResponse>
          rowKey="id"
          loading={nodesQuery.isLoading || nodesQuery.isFetching}
          columns={nodeColumns}
          dataSource={nodesQuery.data ?? []}
          pagination={{ pageSize: 10, showSizeChanger: true }}
        />
      </Card>
    </div>
  );
}

export default TopicDetail;
