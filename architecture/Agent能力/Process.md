# Process 记录
## 2026-04-25 Process 工具能力新增（node_process + sibling_progress_board）
1. `openmate_agent` 新增 `node_process` 默认工具（内建后端 `builtin/node_process`），支持：
   - `action=get`：读取当前 node 的 process 列表。
   - `action=replace`：按传入 `processes` 全量替换当前 node process（可选 `expected_version`）。
2. 新增 `sibling_progress_board` 非默认工具（内建后端 `builtin/sibling_progress_board`），用于读取当前 node 同级进度，输出项包含 `node_id / node_name / process_id / process_name`。
3. 根节点行为约束：当前 node 无 `parent_id` 时，`sibling_progress_board` 返回 `items: []`。
4. CLI 已扩展并支持帮助查询：
   - `openmate-agent tool node_process --help`
   - `openmate-agent tool sibling_progress_board --help`
5. 运行时注册初始化：`load_tool_registry` 会自动确保 `.openmate/runtime/tool_registry.json` 至少包含 `sibling_progress_board` 注册项（`enabled=true`、`is_default=false`、`primary_tag=process`）。
6. 本轮按要求仅完成实现与用例落地，不执行测试。

## 2026-04-25 Tool Registry + Default-Only Injection
1. Agent tool runtime migrated to registry-driven architecture with JSON persistence (`.openmate/runtime/tool_registry.json`).
2. Default model-injected tools fixed to six: `read`, `write`, `search`, `command`, `network`, `tool_query`.
3. Non-default tools (`edit/patch/grep/glob/exec/shell/query`) moved to extension set and discovered on demand via `tool_query`.
4. `tool_query` now supports threshold mode (10): direct tool details when `<=10`, tag summary when `>10`, plus `by_tag` and `keyword` drill-down.
5. `ToolName Literal` static constraint removed; runtime now validates tools by registry metadata.
6. Permission policy changed from hardcoded whitelist to registry metadata + command risk rules.
7. CLI expanded with `openmate-agent tools list/register/update/enable/disable/validate` and `--help` coverage.
8. Verified by tests:
   - `.\.venv\Scripts\python -m unittest tests.test_service tests.test_cli tests.test_pipeline_orchestration`
   - `.\.venv\Scripts\python -m unittest tests.test_worker tests.test_context_injector`

## 2026-04-25 Chat 独立执行链路解耦（Responses 保持不变）

1. `worker` 请求模型新增 `agent_spec.api_id`（可选），并在执行前基于 `model.json` 解析目标 `api_mode`：
   - 单 enabled API：允许自动选择；
   - 多 enabled API 且未传 `api_id`：直接失败并提示必须传 `agent_spec.api_id`；
   - `api_id` 命中后由该 API 唯一决定 `api_mode`（`responses/chat_completions`）。
2. 执行编排新增 `ChatExecutionRunner`，与 `ResponsesExecutionRunner` 独立：
   - Chat 走原生 `chat_request` + 本地 `messages` 窗口回环；
   - 不使用 `previous_response_id`；
   - 工具回环按 Chat 语义维护 `assistant(tool_calls)` + `tool(tool_call_id)`。
3. 在 `ChatExecutionRunner` 内新增 `WindowBuilder` 抽象与默认实现 `DefaultChatWindowBuilder`，本次先落“最小可用回环”窗口，后续可替换策略而不改 runner 主流程。
4. `ResponsesExecutionRunner` 行为保持原样，仅补充 `route_policy.api_id` 透传，确保多模型路由确定性。
5. 会话事件语义保持兼容：`function_call/function_call_output/message/failed` 路径与既有 VOS 消费口径不变。
6. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_worker tests.test_pool -v` 通过（41 项）。

## 2026-04-24 执行链路注入封装重构（单消息内 SystemPrompt + UserPrompt）

1. 执行链路保持单条 `OpenAIResponsesRequest.input`（`role=user`）不变，不改为 `system/user` 双消息。
2. `VosContextInjector` 输出上下文字段改为：
   - `node_id`
   - `user_memory`
   - `topic_memory`
   - `process_contexts`
   - `session_history`
   - 不再注入 `node_memory`、`global_index` 到执行上下文载荷。
3. `ExecutionAgentService` 新增执行态封装：
   - 在单条请求内容中构造第一段 `SystemPrompt`（预设提示词 + 工具管理 + skill 管理 + 记忆更新确认规则）。
   - 同一内容中构造 `UserPrompt`（`user_memory/topic_memory/process_contexts/session_history`，其中主要体量为 `session_history`）。
4. 兼容处理：执行封装支持读取旧 `ContextBundle.payload` 结构并转换到新结构，避免存量注入格式导致执行失败。
5. 本轮范围仅影响执行链路；`decompose` 维持现有注入路径不变。
6. 验证结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector tests.test_service tests.test_pipeline_orchestration -v` 通过（33 项）。

## 2026-04-09

### 本次决策

