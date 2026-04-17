import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, Col, Row, Table, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import StatusTag from '@/components/StatusTag';
import { usePagination } from '@/hooks/usePagination';
import { listTopics } from '@/services/api/topic';
import type { TopicStatusResponse } from '@/types/models';
import TopicDetail from './components/TopicDetail';

const columns: ColumnsType<TopicStatusResponse> = [
  { title: 'Topic ID', dataIndex: 'id', key: 'id' },
  { title: 'PlanList ID', dataIndex: 'planlist_id', key: 'planlist_id' },
  { title: '状态', dataIndex: 'status', key: 'status', render: (v) => <StatusTag status={String(v)} /> },
  { title: '队列', dataIndex: 'queue_size', key: 'queue_size' },
  { title: '进度', dataIndex: 'progress_percent', key: 'progress_percent', render: (v) => `${Math.round(Number(v || 0))}%` },
];

function TopicPage() {
  const [selectedTopicId, setSelectedTopicId] = useState<string>();
  const pagination = usePagination(10);

  const listQuery = useQuery({
    queryKey: ['topic', 'list', pagination.pageSize, pagination.offset],
    queryFn: () => listTopics(pagination.pageSize, pagination.offset),
    refetchInterval: 10000,
  });

  const items = listQuery.data?.items ?? [];

  useEffect(() => {
    if (!selectedTopicId && items.length > 0) {
      setSelectedTopicId(items[0].id);
    }
  }, [items, selectedTopicId]);

  return (
    <div className="space-y-4">
      <Typography.Title level={4} className="!mb-0">
        Topic 指挥中心
      </Typography.Title>
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={10}>
          <Card title="Topic 列表">
            <Table<TopicStatusResponse>
              rowKey="id"
              loading={listQuery.isLoading || listQuery.isFetching}
              columns={columns}
              dataSource={items}
              onRow={(record) => ({
                onClick: () => setSelectedTopicId(record.id),
              })}
              rowClassName={(record) => (record.id === selectedTopicId ? 'bg-blue-50' : '')}
              pagination={{
                total: listQuery.data?.total ?? 0,
                current: pagination.current,
                pageSize: pagination.pageSize,
                showSizeChanger: true,
                onChange: pagination.onChange,
              }}
            />
          </Card>
        </Col>

        <Col xs={24} xl={14}>
          <TopicDetail topicId={selectedTopicId} onChanged={() => { void listQuery.refetch(); }} />
        </Col>
      </Row>
    </div>
  );
}

export default TopicPage;