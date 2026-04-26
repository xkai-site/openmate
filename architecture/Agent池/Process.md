# Agent池内部开发过程（收敛版）

## 2026-04-25 Chat/Responses 输入协议硬解耦

1. `api_mode=chat_completions` 校验收敛为仅接受 `chat_request`：
   - `validateInvokeRequestForMode` 不再接受 `request`（Responses 形态）；
   - 未传 `chat_request` 时返回明确错误：`chat_request is required for api_mode=chat_completions`。
2. provider 层移除 chat 模式下的 Responses->Chat 兼容转换桥：
   - `buildChatCompletionsPayloadForInvoke` 仅从 `chat_request` 构造下游 payload；
   - 传 `request` 时返回 `gateway_unsupported_request`。
3. chat 响应归一化逻辑保留，仍输出统一结构供上层复用既有解析逻辑。
4. 单测同步更新：
   - `internal/poolgateway/openai_responses_test.go`：chat 模式传 `request` 改为失败断言；
   - `internal/poolgateway/providers_test.go`：替换原“chat 模式兼容 request”测试为拒绝断言。
5. 回归结果：
   - `go test ./internal/poolgateway/... ./internal/schedule/...` 通过。

## 2026-04-17 Chat 流式追尾与结果查询闭环（vos/httpapi + frontend）

1. `vos/httpapi`：
   - `/api/v1/chat/stream` 已支持 `invocation_id` 追尾订阅运行中流。
   - `/api/v1/chat/result` 支持按 `invocation_id` 查询最终状态、文本、usage、模型、错误。
   - SSE 事件补齐 `invocation`（含 `invocation_id`）与 `summary.status`；失败态 `fatal` 增加 `invocation_id/status`。
2. 前端 `Home` 与 `SessionPanel`：
   - 增加 `invocation_id` 事件消费与 `sessionStorage` 挂起态保存。
   - 网络中断时优先走 `/chat/result` 查询；若仍 `running` 则自动用 `invocation_id` 追尾流，而非直接退回非流式。
   - 页面刷新后若检测到挂起 `invocation_id`，会先查结果；运行中则自动恢复追尾。
3. 测试补充：
   - 新增 `server_test.go` 覆盖 `/chat/result` 参数校验、不存在、成功查询。
   - 新增 `/chat/stream` 追尾场景：不存在 invocation 返回 404，存在运行返回 invocation/delta/summary 事件。
4. 回归结果：
   - `go test ./internal/poolgateway/... ./internal/vos/httpapi/...` 通过
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
   - `frontend npm run build` 当前失败，原因为前端既有严格类型错误（`ApiResponse<T>` 与业务返回类型映射不一致，含 `nodes/topic/tree/planlist/stats/chat` 等文件），非本轮新增单点错误。

## 2026-04-17 Chat/Responses 切换兼容与日志增强

1. 针对“仅切 `model.json` 不改前端”诉求，`api_mode=chat_completions` 时已支持继续接收 `request`（Responses 形态）并在池内自动转换为 Chat 请求。
2. `chat_request` 仍可用；当前为：
   - `chat_request`：原生 Chat 入参
   - `request`：兼容入参（在 chat 模式下自动转换）
3. 调用日志增强（默认 `info` 可见）：
   - 新增 `attempt started` 日志，输出 `provider/api_id/api_mode/model/base_url`
   - `attempt failed` 日志新增 `error_message/error_details/provider_status_code`
4. 该改动用于快速排查 `HTTP 5xx/4xx`、路由模式误配及上游网关兼容问题。
5. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（61 项）

## 2026-04-17 Chat 工具调用与双协议入参收敛

1. `InvokeRequest` 已支持双入参：
   - `request`（Responses）
   - `chat_request`（Chat Completions）
2. 入参校验收敛为“二选一”：
   - `request` 与 `chat_request` 不能同时传，也不能都不传。
   - 并在命中 `api_mode` 后做二次校验（responses 模式必须有 `request`，chat 模式必须有 `chat_request`）。
3. Chat 路径已取消 `tool_choice=none` 强制覆盖，支持透传 `tools/tool_choice/response_format/max_tokens` 等 Chat 字段。
4. Chat 响应归一化增强：
   - 可将 `choices[0].message.tool_calls` 归一化为 Responses 风格 `output[type=function_call]`。
   - 继续保持统一 `InvokeResponse` 消费口径。
5. 上下文边界已明确：
   - Responses 与 Chat 的上下文均由调用方维护；
   - `pool` 不维护会话历史。
6. Python 适配层同步：
   - 新增 `OpenAIChatCompletionsRequest` 模型；
   - `InvokeRequest` 增加 `chat_request` 并做互斥校验。
