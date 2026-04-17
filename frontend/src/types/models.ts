export interface ApiResponse<T> {
  code: number;
  message: string;
  data: T;
}

export interface PaginatedResponse<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}

export type CommonStatus = 'pending' | 'running' | 'completed' | 'failed' | 'waiting' | string;

export interface TaskCreate {
  id: string;
  node_id: string;
  priority?: string;
  customize?: Record<string, unknown>;
}

export type TaskAddRequest = TaskCreate;

export interface TaskResponse {
  id: string;
  node_id: string;
  priority: string;
  customize: Record<string, unknown>;
  retry_count: number;
  max_retries: number;
}

export interface PlanListCreate {
  id: string;
  name?: string;
  demand?: string;
  source?: 'human' | 'ai' | string;
  agent_config_hint?: Record<string, unknown>;
  tasks?: TaskCreate[];
}

export interface PlanListResponse {
  id: string;
  name?: string;
  demand?: string;
  source?: string;
  priority: number;
  status: CommonStatus;
  task_count: number;
  completed_count: number;
  failed_count: number;
  created_at: string;
  initialization_prompt?: string;
}

export interface PlanListDetailResponse extends PlanListResponse {
  tasks: TaskResponse[];
}

export interface TopicStatusResponse {
  id: string;
  planlist_id: string;
  priority: number;
  status: CommonStatus;
  queue_size: number;
  pending_tasks: number;
  running_tasks: number;
  completed_tasks: number;
  failed_tasks: number;
  progress_percent: number;
  agent_status?: string;
  planlist_context?: {
    name?: string;
    demand?: string;
    source?: string;
  };
}

export interface ExecutionResultResponse {
  success: boolean;
  task_id: string;
  node_id: string;
  output: unknown;
  error: string | null;
}

export interface TaskResultsResponse {
  topic_id: string;
  planlist_id: string;
  total_tasks: number;
  completed_tasks: number;
  failed_tasks: number;
  results: ExecutionResultResponse[];
}

export interface TaskLogStep {
  step_index: number;
  instruction: string;
}

export interface TaskLogResponse {
  task_id: string;
  node_id: string;
  logs: TaskLogStep[];
  total_steps: number;
  total_progress: number;
}

export interface QueueStatsResponse {
  active_topics: number;
  waiting_planlists: number;
  max_concurrent: number;
  is_running: boolean;
}

export interface AgentStatsResponse {
  total_agents: number;
  idle_agents: number;
  busy_agents: number;
  error_agents: number;
  max_concurrent_requests: number;
  requests_per_minute: number;
  requests_in_last_minute: number;
}

// ==================== AITree Node 模型 ====================

export interface NodeCreate {
  name?: string;
  parent_id?: string | null;
}

export interface NodeUpdate {
  name?: string;
  status?: CommonStatus;
}

export interface NodeResponse {
  // 基础元数据（每次必返回）
  id: string;
  name: string;
  parent_id?: string | null;
  children_ids: string[];
  status: CommonStatus;
  created_at: string;
  updated_at: string;
  token_usage?: Record<string, number>;


  // 按需加载字段（通过 include 参数控制，不传则为 undefined）
  session?: SessionMessage[];
  memory?: Record<string, unknown>;
  input?: Record<string, unknown>;
  output?: Record<string, unknown>;
  step?: string[];
  progress?: string[];
}

export interface SessionMessage {
  role: 'user' | 'assistant' | string;
  content: string;
  timestamp?: string;
}

export interface TreeNodeResponse {
  id: string;
  name?: string;
  status: CommonStatus;
  updated_at: string;
  children: TreeNodeResponse[];
}

export interface RootNodeSummary {
  id: string;
  name: string;
  status: CommonStatus;
  created_at: string;
  updated_at: string;
  children_count: number;
}

export interface TreeGenerateRequest {
  demand: string;
  tree_name?: string;
}

export interface TreeNodeInfo {
  id: string;
  name: string;
  parent_id?: string | null;
}

export interface TreeGenerateResponse {
  tree_id: string;
  root_node_id: string;
  demand: string;
  nodes: TreeNodeInfo[];
}

export interface NodeDecomposeRequest {
  tree_name?: string;
}

export interface NodeDecomposeResponse {
  root_node_id: string;
  nodes: TreeNodeInfo[];
  planlist_context: string;
  analysis: string;
}

export interface HealthResponse {
  status: string;
  timestamp: string;
}

// ==================== Chat 模型 ====================

export interface MethodTrace {
  method: string;
  request: Record<string, unknown>;
  response: Record<string, unknown>;
  status?: 'start' | 'retry' | 'result' | 'error';
  attempt?: number;
  error?: string;
}


export interface ChatMessage {
  role: 'user' | 'assistant' | 'system' | string;
  content: string;
  thinking?: string;           // 模型思考内容（Interleaved Thinking）
  timestamp?: string;
  usage?: Record<string, number>;
  method_traces?: MethodTrace[];
  tool_traces?: ToolTrace[];   // 新增：原生 Tool Call 轨迹
}

export interface ToolTrace {
  tool: string;
  args: Record<string, unknown>;
  result?: Record<string, unknown>;
  call: 'start' | 'result' | 'error';
  error?: string;
}

export interface ChatRequest {
  node_id?: string | null;
  message: string;
  history?: ChatMessage[];
  system_prompt?: string | null;
  save_session?: boolean;
}

export interface ChatResponse {
  node_id?: string | null;
  reply: string;
  model: string;
  provider: string;
  usage: Record<string, number>;
  memory_written?: Record<string, unknown> | null;
  method_traces?: MethodTrace[] | null;
}

export type StreamPhase = 'reading_node' | 'reasoning' | 'responding' | 'updating_memory' | 'finalizing' | string;


export interface ChatStreamBaseEvent {
  event_id: string;
  ts: string;
  node_id?: string | null;
  turn_id: string;
}

export interface ChatStreamToolCallEvent extends ChatStreamBaseEvent {
  call: 'start' | 'result' | 'error';
  tool: string;
  args?: Record<string, unknown>;
  result?: Record<string, unknown>;
  error?: string;
}

export interface ChatStreamMethodCallEvent extends ChatStreamBaseEvent {
  call: 'start' | 'retry' | 'result' | 'error';
  method: string;
  request: Record<string, unknown>;
  response?: Record<string, unknown>;
  reason?: string;
  error?: string;
  attempt?: number;
}

export interface ChatStreamSummaryEvent extends ChatStreamBaseEvent {
  node_id?: string;
  usage?: Record<string, number>;
  memory_written?: Record<string, unknown> | null;
  method_traces?: MethodTrace[] | null;
  tool_traces?: ToolTrace[] | null;
  model?: string;
  provider?: string;
  elapsed?: number;
}

export interface ChatStreamFatalEvent extends ChatStreamBaseEvent {
  message: string;
  code?: number;
}

