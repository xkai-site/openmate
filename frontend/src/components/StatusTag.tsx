import { Tag } from 'antd';

interface StatusTagProps {
  status?: string;
}

const colorMap: Record<string, string> = {
  pending: 'default',
  waiting: 'gold',
  running: 'processing',
  completed: 'success',
  failed: 'error',
};

function StatusTag({ status = '-' }: StatusTagProps) {
  const key = status.toLowerCase();
  return <Tag color={colorMap[key] || 'default'}>{status}</Tag>;
}

export default StatusTag;