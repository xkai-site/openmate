import { Card, Statistic } from 'antd';

interface StatsCardProps {
  title: string;
  value: number | string;
  suffix?: string;
  loading?: boolean;
}

function StatsCard({ title, value, suffix, loading = false }: StatsCardProps) {
  return (
    <Card size="small">
      <Statistic title={title} value={value} suffix={suffix} loading={loading} />
    </Card>
  );
}

export default StatsCard;