1. 将 Agent 能力文档从“细粒度流水线分段”收敛为“能力注入核心抽象”。
2. 核心接口统一为：`ContextInjector`、`ToolInjector`、`SkillInjector`、`Assembler`。
3. 明确 `ToolInjector` 需内置权限裁决入口，最小工具集合为 `read/write/query`。
4. `Context` 与 `Skill` 明确拆分，避免上下文压缩策略与 skill 策略耦合。
5. 保留对外最小契约：`build/execute/priority`，并继续约束能力层不承载调度与资源路由。

### 当前状态

1. `architecture/Agent能力/Agent能力.md` 已重写为接口层优先版本。
2. 复杂执行后处理能力（解析、持久化、建议、审计）暂归入后续扩展，不在当前冻结。

### 下一步建议

1. 基于新文档先实现 Pydantic 数据模型与 Protocol 接口骨架。
2. 先提供默认注入实现与 CLI 对外入口，随后补单元测试。

### 本次实现落地

1. 已新增 `openmate_agent` 包，完成最小数据模型（Pydantic）与接口层（Protocol）骨架。
2. 已实现默认能力组件：
   - `DefaultContextInjector`
   - `DefaultToolInjector`（`read/write/query` + `authorize`）
   - `DefaultSkillInjector`
   - `DefaultAssembler`
   - `DefaultAgentExecutor`
3. 已实现 `AgentCapabilityService`，对外提供 `build/execute/priority`。
4. 已新增 CLI 入口 `openmate_agent.cli`，支持 `build/execute/priority` 与 `--help`。
5. 已新增单元测试并通过：
   - `tests/test_service.py`
   - `tests/test_cli.py`
   - 命令：`.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v`

### 增量实现（ToolRuntime）

1. 新增 `ToolRuntime` 抽象，并落地本地实现 `LocalToolRuntime`，对外统一 `read/write/query` 语义。
2. 工具实现采取“封装一层”方案：CLI 不直接绑定系统命令，而是调用 `AgentCapabilityService.run_tool`。
3. 能力语义修正为：`read/write` 针对文件系统，`query` 针对网络调用（HTTP GET/POST）。
4. 加入工作目录边界约束，避免越界读写。
5. CLI 新增 `tool` 子命令：
   - `tool read <node_id> --path ...`
   - `tool write <node_id> --path ... --content ... [--mode overwrite|append]`
   - `tool query <node_id> --url ... [--method GET|POST] [--params '{}'] [--headers '{}'] [--body '{}']`
6. 增补测试覆盖 `run_tool` 与 CLI tool 命令，包含本地 HTTP 测试服务，当前测试总数 11，全部通过。

### 增量重构（Tool 抽象层）

1. 工具层重构为注册式抽象：`ToolRegistry + Tool 抽象 + PermissionGateway`。
2. 新增专用工具实现：`ReadTool / WriteTool / QueryTool`，并新增通用 `ShellTool`。
3. 语义固定为：
   - `read/write` 针对文件系统
   - `query` 针对网络调用（HTTP GET/POST）
   - `shell` 承接系统命令（如 `git`）
4. `AgentCapabilityService.run_tool` 已改为：
   - 构建 `ToolAction`
   - 走 `PermissionGateway`
   - 通过 `ToolRegistry` 分发执行
5. CLI 已支持：
   - `tool shell <node_id> --cmd "..." [--cwd ...] [--timeout-seconds ...]`
6. 删除旧 `runtime.py` 路径，避免与新抽象并存造成歧义。
7. 单测已更新并通过，当前测试总数 13，全部通过。

### 增量更新（权限标记位）

1. 工具调用模型新增两个布尔入参：`is_safe`、`is_read_only`，默认值均为 `false`。
2. 权限网关改为两阶段：
   - 当两个标记均为 `false` 时返回 `confirm`（先记录确认需求，后续接 LangGraph 人机确认流）。
   - 当两个标记均为 `true` 时才进入黑白名单路由。
3. 黑名单仍作为兜底策略：即使标记为 `true`，危险 shell 命令仍会被拒绝。
4. CLI 所有 `tool` 子命令已支持 `--is-safe` 与 `--is-read-only`。
5. 已补充对应测试用例，当前测试总数 15，全部通过。

### 对外暴露面收敛

1. 根据当前阶段目标（聚焦 Agent 手脚/Tool），CLI 对外暴露面已收敛为仅 `tool` 命令。
2. `build/execute/priority` 仍保留为服务层能力，但不再作为 CLI 命令直接暴露。

### 文件工具安全重构

1. 新增共享机制：
   - `FileTimeStore`：记录 `read` 后文件 mtime 基线。
   - `FileLockManager`：提供 `with_lock` 文件互斥锁。
2. `read` 升级为上下文限流阅读器：
   - 支持 `offset/limit` 分页。
   - 硬限制：最多 2000 行、单行最多 2000 字符、总输出最多 50KB。
   - 文件输出增加 `|line|` 行号前缀。
   - 目录路径返回结构化列表（名称/类型/大小）。
   - 二进制文件检测并拒绝输出。
   - 成功读取后写入 FileTime 基线。