7. 测试与回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（61 项）

## 2026-04-17 provider 字段降级为统计标签

1. `internal/poolgateway/providers.go::GetProviderClient` 已调整为不再依据 `provider` 字段做硬性路由判定。
2. 当前调用路径统一走 `OpenAICompatibleProvider`，`provider` 仅作为观测/统计标签保留在记录中，不再触发 `unsupported provider` 阻断。
3. 新增回归测试 `TestGetProviderClientAcceptsArbitraryProviderLabel`，覆盖 `provider="Xcode"` 这类自定义标签场景。
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过

## 2026-04-17 5xx 快速失败（不重试）

1. `internal/poolgateway/providers.go::classifyHTTPError` 已调整：provider 返回 `5xx` 时不再标记 `retryable=true`。
2. 该变更用于避免同一用户请求在上游故障期触发多次重复调用（快速失败，减少重复成本）。
3. 新增回归测试 `TestOpenAICompatibleProviderClassifies5xxAsNonRetryable`。
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过

## 2026-04-17 支持 chat/completions 模式

1. `model.json.apis[*]` 新增 `api_mode`，当前支持：
   - `responses`（默认）
   - `chat_completions`
2. `pool` 在 `chat_completions` 模式下会将内部 `Responses` 形态请求转换为 `chat/completions` 请求并调用 `/chat/completions`。
3. 返回结果会被规范化为既有 `Responses` 风格输出结构，保证上层消费口径不变。
4. 为避免跨轮工具回环语义差异，`chat_completions` 模式下默认关闭工具调用（`tool_choice=none`），目标是先保证基础问答稳定可用。
5. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过

## 2026-04-14 OpenAI Responses 协议切换落地

1. `pool invoke` 的主调用载荷已从旧的 `messages/chat.completions` 形态切到 OpenAI `Responses` 形态；当前外层网关请求为 `{ request_id, node_id, request, timeout_ms, route_policy }`，其中 `request` 对齐 OpenAI `Responses create`，但 `model` 继续由 `model.json` 注入。
2. provider 适配层已切到 `/v1/responses`，并支持工具调用相关字段透传，包括 `tools / tool_choice / parallel_tool_calls / previous_response_id`；Agent池只负责协议转发、落库、观测与重试，不在池内执行工具。
3. `InvokeResponse / InvocationRecord` 已改为返回完整 `response` 对象，同时保留 `output_text / route / usage / timing / error` 这组网关侧消费字段，便于上层直接取文本，也能完整回看原始响应。
4. `model.json.apis[*]` 新增 `request_defaults / headers / pricing`：
   `request_defaults` 用于承载 OpenAI 请求默认参数，
   `headers` 用于 provider 侧额外请求头，
   `pricing` 用于按 `input/output/cached/reasoning` 维度计算 `cost_usd`。
5. usage 口径已从 `prompt/completion/total_tokens` 切到 `input_tokens / output_tokens / total_tokens / cached_input_tokens / reasoning_tokens`，`usage` 聚合命令和落库记录已同步升级。
6. 当前测试结果：`go test ./...` 与 `.\.venv\Scripts\python.exe -m unittest discover -s tests -v` 均通过，Python 侧仍为 30 项通过。
7. 已补充 `architecture/Agent池/对接协议.md`，作为面向调用方的协议文档，明确 `invoke/records/usage/cap/sync/model.json` 的 JSON 结构、错误约定与工具回环示例。

## 2026-04-12 usage 聚合视图落地

1. 在保留 `records` 原始 invocation/attempt 输出的前提下，新增只读 `usage` 聚合视图命令。
2. `usage` 当前按 invocation 级结果聚合，并补充 `attempt_count / retry_count`，用于快速看调用消耗与执行开销。
3. 支持沿用 `records` 的 `node_id / limit` 过滤口径，避免 summary 与原始记录查询窗口不一致。
4. Python 侧 `PoolGateway` 已同步暴露 `usage()`，保持 CLI + JSON 契约不变。
5. 当前测试结果：`go test ./...` 与 `python -m unittest discover -s tests -v` 均通过；Python 侧共 30 项通过。

## 2026-04-11 重试与错误语义收敛

1. `invoke` 执行流已从“单次 reserve 后直接终结 invocation”调整为“attempt 先收尾，再按策略决定是否继续重试，最后统一产出 invocation 最终结果”。
2. 重试策略已开放到 `model.json.retry`：当前支持 `max_attempts / base_backoff_ms`，缺省时仍沿用网关默认值。
3. provider 错误分类已细化为 `provider_unreachable / provider_timeout / provider_rate_limited / provider_http_error / provider_invalid_json / gateway_internal_error` 这一组内部语义。
4. API 摘除计数已收紧为仅统计健康恶化型失败；`429`、返回 JSON 非法、网关内部错误不会直接把 API 打成 `offline`。
5. 已补 Go 单测、Python 契约测试与可选真实 provider 集成测试骨架；当前 `go test ./...` 与 `python -m unittest discover -s tests -v` 均通过。

