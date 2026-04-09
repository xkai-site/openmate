# Agent池架构（当前基线）

## 1. 定位

Agent池是执行资源管理层，本质是 API 通道池与限流状态机：

1. 接收执行请求并分配可用 API 通道。
2. 控制并发与基础配额。
3. 维护健康状态（失败计数、自动摘除）。
4. 记录原始 usage（token、耗时、成本）。

不负责：

1. Topic/Node 优先级决策（由调度层负责）。
2. Prompt/上下文/工具注入（由能力层负责）。
3. 业务语义归因（由 Node/Topic 侧聚合）。

## 2. 用户与模块边界

用户侧：

1. 只维护 `model.json`。
2. 不手动注册 API，不手动维护租约状态。

模块侧（内部调用）：

1. `get`：索取一次 API 租约。
2. `done`：归还租约并上报执行结果。
3. `cap`：查询容量快照。
4. `tickets`：查询活动租约。
5. `usage`：查询原始 usage 记录。
6. `sync`：触发配置同步。

## 3. 配置模型（model.json）

```json
{
  "global_max_concurrent": 20,
  "offline_failure_threshold": 3,
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
3. `max_concurrent`：单 API 并发上限。
4. `enabled=false`：该 API 进入不可分配状态。

## 4. 状态机

状态：

1. `available`：可分配。
2. `leased`：并发已满，暂不可再分配。
3. `offline`：不可用。

核心迁移：

1. `get` 后 `lease_count +1`，达到上限时 `available -> leased`。
2. `done` 后 `lease_count -1`，低于上限时 `leased -> available`。
3. `done --result failure` 触发失败计数；达到阈值时转 `offline`。
4. `done --result success` 清零失败计数（不强制从 `offline` 自动恢复）。
5. 配置同步时，未在 `model.json` 中的 API 自动置 `offline`。

追踪链路：

1. `node_id -> ticket_id -> api_id`。

## 5. 并发一致性实现

运行态存储：

1. 使用 SQLite（WAL）。
2. 关键写路径（`get/done`）在 `BEGIN IMMEDIATE` 事务中执行。

保证：

1. 并发 `get` 不会超发超过 `max_concurrent`/`global_max_concurrent`。
2. `done` 原子完成：释放租约 + 更新健康状态 + 写 usage 记录。

## 6. 命令契约（CLI）

统一入口：`python -m openmate_pool`

1. `get`
输入：`request_id`、`node_id`、可选 `timeout_ms/lease_ms`。
输出：`{ ticket, endpoint }`。
2. `done`
输入：`ticket_id`、`result(success|failure)`、可选 usage 字段。
输出：release receipt。
3. `cap`
输出：容量快照。
4. `tickets`
输入：可选 `node_id` 过滤。
输出：活动租约列表。
5. `usage`
输入：可选 `node_id/limit`。
输出：usage 记录列表。
6. `sync`
输出：`{ synced: true, capacity }`。

## 7. 可观测性（MVP）

已沉淀原始指标字段：

1. token：`prompt/completion/total`
2. 耗时：`latency_ms`
3. 成本：`cost_usd`
4. 执行结果：`result=success|failure`
5. 追踪主键：`ticket_id`、`node_id`、`api_id`

## 8. 当前非目标

1. 复杂路由策略（按成本、按质量动态路由）。
2. 多租户配额治理。
3. 复杂生命周期（warming/draining/preempt）。
4. 业务级聚合报表（仅保留原始记录）。