3. `write` 升级为全量覆盖写入：
   - 写入前执行 FileTime 冲突校验。
   - 生成 unified diff 预览并返回在输出中。
   - 写入后更新 FileTime 基线。
   - 对 Python 文件执行基础编译诊断并回传。
4. 新增 `edit` 工具：
   - 参数为 `old_string -> new_string`，不依赖行号。
   - 匹配策略：`ExactMatch`、`WhitespaceNormalizer`、`BlockAnchorReplacer`、`SimilarityMatcher`、`LineTrimMatcher`。
   - 相似度候选策略：低于 30% 丢弃；单候选高于 70% 才自动通过；多候选时拒绝并提示歧义。
   - 执行前校验 FileTime，执行过程持有文件锁。
5. CLI 工具子命令新增 `tool edit`，`tool read` 新增 `offset/limit` 参数。
6. 测试覆盖冲突拦截、read/write/edit 主链路与 CLI，当前测试总数 14，全部通过。

### 检索工具新增（grep / glob）

1. 新增 `grep` 工具：
   - 基于 `ripgrep (rg)` 做内容检索。
   - 支持正则表达式匹配（pattern）。
   - 支持 `scope`、`max_results`、`file_glob` 参数。
2. 新增 `glob` 工具：
   - 基于 `rg --files` 获取文件列表。
   - 使用 Python `fnmatch` 按通配模式过滤（如 `**/*.ts`）。
   - 保持 `.gitignore` 过滤语义。
3. 修复 `glob` 忽略规则问题：
   - 不再直接将 `-g pattern` 交给 `rg --files`，避免把忽略规则覆盖。
   - 改为先获取已忽略过滤后的候选，再本地匹配 pattern。
4. `ToolRegistry`、`PermissionGateway`、CLI 子命令已同步注册：
   - `tool grep ...`
   - `tool glob ...`
5. 共享依赖已沉淀至 `architecture/sharedInfo/依赖.md`，明确 `ripgrep (rg)` 为前置依赖。
6. 单测已覆盖 `grep/glob`，当前测试总数 16，全部通过。

## 2026-04-11

### 初始化

1. 已读取 `AGENTS.md` 与 `architecture/Agent能力/Process.md`，确认当前负责 `agent` 分支下的 Agent 能力模块。
2. 已核对当前封装形态，核心抽象仍为 `ToolAction / ToolResult / ToolRegistry / PermissionGateway / CLI tool`。
3. 当前工作区未发现项目内 `.venv`，系统 Python 环境缺少 `pydantic`，因此本轮仅完成架构与技术栈预研，未完成本地 agent 单测复核。

### 技术栈预研结论

1. 当前场景本质上是“工具调用运行时”而不是“纯提示词编排”：
   - 需要强类型入参/出参
   - 需要文件、网络、shell、grep/glob 等系统级工具能力
   - 需要权限闸门、超时、取消、并发控制、资源边界
   - 需要 CLI 优先、后续可挂接人机确认与持久化执行
2. 若以“最短交付时间 + 延续现有 Agent/LLM 生态”为优先，首选仍是 `Python + Pydantic`。
3. 若以“长期运行、低资源占用、易部署、并发与取消语义清晰”为优先，更适合作为核心 tool runtime 的候选是 `Go + Cobra`。
4. 若以“极致性能、内存安全、资源可预测性”为优先，备选为 `Rust + clap/serde/tokio`，但研发与维护成本最高。
5. `TypeScript/Node.js` 更适合作为接入层或生态对接层，不建议作为核心工具运行时主栈。

### 下一步建议

1. 先明确 Agent 能力层下一阶段目标到底偏“编排层”还是偏“工具运行时”。
2. 若偏编排层，继续用 Python 成本最低。
3. 若偏工具运行时，建议评估“Python 编排层 + Go 工具执行层”的双层方案，避免一次性全量迁移。

### 环境初始化补充

