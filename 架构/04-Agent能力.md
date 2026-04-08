# Agent 能力架构

## 1. 模块目标

能力层定义“Agent 如何在 Node 上完成工作”，核心是把模型调用前后的能力拼装成可配置流水线，而非把能力硬编码到单一 Agent 内。

核心目标：

1. 统一上下文注入策略。
2. 插件化工具注入与执行。
3. skill 注入与组合编排。
4. 对话与记忆的可控存储。

## 2. 能力流水线

建议执行链路：

```text
NodeRef
  -> Context Builder
  -> Tool Resolver
  -> Skill Resolver
  -> Prompt Assembler
  -> Model Executor
  -> Output Parser
  -> Persistence Writer
  -> NextAction Planner
```

每一段都应可插拔、可替换、可观测。

## 3. 上下文注入策略

### 3.1 上下文分层

1. `L0`：当前 Node 最近窗口（高优先）。
2. `L1`：同 Topic 相关节点摘要。
3. `L2`：长期记忆与偏好。
4. `L3`：外部知识或检索结果。

### 3.2 预算控制

1. 上下文预算由模型窗口和成本目标决定。
2. 采用分层截断：优先保留 `L0/L1`。
3. 超预算时优先摘要而非硬截断。

### 3.3 注入策略接口

```ts
interface ContextPolicy {
  build(nodeId: string, budgetTokens: number): Promise<ContextPack>;
}
```

## 4. 工具注入

工具采用注册中心模式：

1. 工具声明能力标签、参数 schema、权限需求。
2. 执行前由权限策略判断是否可用。
3. 工具结果统一结构化回写，支持可审计。

```ts
interface ToolSpec {
  name: string;
  capabilityTags: string[];
  inputSchema: object;
  sideEffect: "none" | "read" | "write" | "external";
}
```

## 5. skill 注入

skill 是可复用的行为包，建议至少包含：

1. `manifest`：skill 名称、版本、依赖、适用场景。
2. `instructions`：行为指导与约束。
3. `hooks`：前处理/后处理扩展点。

skill 解析流程：

1. 按 `capabilityTags` 选候选 skill。
2. 按主题、Node 状态与执行场景重排。
3. 去重与冲突消解（优先级 + 互斥规则）。

## 6. 对话与记忆存储

存储分层：

1. 会话日志：完整、可追溯。
2. 工作记忆：短期状态，任务结束可衰减。
3. 长期记忆：用户偏好与稳定事实。

写入策略：

1. 原始对话全量存档。
2. 长期记忆必须经过提炼与去噪。
3. 敏感字段脱敏后再落盘。

## 7. 安全与治理

1. 能力白名单：按主题/租户控制工具暴露。
2. 输出守护：结构化输出校验，阻止越权动作。
3. 风险操作二次确认：外部写操作需确认策略。
4. 可审计：保留调用链、输入摘要、输出摘要。

## 8. 关键接口

### 8.1 对 Agent 池

1. `BuildExecutionPlan(nodeId) -> plan`
2. `ExecutePlan(ticketId, plan) -> executionResult`

### 8.2 对 VFS

1. `LoadNodeContext(nodeId)`
2. `AppendSession(nodeId, message)`
3. `WriteArtifacts(nodeId, artifacts)`
4. `WriteMemories(topicId, memories)`

### 8.3 对调度层

1. `SuggestNextActions(nodeId, executionResult)`
2. `SuggestPriorityHint(nodeId, hint)`

## 9. 可观测性

关键指标：

1. `context_build_ms`
2. `tool_call_success_rate`
3. `skill_hit_rate`
4. `memory_write_precision_proxy`
5. `end_to_end_success_rate`

关键追踪：

1. 每次执行中记录 `context/tool/skill` 命中明细与耗时。

## 10. MVP 范围

1. 基础 Context Builder
2. 工具注册与调用
3. skill 清单加载与注入
4. 会话存储与基础记忆写入

## 11. 并行开发分工建议

1. A 组：Context Builder + Prompt Assembler。
2. B 组：Tool Registry + Permission Guard。
3. C 组：Skill Resolver + Memory Pipeline。
