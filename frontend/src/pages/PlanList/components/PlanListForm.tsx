import { Button, Drawer, Form, Input, Select, Space } from 'antd';
import type { PlanListCreate } from '@/types/models';

interface PlanListFormProps {
  open: boolean;
  loading?: boolean;
  onClose: () => void;
  onSubmit: (payload: PlanListCreate) => Promise<void>;
}

function PlanListForm({ open, loading = false, onClose, onSubmit }: PlanListFormProps) {
  const [form] = Form.useForm<PlanListCreate>();

  const handleFinish = async (values: PlanListCreate) => {
    await onSubmit({
      ...values,
      source: values.source || 'human',
      tasks: values.tasks || [],
      agent_config_hint: values.agent_config_hint || {},
    });
    form.resetFields();
  };

  return (
    <Drawer open={open} onClose={onClose} title="新建 PlanList" width={520} destroyOnHidden>
      <Form form={form} layout="vertical" onFinish={(values) => { void handleFinish(values); }}>
        <Form.Item label="ID" name="id" rules={[{ required: true, message: '请输入唯一 ID' }]}>
          <Input placeholder="plan_001" />
        </Form.Item>
        <Form.Item label="名称" name="name">
          <Input placeholder="示例计划" />
        </Form.Item>
        <Form.Item label="需求描述" name="demand">
          <Input.TextArea rows={4} placeholder="完成数据分析任务" />
        </Form.Item>
        <Form.Item label="来源" name="source" initialValue="human">
          <Select options={[{ label: 'human', value: 'human' }, { label: 'ai', value: 'ai' }]} />
        </Form.Item>
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" htmlType="submit" loading={loading}>创建</Button>
        </Space>
      </Form>
    </Drawer>
  );
}

export default PlanListForm;