1. 当前工作区原先缺少项目内 `.venv`，现已在仓库根目录创建。
2. 已执行 `.\.venv\Scripts\python.exe -m pip install -r requirements.txt`，完成 `pydantic`、`langgraph` 及其依赖安装。
3. 已使用项目内虚拟环境完成最小验证：
   - `.\.venv\Scripts\python.exe -m openmate_agent.cli --help`
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_cli.AgentCliTests -v`
4. 当前 agent 相关验证结果为 16 个测试全部通过。

## 2026-04-13

### 主干架构同步判断

1. 已读取 `architecture/sharedInfo/架构.md` 与 `architecture/sharedInfo/模块契约.md`。
2. 当前跨模块总原则已进一步明确为：统一 `CLI + JSON`，不跨模块直接引用内部实现。
3. 对 Agent 模块的直接影响主要有两点：
   - `Agent` 技术栈继续保持 `Python + Pydantic`
   - `schedule -> agent` 的 worker CLI/JSON 契约尚未冻结，当前不能假定已有执行 worker 命令

### 当前优先级判断

1. 其他模块切换 Go 不改变 Agent 的主目标，当前仍应优先提升 agent 表现而不是追随语言迁移。
2. Agent 近期最值得投入的方向应聚焦：
   - 上下文注入质量
   - 工具调用质量与安全策略
   - skill 注入与选择
   - 对 Pool 标准化调用结果的消费质量
   - 可评估、可回归的表现基线
3. 在 worker 契约冻结前，避免过早把 Agent 能力层绑死到调度执行语义上。

### 增量实现（结构化执行与多文件补丁）

1. 新增结构化命令工具 `exec`：
   - payload 固定为 `command/cwd/timeout_seconds/expect_json`
   - `command` 采用 argv 列表而非原始 shell 字符串
   - 默认值与现有 `shell` 保持统一：`cwd=None`、`timeout_seconds=30`
   - `expect_json=true` 时会校验 stdout 是否为合法 JSON
2. 新增结构化补丁工具 `patch`：
   - payload 为 `operations` JSON 列表
   - 首版支持两类操作：`replace`、`write`
   - 复用现有 `edit` 匹配策略与 `write` diff/诊断逻辑
   - 在同一次 patch 内支持多文件、多处修改
3. `exec/patch` 已与现有工具体系保持统一：
   - 已纳入 `ToolName / ToolRegistry / PermissionGateway / DefaultToolInjector`
   - 已纳入 `tool` CLI 子命令
   - 继续复用 `is_safe / is_read_only` 标记位与 `ToolResult` 返回风格
4. `shell` 保持兼容，未删除；默认工具注入顺序已将结构化工具优先暴露。
5. 已补充测试覆盖：
   - `exec` 成功执行
   - `exec` 的 `expect_json` 成功/失败路径
   - `patch` 多文件成功路径
   - `patch` 任一操作失败时不落地
   - CLI 的 `tool exec`、`tool patch` 与非法 JSON 参数
6. 已完成验证：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_cli.AgentCliTests -v`
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v`
7. 当前仓库全量单测结果：38 个测试全部通过。

## 2026-04-14

### Pool / VOS 工具调用协议对齐落地

1. `openmate_agent.service.execute()` 已改为 OpenAI Responses 原生循环：
   - 首轮请求注入 `tools/tool_choice/parallel_tool_calls`
   - 识别 `response.output[].type=function_call`
   - 执行本地工具后回传 `function_call_output`
   - 续调时使用 `previous_response_id`
2. 已新增 VOS SessionEvent 适配层：
   - `openmate_agent.session_models`（强类型 Session/SessionEvent 输入输出模型）
   - `openmate_agent.session_gateway.VosSessionGateway`（CLI + JSON 适配）
   - `openmate_agent.vos_binary`（`cmd/vos` 二进制构建与定位）
3. SessionEvent 写入语义已按共享契约执行：
   - `function_call` 写入 `next_status=waiting`
   - `function_call_output` 写入 `next_status=active`
   - 工具链路结束后写 `message` 并补写 `next_status=completed`
   - 异常路径补写 `next_status=failed`
   - 新增：即使本轮没有工具调用，只要配置了 VOS 网关，也会确保 Session 存在并写入一条 `message`（assistant 输出）用于对话落盘
4. 新增单测覆盖：
   - Responses 多轮工具调用循环
   - `previous_response_id` 续调
   - SessionEvent `waiting/active/completed/failed` 状态流转
5. 已完成验证：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_cli.AgentCliTests -v`
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v`
   - 当前结果：41 个测试全部通过

### 已记录但暂不实现（按当前任务边界）

1. 工具文件副作用治理（写/改/新增文件）仍待补齐：
   - 操作级审计字段标准化
   - 回滚策略（失败时自动恢复/半自动恢复）
   - 高风险写操作确认策略（与 SessionEvent/调度联动）

### 补充（SessionEvent V2 对齐）

1. 已将无工具调用场景从临时 `function_call_output` 迁移为标准 `message` 事件写入。
2. `call_id` 规则已收敛为“仅工具事件必填”，`message` 不强制 `call_id`。
3. VOS 存储层已放宽 `item_type` 约束为非空字符串，并补充旧库 `item_type` CHECK 约束自动迁移逻辑。
4. 回归结果：
   - Go：`go test ./internal/vos/...` 通过
   - Python：`unittest discover -s tests -p "test_*.py"`（41 项）通过
5. 本地部署验证：
   - 已构建 `.openmate/bin/vos.exe` 新版本
   - 已在 `.openmate/runtime/` 初始化新 `vos_state.json` 与 `vos_sessions.db`
   - 已验证 `message` 无 `call_id` 可写入、`function_call` 无 `call_id` 被拒绝
6. 已补充 Agent 文档中的 VOS 落盘配置说明与运行前置条件

### 文档收敛（2026-04-14）

1. 已将工具调用开发清单并入 `architecture/Agent能力/Agent能力.md`（统一主文档）。
2. 已删除冗余文档：
   - `architecture/Agent能力/Agent工具调用开发清单.md`
   - `architecture/Agent能力/Process-2026-04-14-工具调用清单.md`
3. 当前 `Agent能力` 文档区保留：
   - `Agent能力.md`（规范主文档）
   - `Process.md`（过程记录）

## 2026-04-15

### 协议收敛记录（ChatCompletions 清理 + 并行暂缓）

1. 已对 `openmate_agent / openmate_pool / internal/poolgateway / tests` 完成全仓检索与核对：
   - 未发现 `/v1/chat/completions` 路径调用。
   - 未发现旧 ChatCompletions `tool_calls/function_calling` 分支实现。
2. 当前主链路已统一为 OpenAI Responses：
   - provider 侧请求路径为 `/responses`。
   - agent 侧通过 `function_call -> function_call_output -> previous_response_id` 形成闭环。
3. 结论：Agent 层可视为已完成旧 ChatCompletions function-calling 收敛，不再保留该兼容路径。
4. 决策（本轮冻结）：
   - 暂不实现并行工具执行。
   - 维持 `parallel_tool_calls=false` 的串行执行策略。
5. 后续计划（未开工）：
   - 在后续迭代再实现并行工具调用与结果归并。

### 旧协议硬收敛落地（校验层）

1. 已在 Python 适配层 `OpenAIResponsesRequest` 增加显式拦截：当请求含旧 ChatCompletions 字段时直接拒绝。
   - 拦截字段：`messages / functions / function_call / tool_calls / max_tokens`
2. 已在 Go 网关 `validateInvokeRequest` 增加同等拦截，避免绕过 Python 适配层直接调用时出现旧字段回流。
3. 已新增测试覆盖：
   - Python：`tests/test_pool.py::test_request_rejects_chat_completions_fields`
   - Go：`internal/poolgateway/openai_responses_test.go::TestValidateInvokeRequestRejectsLegacyChatCompletionsFields`
4. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_pool -v` 通过（8 项）
   - `go test ./...` 通过（含 `internal/poolgateway`）
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（43 项）

