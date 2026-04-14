# Session 与 SessionEvent 字段收敛（2026-04-14）

## 1. 收敛目标

- Session 只表达执行态与顺序游标，不承载用户控制语义。
- SessionEvent 去除旧 `kind` 语义，统一使用 `item_type`。
- 当前仅支持工具调用链事件：
  - `function_call`
  - `function_call_output`
- 不再做旧字段与旧状态值兼容映射。

## 2. Session 字段定义

```text
Session
- id
- node_id
- status
- created_at
- updated_at
- last_seq
```

- `id`: Session 主键。
- `node_id`: 所属 Node。
- `status`: 仅允许 `active | waiting | completed | failed`。
- `created_at`: 创建时间。
- `updated_at`: 最近变更时间。
- `last_seq`: 会话内最后事件序号。

## 3. SessionEvent 字段定义

```text
SessionEvent
- id
- session_id
- seq
- item_type
- provider_item_id
- role
- call_id
- payload_json
- created_at
```

- `id`: Event 主键。
- `session_id`: 所属 Session。
- `seq`: 会话内单调递增序号。
- `item_type`: 仅允许 `function_call` / `function_call_output`。
- `provider_item_id`: provider 侧 item id（可空）。
- `role`: `user/assistant/tool/system`（可空）。
- `call_id`: 工具调用链 id（必填）。
- `payload_json`: 原始事件 JSON。
- `created_at`: 写入时间。

## 4. 工具调用前置准备

### 4.1 写入约束

- `call_id` 为必填。
- `item_type` 必须是 `function_call` 或 `function_call_output`。
- Session 状态只通过显式 `next_status` 更新，不再隐式推断。

### 4.2 查询能力

- 按 `session_id + seq` 顺序回放。
- 按 `session_id + call_id + seq` 查询工具调用链。
- 索引：
  - `idx_session_events_session_id_seq`
  - `idx_session_events_session_id_call_id_seq`

## 5. 兼容策略（已移除）

- 不再支持 `sessions.status` 的 `open/closed`。
- 不再支持 `session_events.kind`。
- 不再支持 CLI 参数 `--kind`。
- 检测到旧 schema 时，启动直接报错，要求手工迁移或重建 `.vos_sessions.db`。
