# Process 记录

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