### 能力层可插拔重构落地（4 组件 + 编排解耦）

1. 已新增可插拔构建管线 `BuildPipeline`：
   - 固定顺序执行 `ContextInjector -> ToolInjector -> SkillInjector -> Assembler`
   - 输出统一 `AgentInput`
2. 已新增执行编排层 `ExecutionOrchestrator` 与 `ExecutionRunner` 抽象：
   - 默认实现 `ResponsesExecutionRunner` 承载 OpenAI Responses 工具循环
   - 新增 `LangGraphExecutionRunner` 适配接缝（当前委托 fallback，保持行为不变）
3. `AgentCapabilityService.execute()` 已收敛为薄入口：
   - 先调用 `BuildPipeline.build(node_id)`
   - 再调用 `ExecutionOrchestrator.execute(...)`
   - 对外接口与现有调用方保持兼容
4. 服务层新增代码注入装配点（不引入配置驱动）：
   - `build_pipeline`
   - `execution_orchestrator`
   - `execution_runner`
5. 已新增测试覆盖（可插拔与解耦）：
   - `tests/test_pipeline_orchestration.py`
   - 覆盖构建顺序、异常冒泡、runner 注入、LangGraph 适配委托、service 对注入组件的消费
6. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_pipeline_orchestration -v` 通过（5 项）
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（48 项）

### ContextInjector 对接 VOS 聚合快照（四组件实现补齐）

1. 新增 `VosContextGateway`（`openmate_agent/context_gateway.py`）：
   - 通过 `vos context snapshot --node-id ...` 读取 VOS 聚合上下文。
   - 统一解析为强类型 `ContextSnapshotRecord`（`openmate_agent/context_models.py`）。
2. 新增 `VosContextInjector`（`openmate_agent/context_injector.py`）：
   - 将快照组装为单一上下文载荷（内部包含 `SystemPrompt.memory` + `UserPrompt.session`）。
   - 不再注入 `rendered_text/current_node_input/task`。
3. `AgentCapabilityService` 装配策略更新：
   - 若显式传入 `context_injector`，优先使用显式实现。
   - 否则当存在 `vos_state_file / vos_session_db_file / vos_binary_path` 任一配置时，默认切换到 `VosContextInjector`。
   - 无 VOS 配置时继续使用 `DefaultContextInjector`。
4. `DefaultAssembler` 调整：
   - 组装 prompt 时直接消费 `ContextBundle.payload`，不再依赖 `summary/snippets` 等中间概念。
5. 新增测试覆盖：
   - `tests/test_context_injector.py`（gateway 解析、空输出报错、injector 渲染、service 装配优先级）
6. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector -v` 通过（5 项）
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（53 项）

### Context 注入语义收敛（对外仅保留“上下文注入”）

