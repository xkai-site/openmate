import { useQuery } from '@tanstack/react-query';
import { getTopicById } from '@/services/api/topic';

export function usePollingTopic(topicId?: string) {
  const query = useQuery({
    queryKey: ['topic', 'detail', topicId],
    queryFn: () => getTopicById(topicId as string),
    enabled: Boolean(topicId),
  });

  return {
    ...query,
    isRunning: false,
  };
}
