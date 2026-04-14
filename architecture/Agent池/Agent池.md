# Agent池架构（当前基线）

## 0. 2026-04-14 补充

调用方对接时，请优先阅读同目录下的 `对接协议.md`。该文档是当前请求/响应与 `model.json` 约定的落地点。

1. 当前主协议已对齐 OpenAI `Responses API`，不再以 `chat/completions` 为主调用契约。
2. `invoke` 的外层网关封装保持为 `{ request_id, node_id, request, timeout_ms?, route_policy? }`。
3. 其中 `request` 对齐 OpenAI `Responses create` 请求体；`model`、`api_key`、`base_url`、并发与默认参数不由上层请求直接传入，而由 `model.json` 中命中的 API 配置注入。
4. `response` 返回完整 OpenAI `response` 对象，同时保留 `output_text / usage / route / timing / error` 作为网关侧标准消费字段。
5. 工具调用已支持协议透传和观测沉淀：上层可提交 `tools`、接收 `function_call`、再通过下一轮 `input` 提交 `function_call_output`；Agent池不在池内执行工具。
6. `model.json.apis[*]` 已扩展出 `request_defaults / headers / pricing`：
   `request_defaults` 用于承载 OpenAI 请求默认参数，
   `headers` 用于 provider 请求头补充，
   `pricing` 用于按 `input/output/cached/reasoning` token 维度估算成本。

## 1. 定位

Agent池的目标定位是 LLM 调用网关与可观测采集面，而不只是“租约分发器”。

它应该直接承接完整模型请求，选择可用通道并完成实际调用，再把标准化响应返回给上层。

核心职责：

1. 接收标准化 LLM 请求并路由到具体 provider/model。
2. 控制并发、超时、重试、失败摘除等执行治理。
3. 托管凭证与 provider 差异，避免上层直接接触底层调用细节。
4. 采集调用级观测数据，包括时延、usage、成本、错误、路由结果。
5. 返回标准化响应，供上层 `service` 进一步解析。

不负责：

1. Topic/Node 优先级决策（由调度层负责）。
2. Prompt 编排、上下文装配、工具注入（由能力层负责）。
3. 业务语义解析与业务结果归因（由 service / Node / Topic 侧负责）。

## 2. 用户与模块边界

用户侧：

1. 只维护 `model.json`。
2. 不手动注册 API，不手动维护租约状态。

模块侧（当前形态）：

1. 调度/能力模块向 Agent池提交标准化请求，而不是自己拿 endpoint 直连 provider。
2. Agent池返回标准化响应，上层只消费自己关心的字段并做解析。
3. 上层不再持有 provider 调用实现、api_key、重试/限流/熔断细节。

## 3. 配置模型（model.json）

```json
{
  "global_max_concurrent": 20,
  "offline_failure_threshold": 3,
  "retry": {
    "max_attempts": 3,
    "base_backoff_ms": 200
  },
  "apis": [
    {
      "api_id": "openai-main",
      "model": "gpt-4.1",
      "base_url": "https://api.openai.com/v1",
      "api_key": "sk-***",
      "max_concurrent": 8,
      "enabled": true
    }
  ]
}
```

语义：

1. `global_max_concurrent`：全局并发上限（可选）。
2. `offline_failure_threshold`：失败摘除阈值。
3. `retry.max_attempts`：单个 invocation 最多尝试次数，包含首尝试；缺省时使用网关默认值。
4. `retry.base_backoff_ms`：重试前的基础退避毫秒数；第 N 次补偿等待为 `N * base_backoff_ms`。
5. `max_concurrent`：单 API 并发上限。
6. `enabled=false`：该 API 进入不可分配状态。

## 4. 状态机

当前基线中，底层仍保留 API 通道状态机，用于治理可用性与并发：

状态：

1. `available`：可分配。
2. `leased`：并发已满，暂不可再分配。
3. `offline`：不可用。

核心迁移：

1. `invoke` 预留执行槽位后 `lease_count +1`，达到上限时 `available -> leased`。
2. 调用完成后 `lease_count -1`，低于上限时 `leased -> available`。
3. 同一 invocation 内允许出现多次 attempt：单次 attempt 失败先释放槽位并落 attempt 记录，再决定是否继续重试。
4. 只有真正反映通道健康恶化的失败才会增加失败计数；达到阈值时转 `offline`。
5. `provider_unreachable / provider_timeout / retryable provider_http_error(5xx)` 计入摘除；`provider_rate_limited / provider_invalid_json / gateway_internal_error` 不计入摘除。
6. 调用成功会清零失败计数，并恢复到 `available/leased` 之一。
7. 配置同步时，未在 `model.json` 中的 API 自动置 `offline`。

追踪链路：

1. 当前主链路：`node_id -> invocation_id -> api_id`。
2. 失败重试与执行细节可继续下钻到 attempt 粒度。

## 5. 并发一致性实现

运行态存储：

1. 使用 SQLite（WAL）。
2. 当前运行时以 Go 实现，Python 侧仅保留契约模型与 CLI adapter。
3. 关键写路径在 `BEGIN IMMEDIATE` 事务中执行，但真实网络调用不放在长事务内。

保证：

1. 并发 `invoke` 不会超发超过 `max_concurrent`/`global_max_concurrent`。
2. Agent池内部按 `reserve -> provider invoke -> attempt finalize -> (optional retry) -> invocation finalize` 闭环执行。
3. attempt finalize 原子完成：释放执行槽位 + 更新健康状态 + 写 attempt 记录。
4. invocation finalize 原子完成：写最终响应/错误，并把顶层结果收敛为一个可消费的 `InvokeResponse`。

## 6. 命令契约（CLI）

统一入口：

1. Go 运行时：`openmate-pool`
2. Python 侧兼容包装：`python -m openmate_pool`

当前命令：

1. `invoke`
输入：标准化 LLM 请求。
输出：单个 invocation 的最终标准化响应 + 路由信息 + usage + timing + status + error。
2. `cap`
输出：容量快照。
3. `records`
输入：可选 `node_id/limit`。
输出：invocation 记录与完整 attempts，可用于回看内部重试与路由细节。
4. `usage`
输入：可选 `node_id/limit`。
输出：基于 invocation 结果聚合出的 usage 视图，包括成功/失败数、attempt/retry 数、tokens、延迟聚合。
5. `sync`
输出：`{ synced: true, capacity }`。

## 7. 可观测性（MVP）

Agent池应作为调用级观测的单一采集点，至少沉淀以下字段：

1. 路由：`provider`、`api_id`、`model`
2. 追踪：`node_id`、`request_id`、`invocation_id`
3. usage：`prompt/completion/total_tokens`
4. timing：`queued_ms`、`latency_ms`、总耗时
5. 执行结果：`success/failure`、错误类型、错误消息、重试次数
6. 成本：`cost_usd`
7. 原始响应与标准化响应之间的映射关系

说明：

1. Agent池负责采集“调用发生了什么”。
2. service 负责解释“这次返回在业务上意味着什么”。

## 8. 当前非目标

1. 复杂路由策略（按成本、按质量动态路由）。
2. 多租户配额治理。
3. 复杂生命周期（warming/draining/preempt）。
4. 业务级聚合报表（仅保留调用级原始记录）。
5. 在 Agent池内做 Prompt 组装、工具编排、业务解析。