1. `ContextBundle` 字段从 `summary/snippets` 收敛为单一 `payload`，避免暴露中间概念。
2. `VosContextInjector` 改为输出单一上下文载荷：
   - 内部包含 `SystemPrompt.memory`
   - 内部包含 `UserPrompt.session`
3. `DefaultAssembler` 不再拼接 `context=...` 文本行，改为直接消费 `ContextBundle.payload`。
4. 在组装/编排层补充必要英文注释：
   - `openmate_agent/pipeline.py`
   - `openmate_agent/orchestration.py`
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector tests.test_pipeline_orchestration tests.test_service -v` 通过（28 项）
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（53 项）

## 2026-04-17 统一数据库路径对齐（一期）

1. `openmate_agent.service.AgentCapabilityService` 默认 `pool_db_path` 改为 `.openmate/runtime/openmate.db`。
2. `VosContextGateway` 与 `VosSessionGateway` 默认路径改为：
   - `state_file`: `.openmate/runtime/vos_state.json`
   - `session_db_file`: `.openmate/runtime/openmate.db`
3. Agent 侧默认配置与 `vos --db-file`、`openmate-pool --db-file` 完成对齐，未显式传参即可走统一数据库联调。
4. 本轮范围内不引入历史库自动迁移，旧路径数据迁移由后续运维动作单独处理。
5. 回归结果：
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（53 项）

## 2026-04-17 Dead Code Cleanup

1. Removed unused default implementations `DefaultAgentExecutor` and `DefaultLlmGateway` from `openmate_agent.defaults`.
2. Kept public `priority()` contract and current session/tool orchestration behavior unchanged.
3. Verified Python unit tests (`unittest discover -s tests -p "test_*.py" -v`) passed.

## 2026-04-17 Assistant 文本增量事件落地（assistant_delta）

1. SessionEvent 枚举补充 `assistant_delta`，用于 assistant 文本增量回放。
2. `ResponsesExecutionRunner` 在写最终 `message` 前，按分段写入 `assistant_delta` 事件：
   - `item_type=assistant_delta`
   - `role=assistant`
   - `payload.delta=<文本分段>`
3. 工具调用事件语义保持不变，继续使用：
   - `function_call`
   - `function_call_output`
4. 单测已同步更新 `tests/test_service.py`，放宽事件数量断言并校验 `assistant_delta` 存在。
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（53 项）

## 2026-04-17 worker 复用已有 Session 修复（避免重复创建）

1. 问题定位：
   - 调度链路已先创建 session 并把 `session_id` 传给 worker。
   - Agent 侧 `VosSessionGateway.ensure_session()` 仍直接调用 `vos session create --session-id ...`，导致重复创建冲突（`session already exists`）。
2. 修复策略：
   - 当传入 `session_id` 时，先执行 `vos session get --session-id ...`。
   - 若 session 存在且 `node_id` 匹配：直接复用，不再创建。
   - 若返回 `session not found`：再走 `session create`。
   - 若 session 存在但属于其它 `node_id`：返回明确错误并中止执行。
3. 代码变更：
   - `openmate_agent/session_gateway.py`
     - `ensure_session()` 增加“先查后建”逻辑。
     - 新增 `_try_get_session()` 与 `_is_session_not_found_error()`。
4. 测试补充：
   - 新增 `tests/test_session_gateway.py`，覆盖：
     - 已有 session 复用
     - session 不存在后创建
     - session 与 node 不匹配拒绝
     - 未提供 session_id 时直接创建
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_session_gateway -v` 通过（4 项）
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service -v` 通过（18 项）
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（57 项）

## 2026-04-17 Windows 子进程 UTF-8 解码修复

1. 问题现象：
   - worker 执行链路在 Windows 下出现 `Exception in thread ... _readerthread`，来源为 Python `subprocess` 文本读取线程。
2. 根因定位：
   - 多处 `subprocess.run(..., text=True)` 未显式指定 `encoding`，默认系统代码页解码与 UTF-8 输出不一致时会触发读取线程异常。
3. Agent 侧修复：
   - `openmate_agent/session_gateway.py`
   - `openmate_agent/context_gateway.py`
   - `openmate_agent/vos_binary.py`
   - `openmate_agent/tooling/tools.py`
   - 统一补充 `encoding="utf-8", errors="replace"`。
4. 测试补充：
   - `tests/test_context_injector.py` 新增 `test_snapshot_runs_subprocess_with_utf8_decode`，校验 subprocess 调用参数。
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector tests.test_pool.PoolGatewayTestCase.test_run_command_uses_utf8_decode` 通过（7 项）
   - `.\.venv\Scripts\python.exe -m unittest tests.test_session_gateway` 通过（4 项）

## 2026-04-17 Responses input 首轮格式修正

1. 问题：首轮调用 Responses API 时，`input` 仍以字符串传递，不符合当前约定的消息数组形式。
2. 修复：
   - `openmate_agent/orchestration.py` 中 `ResponsesExecutionRunner` 首轮输入改为：
     - `input=[{"role":"user","content":"..."}]`
   - 工具续轮输入保持 `function_call_output` 数组，不变。
