# Agent 工具调用开发清单（基于 VOS SessionEvent）

## 1. 对齐基线

- 读取并遵循共享契约：
  - `architecture/sharedInfo/工具调用-SessionEvent契约.md`
- 当前 VOS 存储层限制：
  - `item_type` 支持 `message`、`function_call`、`function_call_output`（并对 Responses 其他类型做前向兼容）
  - `call_id` 仅工具事件必填（`function_call`、`function_call_output`）
  - Session 状态仅允许 `active/waiting/completed/failed`

## 2. Agent 层输入输出约定

- 读取模型输出时，识别工具调用意图并产出：
  - `item_type=function_call`
  - `call_id`
  - `payload_json.name`
  - `payload_json.arguments`
- 工具执行完成后回写：
  - `item_type=function_call_output`
  - 同一个 `call_id`
  - `payload_json.output | ok | error`
- 无工具调用时回写：
  - `item_type=message`
  - `role=assistant`
  - `payload_json.output_text | content`

## 3. 最小执行循环（必须实现）

1. 模型响应中提取工具调用。
2. 将 `function_call` 追加到 VOS SessionEvent，并设置 `next_status=waiting`。
3. 调用实际工具执行器。
4. 将 `function_call_output` 追加到 VOS SessionEvent，并设置 `next_status=active`。
5. 继续推进模型调用，直到无工具调用。
6. 正常完成写 `next_status=completed`，异常终止写 `next_status=failed`。

## 4. 幂等与重试（必须实现）

- 幂等键：`session_id + call_id + item_type`
- 推荐重试策略：
  - 复用同一 `call_id`
  - 多次 `function_call_output` 允许保留历史，按最大 `seq` 作为最终结果
- Agent 重启恢复时，先按 `session_id + call_id` 拉链路，避免重复执行工具

## 5. 错误处理（必须实现）

- 工具执行失败时，必须写入 `function_call_output`：
  - `ok=false`
  - `error.code`
  - `error.message`
  - `error.retryable`
- 不允许只在日志报错而不落 SessionEvent。

## 6. 接口改造建议（Agent 内部）

- 新增一个 SessionEvent 适配器（建议）：
  - 输入：Agent 内部 tool-call 中间结构
  - 输出：VOS `append-event` 请求结构
- 对接接口最小字段：
  - `session_id`
  - `item_type`
  - `call_id`
  - `payload_json`
  - `next_status`

## 7. 测试清单（必须通过）

- 单元测试：
  - 产生 `function_call` 时能正确落库并进入 `waiting`
  - 回写 `function_call_output` 时能正确落库并回到 `active`
  - 工具失败时 `ok=false` 与 `error` 结构完整
  - 重试时不会因幂等冲突导致重复执行
- 集成测试：
  - 一次会话含多次工具调用（串行）
  - 一次会话工具调用失败后继续恢复
  - Agent 重启后按 SessionEvent 恢复执行

## 8. 验收标准

- Agent 能在同一 Session 内完整执行至少一轮：
  - `function_call -> function_call_output`
- VOS 中可按 `session_id` 顺序回放，且可按 `call_id`查询工具链路。
- 失败场景下 Session 状态与错误事件一致可追溯。
