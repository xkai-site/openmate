import { useMutation } from '@tanstack/react-query';
import { Button, Space, message } from 'antd';
import { deleteTopic, executeAllTasks, executeNextTask, pauseTopic, resumeTopic } from '@/services/api/topic';

interface ControlButtonsProps {
  topicId?: string;
  disabled?: boolean;
  onActionDone?: () => void;
}

function ControlButtons({ topicId, disabled = false, onActionDone }: ControlButtonsProps) {
  const runMutation = useMutation({
    mutationFn: async (action: 'execute' | 'execute-all' | 'pause' | 'resume' | 'delete') => {
      if (!topicId) throw new Error('缺少 topicId');
      if (action === 'execute') return executeNextTask(topicId);
      if (action === 'execute-all') return executeAllTasks(topicId);
      if (action === 'pause') return pauseTopic(topicId);
      if (action === 'resume') return resumeTopic(topicId);
      return deleteTopic(topicId);
    },
    onSuccess: (_, action) => {
      const textMap: Record<string, string> = {
        execute: '已触发执行下一个任务',
        'execute-all': '已触发执行全部任务',
        pause: '已暂停 Topic',
        resume: '已恢复 Topic',
        delete: '已清理 Topic',
      };
      message.success(textMap[action]);
      onActionDone?.();
    },
  });

  const loading = runMutation.isPending;

  return (
    <Space wrap>
      <Button disabled={disabled || !topicId} loading={loading} onClick={() => runMutation.mutate('execute')}>
        执行下一个
      </Button>
      <Button disabled={disabled || !topicId} loading={loading} onClick={() => runMutation.mutate('execute-all')}>
        执行全部
      </Button>
      <Button disabled={disabled || !topicId} loading={loading} onClick={() => runMutation.mutate('pause')}>
        暂停
      </Button>
      <Button disabled={disabled || !topicId} loading={loading} onClick={() => runMutation.mutate('resume')}>
        恢复
      </Button>
      <Button danger disabled={disabled || !topicId} loading={loading} onClick={() => runMutation.mutate('delete')}>
        清理
      </Button>
    </Space>
  );
}

export default ControlButtons;