3. 测试更新：
   - `tests/test_service.py` 增加首轮 `request.input` 结构断言（数组 + `role=user`）。
4. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service.AgentCapabilityServiceTests.test_execute_runs_responses_tool_loop_and_writes_session_events` 通过

## 2026-04-21 Agent 组装层收敛重构（三服务 + 薄门面）

1. `openmate_agent/service.py` 已从“混合职责大类”收敛为薄门面，职责限定为：
   - 依赖装配（context/tool/skill/pipeline/gateway/session/tool runtime）。
   - 对外路由（execution/decompose/priority/tool）。
2. 新增三类能力服务实现：
   - `ExecutionAgentService`：保留 ReAct 执行链路（build -> tool schema -> orchestration）。
   - `DecomposeAgentService`：新增拆解模式骨架（workflow 入口，结构化 `tasks` 输出）。
   - `PriorityAgentService`：新增排序模式骨架（结构化 `priority_plan` 输出）。
3. `tool schema` 与 `tool runtime` 从 `service.py` 拆出：
   - 新增 `openmate_agent/tool_schema.py`（OpenAI function schema 组装）。
   - 新增 `openmate_agent/tool_runtime.py`（工具权限判定与执行器）。
4. CLI 扩展（与 `worker run` 并存）：
   - `python -m openmate_agent.cli decompose run`
   - `python -m openmate_agent.cli priority run`
   - 输入保持 `--request-json` / `--request-file` 二选一，输出结构化 JSON，退出码语义为 `0/1/2`。
5. 兼容性说明：
   - `worker run` 现有请求/响应契约保持不变，仍服务执行 Agent。
   - `priority()` 旧布尔接口保留（兼容旧调用点）。
6. 测试与回归：
   - 新增/更新 `tests/test_cli.py`（decompose/priority CLI）。
   - 新增/更新 `tests/test_service.py`（run_decompose/run_priority）。
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（67 项）。

## 2026-04-21 master 初始化（agent 开发前）

1. 已确认当前工作分支为 `master`，并按本轮要求保持不切分支。
2. 已读取并对齐以下文档：
   - `architecture/sharedInfo/Process.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/Agent能力/Agent能力.md`
3. 已核对 Agent 代码目录与本地 Python 环境入口：
   - `openmate_agent/` 模块结构可读
   - `.venv\Scripts\python.exe` 可用（`Python 3.12.2`）
4. 按当前协作指令，本次仅做初始化，不执行测试。

## 2026-04-21 AgentCapabilityService 命名收敛（方法名清晰化）

1. `openmate_agent/service.py` 对外主方法命名已收敛为：
   - `execute_agent(build: Build) -> str`
   - `decompose_agent(request: DecomposeRequest) -> DecomposeResponse`
   - `priority_agent(request: PriorityRequest) -> PriorityResponse`
2. 为避免一次性破坏现有调用方，旧方法名保留为兼容别名：
   - `execute(...)` -> `execute_agent(...)`
   - `run_decompose(...)` -> `decompose_agent(...)`
   - `run_priority(...)` -> `priority_agent(...)`
3. 调用方已同步迁移到新命名：
   - `openmate_agent/worker.py`
   - `openmate_agent/cli.py`
   - `tests/test_service.py`
   - `tests/test_pipeline_orchestration.py`
4. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（67 项）。

## 2026-04-21 AgentCapabilityService 兼容壳清理（删除无效别名）

1. 审计结论：`execute/run_decompose/run_priority` 在仓库内已无调用，属于纯兼容壳，不再产生业务价值。
2. 已在 `openmate_agent/service.py` 删除以下无效兼容方法：
   - `execute(self, build: Build) -> str`
   - `run_decompose(self, request: DecomposeRequest) -> DecomposeResponse`
   - `run_priority(self, request: PriorityRequest) -> PriorityResponse`
3. 当前对外主方法保持为：
   - `execute_agent`
   - `decompose_agent`
   - `priority_agent`
4. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（67 项）。

## 2026-04-21 Agent 文件收敛 P0（schema/model 薄层内联）

1. 已完成 `tool_schema.py -> agent_services.py` 内联合并：
   - 将工具参数 schema 与 OpenAI function payload 组装逻辑迁入 `openmate_agent/agent_services.py`。
   - `ExecutionAgentService` 改为调用模块内私有函数 `_build_openai_tools(...)`。
2. 已完成 `context_models.py -> context_gateway.py` 内联合并：
   - 将 `ContextSessionHistoryRecord`、`ContextSnapshotRecord` 模型迁入 `openmate_agent/context_gateway.py`。
   - `openmate_agent/context_injector.py` 与 `tests/test_context_injector.py` 改为从 `context_gateway` 导入 `ContextSnapshotRecord`。
3. 已删除已并入文件：
   - `openmate_agent/tool_schema.py`
   - `openmate_agent/context_models.py`
4. 行为边界保持不变：
   - 执行链路与工具 schema 输出语义不变。
   - VOS context snapshot 解析与注入 payload 语义不变。
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector tests.test_service tests.test_pipeline_orchestration -v` 通过（31 项）。
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（67 项）。

