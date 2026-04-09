# Agent 池架构

## 1. 模块定位

Agent 池是执行资源管理层，本质上更接近模型通道池或 API 调用池。
它默认不在资源层固化 skill / prompt 角色，但允许上层在能力装配时组织出不同执行风格。

它的职责是接收执行请求，将请求分配到可用的模型通道，并管理并发、限流、稳定性与成本。

核心目标：

1. 管理可用执行资源。
2. 提供资源分配，并预留通道选择扩展口。
3. 管控并发、速率与基础配额。
4. 记录原始 usage、耗时与成本。

非目标：

1. 不决定 Topic 或 Node 的优先级。
2. 不负责上下文注入、工具注入、skill 注入与记忆读写。
3. 不理解 Topic/Node 的业务语义，`node_id` 只用于追踪与关联。
4. 不负责 Topic / Node 级别的业务计量归因与聚合。

## 2. 核心职责

1. `Registry`：维护可用执行资源的描述与状态。
2. `Allocator`：为执行请求分配可用资源。
3. `QuotaManager`：控制并发、速率与基础预算。
4. `HealthManager`：处理故障、回退与恢复。
5. `UsageCollector`：记录原始 token、耗时、成本等指标。

## 3. 资源模型

这里的 `Agent` 更接近逻辑执行槽位，而不是只能拥有单一固定身份的智能体角色。

因此，Agent 池默认不基于 `skill/tool` 做静态资源划分，也不建议把某类执行资源长期硬绑定到某类任务上；
如果业务上需要不同 Agent 风格，优先在能力层通过 Prompt、Build 等装配方式塑造。

在当前阶段，资源状态建议收敛为三种：

1. `available`：在线，且当前并发占用尚未达到 `max_concurrent`。
2. `leased`：在线，但当前并发占用已经达到 `max_concurrent`，暂时不能再接新请求。
3. `offline`：不可用。

每个 Agent 还需要维护一个运行时并发计数（可理解为租约信号量）：

1. 初始为 `0`。
2. 每发放一个租约，计数 `+1`。
3. 每释放一个租约，计数 `-1`。
4. 只要计数 `< max_concurrent`，该 Agent 仍可继续承接新请求。
5. 只有当计数达到 `max_concurrent` 时，才进入“无剩余容量”的状态。

状态迁移也保持最小化：

1. `available -> available`：发放租约后，若并发计数仍小于 `max_concurrent`。
2. `available -> leased`：发放租约后，并发计数恰好达到 `max_concurrent`。
3. `leased -> available`：释放租约后，并发计数重新低于 `max_concurrent`。
4. 在线状态异常连续触发时 `available / leased -> offline`
5. 恢复后 `offline -> available`

## 4. 数据结构

当前阶段只冻结最小资源模型。

技术实现默认采用 `Python + Pydantic`，因此这里的数据结构使用 Python 风格命名与类型表达。

```python
from __future__ import annotations

from datetime import datetime
from enum import StrEnum
from typing import Any

from pydantic import BaseModel


class AgentStatus(StrEnum):
    AVAILABLE = "available"
    LEASED = "leased"
    OFFLINE = "offline"


class AgentDescriptor(BaseModel):
    agent_id: str
    model_class: str
    max_concurrent: int
    status: AgentStatus


class AgentRuntimeState(BaseModel):
    agent_id: str
    lease_count: int = 0


class ExecutionRequest(BaseModel):
    request_id: str
    node_id: str
    timeout_ms: int | None = None
    route_hint: dict[str, Any] | None = None


class DispatchTicket(BaseModel):
    ticket_id: str
    node_id: str
    agent_id: str
    lease_ms: int


class CapacitySnapshot(BaseModel):
    total_agents: int
    total_slots: int
    available_slots: int
    leased_slots: int
    offline_agents: int
    throttled: bool
    updated_at: datetime
```

说明：

1. `AgentDescriptor` 只描述资源特征，不直接携带 `skill/tool/prompt` 级别的执行人格配置。
2. `AgentRuntimeState` 是运行时状态，负责记录当前已发放的租约数量；是否还能继续接单，要结合 `lease_count` 与 `max_concurrent` 一起判断。
3. `ExecutionRequest` 只表达执行约束，并为未来的通道选择策略预留扩展口。
4. `node_id` 在 Agent 池中主要承担追踪作用，而不是资源决策依据。
5. `leased` 表示该 Agent 当前并发额度已满，而不是“只要有一个请求在跑就整机不可再分配”。
6. `CapacitySnapshot` 以 slot 视角表达容量，更适合和 `max_concurrent` 语义对齐。
7. 这里展示的是模型层定义，用于表达强类型边界，不代表已经冻结完整实现细节。

## 5. 分配流程

Agent 池的分配流程建议保持简单：

```text
1) 接收 ExecutionRequest
2) 过滤可用资源（status / `lease_count < max_concurrent`）
3) 选择一个当前可用的执行通道
4) 检查限流与预算
5) 发放 DispatchTicket，并将对应 Agent 的 `lease_count += 1`
6) 若 `lease_count == max_concurrent`，则将该 Agent 标记为 `leased`
7) 执行完成后记录 usage，释放资源并执行 `lease_count -= 1`
8) 若释放后 `lease_count < max_concurrent`，则恢复为 `available`
```

这里的“通道选择”只解决“把请求送到哪里执行”，不解决“请求该执行什么内容”。

当前阶段不提前冻结具体策略，只保留扩展口。

## 6. 与其他模块的边界

模块边界建议明确成三句话：

1. 调度层决定“先执行哪个 Node”。
2. 能力层决定“如何构造执行内容”。
3. Agent 池决定“把这次执行放到哪个模型通道上”。

因此：

1. 调度层不管理底层模型并发。
2. 能力层不管理资源限流与模型健康，但可以决定本次执行采用什么 Prompt 化工作风格。
3. Agent 池不接管 `context/tool/skill/memory` 的具体逻辑。
4. Agent 池只输出原始计量结果，业务计量由 Node / Topic 承接。

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
2. 复杂通道选择策略。
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
4. Python 实现中，接口参数与返回值建议直接围绕 Pydantic 模型组织。

## 9. 可观测性

这里的可观测性以原始计量为主，不承担业务归因。

关键指标：

1. `agent_utilization`
2. `acquire_latency_ms`
3. `throttle_rate`
4. `error_rate_by_model`
5. `cost_per_successful_execution`

关键追踪：

1. `node_id -> ticket_id -> agent_id -> model_call_id`

说明：

1. Agent 池负责记录一次模型调用真实发生了什么。
2. Node / Topic 负责把这些原始计量聚合成业务视角的数据。

## 10. MVP 范围

MVP 只需要覆盖以下内容：

1. 基础资源注册与状态管理。
2. 基础资源分配。
3. 并发门控与基础限流。
4. 基础故障回退。
5. usage 统计与追踪。

## 11. 后续扩展

这些能力明确属于后续扩展，而不是当前阶段重点：

1. `cold / warming / draining` 等复杂生命周期。
2. 多租户资源治理。
3. 复杂通道选择与模型路由。
4. 更复杂的 lease / heartbeat / 抢占机制。

## 12. 并行开发分工建议

1. A 组：Registry + Allocator。
2. B 组：QuotaManager + HealthManager。
3. C 组：UsageCollector + 通道选择扩展口预留。
