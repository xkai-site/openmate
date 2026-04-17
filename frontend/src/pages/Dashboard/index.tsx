import { useQuery } from '@tanstack/react-query';
import { Button, Card, Col, Row, Typography } from 'antd';
import { getAgentStats, getQueueStats } from '@/services/api/stats';
import StatsCard from '@/components/StatsCard';

function DashboardPage() {
  const queueQuery = useQuery({
    queryKey: ['stats', 'queue'],
    queryFn: getQueueStats,
    refetchInterval: 10000,
  });

  const agentQuery = useQuery({
    queryKey: ['stats', 'agent'],
    queryFn: getAgentStats,
    refetchInterval: 10000,
  });

  const loading = queueQuery.isLoading || agentQuery.isLoading;

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Typography.Title level={4} className="!mb-0">
          Dashboard
        </Typography.Title>
        <Button onClick={() => { void queueQuery.refetch(); void agentQuery.refetch(); }}>刷新</Button>
      </div>

      <Card title="队列统计">
        <Row gutter={[16, 16]}>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="活跃 Topic" value={queueQuery.data?.active_topics ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="等待 PlanList" value={queueQuery.data?.waiting_planlists ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="最大并发" value={queueQuery.data?.max_concurrent ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="队列运行状态" value={queueQuery.data?.is_running ? '运行中' : '已停止'} /></Col>
        </Row>
      </Card>

      <Card title="Agent 统计">
        <Row gutter={[16, 16]}>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="总 Agent" value={agentQuery.data?.total_agents ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="空闲 Agent" value={agentQuery.data?.idle_agents ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="忙碌 Agent" value={agentQuery.data?.busy_agents ?? 0} /></Col>
          <Col xs={24} md={12} lg={6}><StatsCard loading={loading} title="异常 Agent" value={agentQuery.data?.error_agents ?? 0} /></Col>
        </Row>
      </Card>
    </div>
  );
}

export default DashboardPage;