## 2026-04-21 Gateway 命名收敛（Reader/Writer）

1. 数据适配层命名已从 `Gateway` 收敛为 `Reader/Writer`：
   - `openmate_agent/context_gateway.py` -> `openmate_agent/context_reader.py`
   - `openmate_agent/session_gateway.py` -> `openmate_agent/session_writer.py`
   - `VosContextGateway` -> `VosContextReader`
   - `VosSessionGateway` -> `VosSessionWriter`
   - `ContextGatewayError` -> `ContextReaderError`
   - `SessionGatewayError` -> `SessionWriterError`
2. 协议层命名同步收敛：
   - `SessionEventGateway` -> `SessionEventWriter`（`openmate_agent/interfaces.py`）
3. 执行编排与服务装配同步更新：
   - `openmate_agent/orchestration.py` 统一使用 `session_writer`
   - `openmate_agent/service.py` 构造参数与装配改为 `session_writer`
4. 相关测试已同步：
   - `tests/test_context_injector.py`
   - `tests/test_session_gateway.py`（测试内容已切至 Writer 命名）
   - `tests/test_service.py`
   - `tests/test_pipeline_orchestration.py`
5. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_context_injector tests.test_session_gateway tests.test_service tests.test_pipeline_orchestration -v` 通过（35 项）。
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（67 项）。

## 2026-04-22 DecomposeAgent 模型驱动化（替换模板拆解）

1. `DecomposeAgentService` 从静态模板生成升级为真实模型调用：
   - 通过 `PoolGateway` 走 Responses 请求
   - 要求输出严格 JSON `tasks`
   - 解析失败/空任务均返回 `status=failed`
2. `DecomposeRequest` 补充可选 `context_snapshot` 字段，用于上游（VOS）显式传入上下文快照。
3. 拆解提示词收口规则：
   - 业务域优先拆解，不按技术栈拆解
   - 仅输出一层可执行子任务
   - 结构化 JSON 输出
4. CLI 回归对齐：
   - `tests/test_cli.py::test_decompose_run` 改为本地假网关 + 临时 `model.json`，验证真实模型调用路径
5. 新增/更新测试：
   - `tests/test_service.py`：
     - 拆解成功
     - 非法 JSON 失败
     - 空任务失败
   - `tests/test_cli.py`：`decompose run` 成功链路
6. 回归结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_cli -v` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）

## 2026-04-24 Compact Agent 输出契约升级（summary + memory_proposal）

1. `openmate_agent` compact 输出从单一 `memory` 改为双输出：
   - `summary`：Process 摘要
   - `memory_proposals`：topic_memory 候选提案（候选，不直接落库）
2. 模型变更：
   - `openmate_agent/models.py`
     - `CompactedProcess.memory -> summary`
     - 新增 `process_id` 与 `memory_proposals` 结构
3. 解析与注入链路变更：
   - `openmate_agent/agent_services.py`：compact prompt/解析改为结构化 JSON 双动作输出
   - `openmate_agent/context_reader.py`、`openmate_agent/context_injector.py`：`process_contexts.memory -> process_contexts.summary`
4. 新增测试：
   - `tests/test_service.py`：compact 成功输出 summary+proposal、异常输出失败路径
5. 回归：`python -m unittest discover -s tests` 通过（71 项）。


## 2026-04-26 Tool Monitor 最小可插拔增强（AOP日志 + CLI monitor）

1. `ToolRuntimeExecutor.run_tool` 已新增监控前后切面：
   - `before`：记录 `node_id/tool_name/source/is_safe/is_read_only/request_id/ts`。
   - `after`：统一覆盖 success/failure/blocked/invalid/上下文错误路径，记录 `success/error_code/duration_ms/ts`。
2. 新增监控模块 `openmate_agent/tool_monitor.py`：
   - `ToolMonitorEvent`（Pydantic 强类型）
   - `ToolMonitorStore`（append-only JSONL）
   - `ToolMonitorService`（list/summary 聚合，含 p95）
   - 持久化文件：`.openmate/runtime/tool_monitor.jsonl`（UTF-8 JSONL）。
3. 监控写入降级策略已落地：写失败不影响工具主流程（runtime 内部静默容错）。
4. CLI 已新增 `openmate-agent tools monitor` 分组：
   - `openmate-agent tools monitor list [--tool-name] [--node-id] [--source] [--success true|false] [--limit]`
   - `openmate-agent tools monitor summary [--tool-name] [--node-id] [--source] [--success true|false] [--limit] [--window-minutes]`
   - `--help` 已补参数说明与示例。
5. 回归结果：
   - `\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_tool_monitor tests.test_cli.AgentCliTests` 通过（58 项）。
