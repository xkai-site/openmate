import { Card, Typography } from 'antd';
import type { SessionMessage } from '@/types/models';

interface ChatBubbleProps {
  message: SessionMessage;
}

function ChatBubble({ message }: ChatBubbleProps) {
  const isUser = message.role === 'user';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-2`}>
      <Card
        size="small"
        className={isUser ? 'max-w-[80%] bg-blue-50' : 'max-w-[80%] bg-white'}
        bodyStyle={{ padding: '8px 12px' }}
      >
        <Typography.Text strong>{isUser ? 'User' : 'Assistant'}</Typography.Text>
        <div className="mt-1 whitespace-pre-wrap break-words">{message.content}</div>
      </Card>
    </div>
  );
}

export default ChatBubble;