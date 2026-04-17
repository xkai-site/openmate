import { api } from '@/services/api';
import type {
  ApiResponse,
  TreeNodeResponse,
  TreeGenerateRequest,
  TreeGenerateResponse,
  NodeDecomposeRequest,
  NodeDecomposeResponse,
  RootNodeSummary,
} from '@/types/models';

/**
 * 获取完整树拓扑（轻量，仅含 id/name/status/children）
 * @param rootId 指定根节点 ID，不传则自动查找第一个根节点
 */
export async function getFullTree(
  rootId?: string
): Promise<TreeNodeResponse | null> {
  const response = await api.get<
    ApiResponse<TreeNodeResponse | Record<string, never>>
  >('/tree', {
    params: rootId ? { root_id: rootId } : undefined,
  });
  const data = response.data;
  if (!data || !('id' in data)) {
    return null;
  }
  return data as TreeNodeResponse;
}

/**
 * 一句话生成树结构
 * 接收用户的一句话需求，由 AI 拆解并初始化完整的 Node 树结构
 * 
 * 注意：此操作涉及 LLM 调用，可能需要较长时间，超时设置为 120 秒
 */
export async function generateTree(
  payload: TreeGenerateRequest
): Promise<TreeGenerateResponse> {
  const response = await api.post<ApiResponse<TreeGenerateResponse>>(
    '/tree/generate',
    payload,
    {
      timeout: 120000, // 120 秒超时，因为涉及 AI 拆解
    }
  );
  return response.data;
}

/**
 * 基于已有 Node 的对话历史（Session）和记忆（Memory）触发拆解
 * 
 * 与 generateTree 的区别：
 * - generateTree 基于一句话 demand，无上下文
 * - decomposeNode 从 Node 的完整对话历史 + Memory 提炼需求，上下文感知
 * 
 * 注意：此操作涉及 LLM 调用，超时设置为 120 秒
 */
export async function decomposeNode(
  nodeId: string,
  payload?: NodeDecomposeRequest
): Promise<NodeDecomposeResponse> {
  const response = await api.post<ApiResponse<NodeDecomposeResponse>>(
    `/nodes/${nodeId}/decompose`,
    payload ?? {},
    {
      timeout: 120000,
    }
  );
  return response.data;
}

/**
 * 获取所有根节点摘要列表（用于首页项目管理）
 * 按 updated_at 倒序，每项包含 id/name/status/created_at/updated_at/children_count
 */
export async function listRootNodes(): Promise<RootNodeSummary[]> {
  const response = await api.get<ApiResponse<RootNodeSummary[]>>('/tree/roots');
  return response.data ?? [];
}