## 2026-04-11 invoke 网关落地

1. 已取消对外 `get / done / tickets / usage` 旧语义，CLI 主命令先收敛为 `invoke / cap / records / sync`，后续再按新定义补充聚合视图命令。
2. `openmate_pool` 已完成请求/响应模型重构：从租约模型切到 `InvokeRequest / InvokeResponse / InvocationRecord / InvocationAttempt`。
3. 运行态存储已从 `tickets + usage_records` 转为 `invocations + invocation_attempts`，并保留 `apis` 作为底层并发与健康治理状态机。
4. 新增 provider adapter 分层，当前先落地 `openai_compatible` 调用实现。
5. `service.execute()` 已改为通过网关发起模型调用，不再向上暴露 endpoint 或持有 provider 细节。
6. 当前测试结果：`python -m unittest discover -s tests -v` 共 30 项通过。

## 2026-04-11 Go 迁移完成

1. Agent池运行时已迁移为 Go 实现，代码入口为 `cmd/openmate-pool`，核心逻辑位于 `internal/poolgateway`。
2. Python 侧 `openmate_pool` 已降为契约模型 + Go CLI adapter，不再承担 SQLite/provider/runtime 逻辑。
3. `openmate_agent.service` 默认通过 Python adapter 调用 Go 二进制，边界固定为 `CLI + JSON`。
4. Go 侧已通过 `go test ./...`；Python 侧已通过 `python -m unittest discover -s tests -v`。
5. 为避免权限问题，Go 构建缓存与模块缓存固定落在仓库内 `.openmate/`，并已加入 `.gitignore`。

## 2026-04-11 网关方向更新

1. 已明确将 Agent池的目标定位上收为“LLM 调用网关 + 可观测体系”，不再把它仅视为 API 租约池。
2. 方向性调整为：完整请求与完整响应都应经过 Agent池；上层 service 只消费标准化结果中的必要部分，再做业务解析。
3. 当前 `get/done` 仍是现网兼容基线，但后续主写路径应迁移到 `invoke` 一类统一调用契约。
4. 这意味着 provider 适配、凭证托管、重试/限流/熔断、usage/latency/error 采集都需要进一步收口到 Agent池内部。
5. 该变化已同步到 `sharedInfo/架构.md`，作为跨模块边界更新。

## 2026-04-11 接手初始化

1. 当前工作分支确认为 `pool`，职责边界继续按 Agent 池模块推进。
2. 仓库根目录原先缺少本地 `.venv`，已按项目约定补建，并完成 `pip install -r requirements.txt`。
3. 初始化验证发现 `PoolStateStore._init_db()` 在 Windows 下未及时关闭 SQLite 连接，导致 CLI 测试清理临时目录时 `.db` 文件句柄残留。
4. 已修复为显式关闭初始化连接。
5. 当前可直接继续在 `openmate_pool` 上开发。

## 2026-04-17 统一数据库路径对齐（一期）

1. `cmd/openmate-pool` 默认 `--db-file` 改为 `.openmate/runtime/openmate.db`，与调度和 VOS session 统一。
2. `internal/poolgateway.NewStore` 新增路径收敛与目录自动创建，避免首次启动因目录不存在而失败。
3. Python 适配层 `openmate_pool.PoolGateway` 默认 `db_path` 改为 `.openmate/runtime/openmate.db`。
4. 与 Agent 侧 `PoolGateway` 默认入参保持一致，减少跨模块联调时的路径分叉配置。
5. 回归结果：
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（53 项）

## 后续待办（仅保留有效项）

1. 将默认重试策略参数化到配置层，但在开放前先保持当前代码内默认值稳定。
2. 视需要继续扩 `usage` 维度，例如按 api/provider/model 的分桶聚合，但保持 `records` 作为原始事实来源。

## 2026-04-17 Dead Code Cleanup

1. Removed unused helper functions `intPtr` and `parseInt` from `cmd/openmate-pool/main.go`.
2. Kept all pool CLI contracts (`invoke/cap/records/usage/sync`) unchanged.
3. Verified `go test ./internal/poolgateway/...` and full `go test ./...` passed.

## 2026-04-17 统一 Go 运行时装配接入

1. 新增统一运行时装配层 `internal/openmate/runtime`，已将 `poolgateway` 纳入同一装配上下文。
2. 当前行为为：
   - `pool` 运行时与 `vos/schedule` 共享 `.openmate/runtime/openmate.db` 默认路径
   - `pool` 对外 `cmd/openmate-pool` CLI 语义保持不变
