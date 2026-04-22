import { api } from '@/services/api';
import type { ApiResponse, NodeCreate, NodeResponse, NodeUpdate, ProcessItem, SessionMessage } from '@/types/models';

/**
 * 创建节点
 * ID 由后端自动生成，前端只需提供 name 和可选的 parent_id
 */
export async function createNode(payload: NodeCreate): Promise<NodeResponse> {
  const response = await api.post<ApiResponse<NodeResponse>>('/nodes', payload);
  return response.data;
}

/**
 * 获取节点信息（按需加载）
 * @param nodeId 节点 ID
 * @param include 按需加载的字段，逗号分隔：session,memory,input,output,process
 */
export async function getNode(
  nodeId: string,
  include?: string
): Promise<NodeResponse> {
  const response = await api.get<ApiResponse<NodeResponse>>(`/nodes/${nodeId}`, {
    params: include ? { include } : undefined,
  });
  return response.data;
}

/**
 * 获取节点详情（包含 session 和 memory）
 * 快捷方法，等价于 getNode(nodeId, 'session,memory')
 */
export async function getNodeDetail(nodeId: string): Promise<NodeResponse> {
  return getNode(nodeId, 'session,memory,input,output');
}

/**
 * 更新节点（PATCH 局部更新）
 * 只支持更新 name 和 status
 */
export async function updateNode(
  nodeId: string,
  payload: NodeUpdate
): Promise<null> {
  const response = await api.patch<ApiResponse<null>>(`/nodes/${nodeId}`, payload);
  return response.data;
}

/**
 * 删除节点
 */
export async function deleteNode(nodeId: string): Promise<null> {
  const response = await api.delete<ApiResponse<null>>(`/nodes/${nodeId}`);
  return response.data;
}

/**
 * 获取节点 Session 对话历史
 * 通过 include=session 获取，返回 session 数组
 */
export async function getNodeSession(nodeId: string): Promise<SessionMessage[]> {
  const response = await getNode(nodeId, 'session');
  return response.session ?? [];
}

/**
 * 获取节点 Memory
 * 通过 include=memory 获取，返回 memory 对象
 */
export async function getNodeMemory(
  nodeId: string
): Promise<Record<string, unknown>> {
  const response = await getNode(nodeId, 'memory');
  return response.memory ?? {};
}

/**
 * 获取节点执行过程
 * 通过 include=process 获取
 */
export async function getNodeExecution(nodeId: string): Promise<{
  process?: ProcessItem[];
}> {
  const response = await getNode(nodeId, 'process');
  return {
    process: response.process,
  };
}
