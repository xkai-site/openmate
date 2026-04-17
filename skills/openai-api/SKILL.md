---
name: openai-api
description: OpenAI API 双请求方式技能。用于按 SOP 使用 Chat Completions 与 Responses，包含请求参数与响应参数注释模板、多轮会话、结构化输出、函数/工具调用迁移要点；新项目优先 Responses。
---

# OpenAI API 双通道 SOP

## 固定结论

1. 旧流程兼容：`POST /v1/chat/completions`
2. 新项目推荐：`POST /v1/responses`
3. 两者都可 `store: false` 关闭存储；Responses 默认更适合多轮和工具调用

## 通用请求头

```bash
-H "Content-Type: application/json"
-H "Authorization: Bearer $OPENAI_API_KEY"
```

## SOP A：Chat Completions（兼容模式）

### A1. 请求参数模板（带注释）

```jsonc
{
  "model": "gpt-5", // 必填：模型名
  "messages": [ // 必填：消息数组，按顺序拼接上下文
    { "role": "system", "content": "You are a helpful assistant." },
    { "role": "user", "content": "Hello!" }
  ],

  "store": false, // 可选：是否存储该请求/响应

  "response_format": { // 可选：结构化输出（Chat 形态）
    "type": "json_schema",
    "json_schema": {
      "name": "person",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "name": { "type": "string", "minLength": 1 },
          "age": { "type": "number", "minimum": 0, "maximum": 130 }
        },
        "required": ["name", "age"],
        "additionalProperties": false
      }
    }
  },

  "functions": [ // 可选：旧函数调用定义（迁移前常见）
    {
      "name": "web_search",
      "description": "Search the web for information",
      "parameters": {
        "type": "object",
        "properties": { "query": { "type": "string" } },
        "required": ["query"]
      }
    }
  ],
  "function_call": "auto", // 可选：函数调用策略（旧形态）

  "verbosity": "medium", // 可选：文本详细度（支持时）
  "reasoning_effort": "medium", // 可选：推理强度（支持时）
  "max_tokens": 1024, // 可选：输出 token 上限（字段名按 SDK/模型版本为准）
  "temperature": 1 // 可选：采样温度
}
```

### A2. 响应参数模板（带注释）

```jsonc
{
  "id": "chatcmpl_xxx", // 响应 ID
  "object": "chat.completion", // 对象类型
  "created": 1710000000, // 时间戳
  "model": "gpt-5", // 实际模型
  "choices": [ // 结果数组；常取第 1 项
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Hi! How can I help?",
        "function_call": null, // 旧函数调用字段（可能出现）
        "tool_calls": [] // 工具/函数调用信息（可能出现）
      },
      "finish_reason": "stop"
    }
  ],
  "usage": { // token 统计
    "prompt_tokens": 20,
    "completion_tokens": 12,
    "total_tokens": 32
  }
}
```

### A3. 最小调用示例

```bash
curl https://api.openai.com/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-5",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello!"}
    ]
  }'
```

### A4. 结果提取 SOP

1. 文本结果：`choices[0].message.content`
2. 工具/函数调用：检查 `choices[0].message.tool_calls` 或 `function_call`
3. 多轮：把 assistant 返回消息 append 回 `messages` 再发下一次请求

## SOP B：Responses（推荐模式）

### B1. 请求参数模板（带注释）

```jsonc
{
  "model": "gpt-5", // 必填：模型名

  "instructions": "You are a helpful assistant.", // 可选：系统指令
  "input": "Hello!", // 必填：字符串或 items 数组

  "store": true, // 可选：是否存储（多轮链路常设 true）
  "previous_response_id": "resp_abc123", // 可选：链路续接上轮 response

  "tools": [ // 可选：工具定义（原生工具或自定义函数）
    { "type": "web_search" },
    {
      "type": "function",
      "name": "get_weather",
      "description": "Get weather by city",
      "parameters": {
        "type": "object",
        "properties": {
          "city": { "type": "string" }
        },
        "required": ["city"],
        "additionalProperties": false
      },
      "strict": true // Responses 默认 strict 语义
    }
  ],
  "tool_choice": "auto", // 可选：工具选择策略
  "parallel_tool_calls": false, // 可选：是否允许并行工具调用

  "text": { // 可选：结构化输出（Responses 形态）
    "format": {
      "type": "json_schema",
      "name": "person",
      "strict": true,
      "schema": {
        "type": "object",
        "properties": {
          "name": { "type": "string", "minLength": 1 },
          "age": { "type": "number", "minimum": 0, "maximum": 130 }
        },
        "required": ["name", "age"],
        "additionalProperties": false
      }
    }
  },

  "include": ["reasoning.encrypted_content"], // 可选：无状态推理回传加密 reasoning
  "temperature": 1, // 可选：采样温度
  "max_output_tokens": 1024, // 可选：输出 token 上限
  "metadata": { // 可选：业务透传元数据
    "trace_id": "req-001"
  }
}
```

### B2. 响应参数模板（带注释）

```jsonc
{
  "id": "resp_abc123", // 响应 ID，可用于 previous_response_id
  "object": "response", // 对象类型
  "created_at": 1710000000, // 时间戳
  "model": "gpt-5",
  "status": "completed", // completed / incomplete / failed 等

  "output": [ // Items 数组（message、function_call、function_call_output...）
    {
      "type": "message",
      "id": "msg_1",
      "role": "assistant",
      "content": [
        {
          "type": "output_text",
          "text": "Hi! How can I help?"
        }
      ]
    }
  ],
  "output_text": "Hi! How can I help?", // SDK 常用聚合字段

  "usage": {
    "input_tokens": 20,
    "output_tokens": 12,
    "total_tokens": 32
  },
  "error": null // 失败时包含错误对象
}
```

### B3. 工具调用回环（关键 SOP）

1. 首次请求：发送 `tools` + 用户 `input`
2. 若 `output` 内出现 `type=function_call`：
3. 在本地执行工具后，回传 `function_call_output`
4. 下一轮请求带上 `previous_response_id=<上一轮id>`

回传模板：

```jsonc
{
  "model": "gpt-5",
  "previous_response_id": "resp_abc123",
  "input": [
    {
      "type": "function_call_output",
      "call_id": "call_001", // 与上轮 function_call.call_id 对齐
      "output": "{\"result\":\"done\"}" // 工具执行结果
    }
  ]
}
```

### B4. 最小调用示例

```bash
curl https://api.openai.com/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $OPENAI_API_KEY" \
  -d '{
    "model": "gpt-5",
    "instructions": "You are a helpful assistant.",
    "input": "Hello!"
  }'
```

### B5. 结果提取 SOP

1. 快速文本：优先读 `output_text`
2. 细粒度处理：遍历 `output` items
3. 多轮：优先 `previous_response_id` 链接上下文

## 映射速查（迁移时必看）

1. `messages`（Chat） -> `input`（Responses）
2. `response_format`（Chat） -> `text.format`（Responses）
3. `choices[].message`（Chat） -> `output[]` + `output_text`（Responses）
4. 手动维护消息历史（Chat） -> `previous_response_id`/Conversations（Responses）
5. 旧 `functions/function_call`（Chat） -> `tools(function)` + `function_call_output`（Responses）

## 执行清单（简洁版）

1. 新功能默认走 `Responses`
2. 兼容存量时保留 `Chat Completions`
3. 结构化输出统一改到 `text.format`
4. 工具调用统一按 `call_id` 回传 `function_call_output`
5. 需要无状态推理时使用：`store=false` + `include=["reasoning.encrypted_content"]`