3. 该变更用于支撑同进程运行形态，不改变现有 Python adapter 调用方式。

## 2026-04-17 Pool 日志链路接入（slog）

1. `internal/poolgateway/gateway.go` 已接入结构化日志，覆盖调用生命周期：
   - 调用预留
   - 尝试执行与重试
   - 成功/失败收敛
2. `cmd/openmate-pool` 新增统一日志参数：
   - `--log-level`（`debug|info|warn|error`）
   - `--log-format`（`json|text`）
3. CLI 兼容性要求保持不变：业务 JSON 始终输出到 `stdout`，日志仅写入 `stderr`。
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过

## 2026-04-17 model.json 哈希增量同步

1. `internal/poolgateway/store.go` 新增 `model_config_hash` 元信息，`syncConfig` 改为先比对哈希：
   - 配置未变化：跳过 `apis/meta` 重写，避免每次调用都执行 upsert。
   - 配置变化：执行完整同步，并在事务内回写新哈希。
2. 为降低无意义抖动，哈希计算前对 `ModelConfig` 做标准化：
   - `apis` 按 `api_id`（及次级字段）排序后再计算；
   - `headers/request_defaults` 统一深拷贝，保证序列化稳定。
3. 新增测试：
   - `TestStoreSyncFromModelConfigSkipsWriteWhenHashUnchanged`
   - `TestStoreSyncFromModelConfigAppliesWhenHashChanged`
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `go test ./...` 通过

## 2026-04-17 Windows 子进程 UTF-8 解码修复（Python Adapter）

1. 问题现象：
   - 调度调用 `openmate_agent.cli worker run` 时，链路内 Python adapter 可能触发 `subprocess` 读取线程异常，导致 worker 失败。
2. 适配层修复：
   - `openmate_pool/pool.py`
   - `openmate_pool/binary.py`
   - `openmate_pool/cli.py`
   - 所有 `subprocess.run(..., text=True)` 统一改为显式 `encoding="utf-8", errors="replace"`。
3. 测试补充：
   - `tests/test_pool.py` 新增 `test_run_command_uses_utf8_decode`，校验 `PoolGateway` 的 subprocess 参数。
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过
   - `.\.venv\Scripts\python.exe -m unittest tests.test_pool.PoolGatewayTestCase.test_run_command_uses_utf8_decode` 通过

## 2026-04-17 Responses input 统一归一化为消息数组

1. 问题：上游可能以字符串传入 `request.input`，导致发送外部 `/responses` 时请求形态不一致。
2. 修复：
   - `internal/poolgateway/providers.go` 在 `invokeResponses` 增加 `normalizeResponsesInput()`：
     - 字符串自动归一化为 `[{\"role\":\"user\",\"content\":\"...\"}]`
     - 数组输入保持数组
     - 对象输入包装为单元素数组
3. 测试更新：
   - `internal/poolgateway/providers_test.go` 校验 `/responses` 外发 payload 的 `input` 为消息数组而非字符串。
4. 回归结果：
   - `go test ./internal/poolgateway/...` 通过

## 2026-04-17 Stream Enablement for Responses and Chat

1. Removed stream rejections in both Go and Python request validation paths:
   - `request.stream` and `chat_request.stream` are now allowed.
   - `request_defaults.stream=true` is now accepted in `model.json`.
2. Added provider-side streaming parsing support:
   - `responses` mode now parses SSE frames and reconstructs a final Responses payload.
   - `chat_completions` mode now parses SSE chunk deltas (content and tool_calls), then normalizes to the existing unified Responses-style output.
3. Preserved compatibility constraints:
   - `metadata`, `truncation`, and `user` are still stripped before provider request dispatch in Responses path.
   - Frontend request shape stays unchanged.
4. Added regression tests:
   - Go: stream validation acceptance, config stream defaults acceptance, Responses stream parsing, Chat stream parsing, Chat tool-call stream parsing.
   - Python: stream field acceptance for both request models.
5. Validation results:
   - `go test ./internal/poolgateway/...` passed
   - `go test ./...` passed
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` passed (63 tests)

## 2026-04-21 Pool CLI 帮助输出去重（无行为改动）

1. 修复 `openmate-pool --help` 场景下帮助文本重复打印的问题。
2. 调整点：`cmd/openmate-pool/main.go` 中 `flag.ErrHelp` 分支不再二次调用 `Usage()`。
3. `invoke/cap/records/usage/sync` 命令语义与 JSON 输出契约不变，仅修复帮助输出一致性。
4. 回归结果：
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
