# 工具调用 SessionEvent 契约（V1）

## 1. 目标与边界

- 本契约用于 Agent 层与 VOS Session 存储层对齐工具调用事件。
- VOS 只负责事件存储、顺序回放、按 `call_id` 查询，不负责工具执行。
- 当前仅允许两类 `item_type`：
  - `function_call`
  - `function_call_output`

## 2. Session 状态约定

- 允许值：`active | waiting | completed | failed`
- 推荐推进：
  - 写入 `function_call` 后：可设置 `next_status=waiting`
  - 写入 `function_call_output` 后：通常设置 `next_status=active`
  - 全流程结束：显式设置 `next_status=completed` 或 `failed`

## 3. SessionEvent 公共字段

```json
{
  "session_id": "string",
  "item_type": "function_call | function_call_output",
  "provider_item_id": "string|null",
  "role": "user|assistant|tool|system|null",
  "call_id": "string",
  "payload_json": {},
  "next_status": "active|waiting|completed|failed|null"
}
```

- `call_id` 必填。
- `payload_json` 必填，且应保留原始业务语义，不做截断。

## 4. function_call 载荷

`item_type=function_call` 时，`payload_json` 最小结构：

```json
{
  "name": "tool_name",
  "arguments": {},
  "role": "assistant"
}
```

- `name`：必填，工具名。
- `arguments`：必填，工具入参（建议固定为 JSON object，不使用 JSON string）。
- `role`：可选，建议写 `assistant` 便于检索。

## 5. function_call_output 载荷

`item_type=function_call_output` 时，`payload_json` 最小结构：

```json
{
  "output": {},
  "ok": true,
  "error": null,
  "role": "tool"
}
```

- `output`：成功时必填，建议 JSON object。
- `ok`：必填，`true/false`。
- `error`：失败时必填对象，成功时可为 `null`。
- `role`：可选，建议写 `tool`。

失败示例：

```json
{
  "output": null,
  "ok": false,
  "error": {
    "code": "TOOL_TIMEOUT",
    "message": "tool execution timed out",
    "retryable": true
  },
  "role": "tool"
}
```

## 6. 幂等与重试约定

- 幂等键建议：`session_id + call_id + item_type`
- 同一工具调用重试策略：
  - 推荐复用同一个 `call_id`
  - 允许同一 `call_id` 下出现多条 `function_call_output`（保留历史），由上层按最新 `seq` 取最终结果

## 7. 时序约定（最小闭环）

1. Agent 产出 `function_call`
2. VOS 追加事件（可置 `waiting`）
3. 上层执行工具
4. 上层写入 `function_call_output`（可置 `active`）
5. Agent 继续推理或结束（`completed/failed`）

## 8. 兼容策略

- 不兼容旧 `kind` 字段。
- 不兼容旧状态值 `open/closed`。
- 检测到旧 Session schema 时，VOS 启动应失败并提示手工迁移或重建库。
