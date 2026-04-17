import { useQuery } from '@tanstack/react-query';
import { getTopicById } from '@/services/api/topic';

const RUNNING_STATUS = new Set(['running']);

export function usePollingTopic(topicId?: string) {
  const query = useQuery({
    queryKey: ['topic', 'detail', topicId],
    queryFn: () => getTopicById(topicId as string),
    enabled: Boolean(topicId),
    refetchInterval: (ctx) => {
      const status = String(ctx.state.data?.status || '').toLowerCase();
      return RUNNING_STATUS.has(status) ? 3000 : false;
    },
  });

  return {
    ...query,
    isRunning: RUNNING_STATUS.has(String(query.data?.status || '').toLowerCase()),
  };
}