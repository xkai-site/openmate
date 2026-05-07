import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Card, Col, Row, Table, Tooltip, Typography } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { usePagination } from '@/hooks/usePagination';
import { listTopics } from '@/services/api/topic';
import type { TopicStatusResponse } from '@/types/models';
import TopicDetail from './components/TopicDetail';

const columns: ColumnsType<TopicStatusResponse> = [
  {
    title: '名称',
    dataIndex: 'name',
    key: 'name',
    ellipsis: true,
    render: (value: string) => value || '-',
  },
  {
    title: 'Topic ID',
    dataIndex: 'id',
    key: 'id',
    ellipsis: true,
    render: (value: string) => (
      <Tooltip title={value}>
        <Typography.Text code>{value}</Typography.Text>
      </Tooltip>
    ),
  },
  {
    title: 'Workspace',
    dataIndex: 'workspace',
    key: 'workspace',
    ellipsis: true,
    render: (value?: string | null) => value || '-',
  },
  {
    title: '更新时间',
    dataIndex: 'updated_at',
    key: 'updated_at',
    render: (value: string) => (value ? new Date(value).toLocaleString() : '-'),
  },
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
        Topic 浏览维护
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
          <TopicDetail
            topicId={selectedTopicId}
            onChanged={() => {
              setSelectedTopicId(undefined);
              void listQuery.refetch();
            }}
          />
        </Col>
      </Row>
    </div>
  );
}

export default TopicPage;
