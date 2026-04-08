# Agent 池架构

## 1. 模块定位

Agent 池是执行资源管理层，本质上更接近模型通道池或 API 调用池，而不是预装 skill 的常驻角色池。

它的职责是接收执行请求，将请求分配到可用的模型通道，并管理并发、限流、稳定性与成本。

核心目标：

1. 管理可用执行资源。
2. 提供模型路由与资源分配。
3. 管控并发、速率与基础配额。
4. 记录 usage、耗时与成本。

非目标：

1. 不决定 Topic 或 Node 的优先级。
2. 不负责上下文注入、工具注入、skill 注入与记忆读写。
3. 不理解 Topic/Node 的业务语义，`nodeId` 只用于追踪与关联。

## 2. 核心职责

1. `Registry`：维护可用执行资源的描述与状态。
2. `Allocator`：为执行请求分配可用资源。
3. `Router`：选择合适的模型/API 通道。
4. `QuotaManager`：控制并发、速率与基础预算。
5. `HealthManager`：处理故障、回退与恢复。
6. `UsageCollector`：记录 token、耗时、成本等指标。

## 3. 资源模型

这里的 `Agent` 更接近逻辑执行槽位，而不是拥有固定身份的智能体角色。

因此，Agent 池不应该基于 `skill/tool` 做静态角色划分，也不应该把某类执行资源长期绑定到某类任务上。

在当前阶段，资源状态建议收敛为三种：

1. `idle`：可接新请求。
2. `busy`：执行中。
3. `offline`：不可用。

状态迁移也保持最小化：

1. `idle -> busy -> idle`
2. 异常连续触发时 `busy -> offline`
3. 恢复后 `offline -> idle`

## 4. 数据结构

当前阶段只冻结最小资源模型。

```ts
interface AgentDescriptor {
  agentId: string;
  modelClass: string;
  maxConcurrent: number;
  status: "idle" | "busy" | "offline";
}

interface ExecutionRequest {
  requestId: string;
  nodeId: string;
  preferredModelClass?: string;
  latencyClass?: "interactive" | "background";
  costClass?: "default" | "low_cost";
  timeoutMs?: number;
}

interface DispatchTicket {
  ticketId: string;
  nodeId: string;
  agentId: string;
  leaseMs: number;
}

interface CapacitySnapshot {
  total: number;
  idle: number;
  busy: number;
  throttled: boolean;
  updatedAt: string;
}
```

说明：

1. `AgentDescriptor` 只描述资源特征，不携带 `skill/tool` 业务能力信息。
2. `ExecutionRequest` 只表达资源偏好与执行约束，不表达上下文注入细节。
3. `nodeId` 在 Agent 池中主要承担追踪作用，而不是资源决策依据。

## 5. 分配与路由流程

Agent 池的分配流程建议保持简单：

```text
1) 接收 ExecutionRequest
2) 过滤可用资源（status / 并发容量）
3) 按模型偏好、延迟目标、成本目标进行路由
4) 检查限流与预算
5) 发放 DispatchTicket
6) 执行完成后记录 usage 并释放资源
```

路由维度建议：

1. 模型类型优先。
2. 交互延迟优先。
3. 成本控制优先。
4. 稳定性优先。

这里的“路由”只解决“把请求送到哪里执行”，不解决“请求该执行什么内容”。

## 6. 与其他模块的边界

模块边界建议明确成三句话：

1. 调度层决定“先执行哪个 Node”。
2. 能力层决定“如何构造执行内容”。
3. Agent 池决定“把这次执行放到哪个模型通道上”。

因此：

1. 调度层不管理底层模型并发。
2. 能力层不管理资源限流与模型健康。
3. Agent 池不接管 `context/tool/skill/memory` 的具体逻辑。

## 7. 限流与故障处理

当前阶段建议只保留最必要的资源保护能力。

限流与配额：

1. 全局并发上限。
2. 模型级并发或调用频率限制。
3. 基础预算保护。

故障处理：

1. API 超时：快速失败。
2. 速率限制：退避后重试或交还调度层。
3. 上游不可用：切换候选模型或标记资源不可用。
4. 长时执行中断：依据 `lease` 回收资源。

当前阶段不把以下内容放入主设计：

1. 多租户配额。
2. 区域路由与区域回退。
3. 复杂预热与摘流控制。
4. 主题级资源配额。

## 8. 关键接口

### 8.1 对调度层

1. `AcquireAgent(request: ExecutionRequest) -> DispatchTicket | NoCapacity`
2. `ReleaseAgent(ticketId, usage, resultSummary)`
3. `GetCapacitySnapshot()`

### 8.2 对能力层

1. `Execute(ticketId, modelInput)`
2. `Abort(ticketId, reason)`
3. `ReportUsage(ticketId, usage)`

说明：

1. `AcquireAgent` 面向的是执行请求，而不是 `Node` 语义本身。
2. `Execute` 面向的是已经完成注入和拼装后的执行内容。
3. `Heartbeat` 在当前阶段不是必须接口，后续再视长任务需要决定是否引入。

## 9. 可观测性

关键指标：

1. `agent_utilization`
2. `acquire_latency_ms`
3. `throttle_rate`
4. `error_rate_by_model`
5. `cost_per_successful_execution`

关键追踪：

1. `nodeId -> ticketId -> agentId -> modelCallId`

## 10. MVP 范围

MVP 只需要覆盖以下内容：

1. 基础资源注册与状态管理。
2. 简单模型路由。
3. 并发门控与基础限流。
4. 基础故障回退。
5. usage 统计与追踪。

## 11. 后续扩展

这些能力明确属于后续扩展，而不是当前阶段重点：

1. `cold / warming / draining` 等复杂生命周期。
2. 多租户资源治理。
3. 多区域与容灾路由。
4. 更复杂的 lease / heartbeat / 抢占机制。

## 12. 并行开发分工建议

1. A 组：Registry + Allocator。
2. B 组：Router + QuotaManager。
3. C 组：HealthManager + UsageCollector。
