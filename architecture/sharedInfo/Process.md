# SharedInfo Process
## 2026-04-26 权限确认与放行记忆（VOS + Agent）

1. VOS 新增 Topic/User 权限管理能力（仅 allow 项，支持删除反悔）：
   - Topic 工具放行：`Topic.metadata.permission.tool_allows[]`（键：`tool_name + dir_prefix`，prefix 命中）。
   - User skill 放行：`User.user_permission.skill_allows[]`（键：`skill_name`）。
2. VOS CLI 新增命令组（均支持 `--help`）：
   - `vos permission topic list|add|delete --topic-id ...`
   - `vos permission user list|add|delete`
3. VOS HTTP v1 新增接口：
   - `GET/POST/DELETE /api/v1/topics/{topic_id}/permissions`
   - `GET/POST/DELETE /api/v1/user/permissions`
4. Agent 执行链路接入审批回调接缝：
   - 工具调用在执行链路支持 `allow_and_remember / allow_once / deny / supplement`。
   - CLI 直调保持原语义（未确认仍 blocked）。
   - `supplement` 不执行工具，继续回环；审批信息落 `mcp_approval_request` 事件。
5. 验证结果：
   - Go：`go test ./internal/vos/...` 通过；`go test ./...` 通过（仓库内 GOCACHE/GOMODCACHE）。
   - Python：`python -m unittest tests.test_permission_flow tests.test_service tests.test_tool_monitor -v` 通过（47 项）。
   - Python 全量发现测试已通过：`python -m unittest discover -s tests -p "test_*.py" -v`（104 项）。
## 2026-04-26 User 级数据骨架落地（VOS）

1. VOS 状态模型新增 `User` 一级对象，当前字段：
   - `user_memory`
   - `model_json`
   - `user_permission`（当前默认空 map，后续用于鉴权）
2. `ContextSnapshot.user_memory` 读取口径已从 `Topic.metadata.user_memory` 调整为优先读取 `User.user_memory`。
3. 兼容迁移已落地：
   - 加载旧状态时，若 `Topic.metadata.user_memory` 存在，会迁移并合并到 `User.user_memory`。
   - 迁移后会删除 Topic 上的旧 `user_memory` 键，完成“UserMemory 从 Topic 抽离”。
4. 本轮不改调度与 Agent 执行协议，仅收口 VOS 状态与上下文读取语义。
5. 验证结果：
   - `go test ./internal/vos/domain ./internal/vos/service ./internal/vos/cli` 通过
   - `go test ./...` 通过
## 2026-04-26 master 初始化（执行基线验证）

1. 已确认当前工作分支为 `master`，并保持在 `master` 上执行初始化。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/sharedInfo/Go仓库约定.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 基线验证结果：
   - `go test ./...` 通过（使用仓库内 `GOCACHE=.openmate/go/cache`、`GOMODCACHE=.openmate/go/modcache` 等价路径）。
   - CLI `--help` 冒烟通过：
     - `go run ./cmd/vos --help`
     - `go run ./cmd/openmate-schedule --help`
     - `go run ./cmd/openmate-pool --help`
     - `.\.venv\Scripts\python.exe -m openmate_agent.cli --help`
4. Python 全量单测未全部通过：
   - 命令：`.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v`
   - 结果：`Ran 100 tests`，`FAILED (failures=6)`
   - 失败用例：
     - `tests.test_cli.AgentCliTests.test_tool_exec`
     - `tests.test_cli.AgentCliTests.test_tool_grep_and_glob`
     - `tests.test_cli.AgentCliTests.test_tool_patch`
     - `tests.test_cli.AgentCliTests.test_tool_shell`
     - `tests.test_cli.AgentCliTests.test_tool_write_and_read`
     - `tests.test_tool_monitor.ToolMonitorTests.test_run_tool_records_before_and_after_for_all_outcomes`
## 2026-04-26 Topic Workspace 语义硬切（VOS + Agent + Frontend）

1. VOS HTTP `topics/{id}/workspace` 契约已切换为 `workspace`：
   - PUT 入参：`workspace`
   - GET/PUT 出参：`topic_id/workspace/created_at/updated_at`
   - 旧字段 `workspace_root` 不再兼容。
2. `vos node tool-context` 输出字段改为 `topic_workspace`（替换 `topic_workspace_root`）。
3. Agent ToolContext 字段改为 `workspace/topic_workspace/runtime_workspace`，移除 `*_root` 字段。
4. Agent 系统操作类工具执行策略调整为“禁止回退”：
   - Topic workspace 联查失败、未绑定或路径无效时，返回 `WORKSPACE_UNAVAILABLE` 且不执行工具。
   - `node_process/sibling_progress_board` 继续走 `runtime_workspace`。
5. 前端 Topic workspace 类型与调用已同步到 `workspace` 字段，避免与后端硬切不一致。

## 2026-04-26 Node->Topic workspace 联查命令（Agent 消费）

1. 新增 VOS CLI 命令：os node tool-context --node-id <ID>，一次返回 parent_id/node_name/topic_id/topic_workspace_root。
2. Agent Tool Runtime 改为消费该联查命令，不再拆成两次 VOS 查询。
3. 该收口不涉及 schedule 路由改造，仅优化 Agent 工具侧上下文解析链路。
## 2026-04-26 Topic.workspace 消费口径（Agent Tool Runtime）

1. Topic.workspace 的消费落在 Agent Tool 运行时，不强耦合到 schedule。
2. 消费策略：仅系统操作类工具读取 Topic.workspace 作为优先工作目录；失败回退仓库默认 workspace。
3. VOS 读写类工具继续走 runtime workspace，确保状态读写路径稳定。
4. 前端/后端接口契约保持不变，后续可在不改调度契约前提下迭代工具消费策略。
## 2026-04-26 Topic.workspace 字段化（脱离 metadata）

1. Topic 结构新增一级字段 workspace，不再以 metadata.workspace_root 作为主存储面。
2. VFS 兼容迁移已落地：读取旧状态时若存在 metadata.workspace_root，自动迁移到 Topic.workspace。
3. 迁移后会清理旧键：metadata.workspace_root 不再保留在 Topic metadata 中。
4. 现有接口保持不变：
   - GET /api/v1/topics/{topic_id}/workspace
   - PUT /api/v1/topics/{topic_id}/workspace
   - 返回结构仍为 topic_id/workspace_root/created_at/updated_at，前端无需改调用。
5. 回归结果：
   - go test ./internal/vos/service/... ./internal/vos/httpapi/... 通过（仓库内 GOCACHE/GOMODCACHE）。
   - go test ./... 通过。
## 2026-04-26 Topic-Workspace 绑定接口落地（VOS HTTP）

1. 后端已补齐前端所需接口：
   - GET /api/v1/topics/{topic_id}/workspace
   - PUT /api/v1/topics/{topic_id}/workspace
2. 数据语义：
   - 绑定值落在 Topic.metadata.workspace_root（不新增独立存储）。
   - GET 在未绑定时返回 data: null（HTTP 200）。
   - PUT 要求 workspace_root 非空字符串，返回最新绑定结构。
3. 服务层新增能力：
   - Service.GetTopicWorkspaceBinding(topicID)
   - Service.UpdateTopicWorkspaceBinding(topicID, workspaceRoot)
4. HTTP 返回结构对齐前端契约：topic_id/workspace_root/created_at/updated_at。
5. 回归结果：
   - go test ./internal/vos/service/... ./internal/vos/httpapi/... 通过（仓库内 GOCACHE/GOMODCACHE）。
   - go test ./... 通过。
## 2026-04-25 Agent Process 工具能力新增（node_process + sibling_progress_board）
1. Agent 默认工具集合新增 `node_process`，当前默认注入集合变更为：`read/write/search/command/network/node_process/tool_query`。
2. 新增非默认注册工具 `sibling_progress_board`（`primary_tag=process`），用于读取同级节点进度板（父节点 children 的 process `id/name`）。
3. 运行时在加载 registry 时会自动初始化 `.openmate/runtime/tool_registry.json`，确保包含 `sibling_progress_board` 注册项（`enabled=true`、`is_default=false`）。
4. 行为约束：当当前 node 为根节点（`parent_id` 为空）时，`sibling_progress_board` 返回空列表 `items: []`。
5. CLI 已新增：
   - `openmate-agent tool node_process --help`
   - `openmate-agent tool sibling_progress_board --help`

## 2026-04-25 Agent Tool Mechanism Refactor (Registry + On-Demand Discovery)
1. Agent tools switched to registry-driven runtime, loading built-in defaults plus JSON registrations from `.openmate/runtime/tool_registry.json`.
2. Prompt injection now exposes only six default tools (`read/write/search/command/network/tool_query`) and adds discovery policy for extensions.
3. Added threshold-based `tool_query` protocol (threshold=10): full details when small set, tag aggregation when large set, and by-tag drill-down.
4. Permission gateway now evaluates by registry metadata (enabled/backend) and keeps dangerous command blocking.
5. Added CLI management commands with `--help`: `openmate-agent tools list/register/update/enable/disable/validate`.
6. Regression tests passed:
   - `.\.venv\Scripts\python -m unittest tests.test_service tests.test_cli tests.test_pipeline_orchestration`
   - `.\.venv\Scripts\python -m unittest tests.test_worker tests.test_context_injector`

## 2026-04-25 master 初始化（不跑测试/不切分支，第七次）

1. 已确认当前工作分支为 `master`，并按本次要求保持不创建/切换分支。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 本轮遵循当前指令，不执行单元测试、不执行构建验证，仅完成初始化与过程沉淀。

## 2026-04-25 Node 旧 `process` 兼容字段下线

1. VFS `Node` 已下线旧兼容字段 `process`，当前仅保留 `process_ids` 作为稳定引用面。
2. `VfsState.Normalize()` 的旧数据迁移逻辑已删除，进入纯新结构运行阶段。
3. 回归结果：`go test ./...` 通过。

## 2026-04-25 master 初始化（不跑测试/不加分支，第六次）

1. 已确认当前工作分支为 `master`，并按本次要求保持不创建/切换分支。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 本轮遵循当前指令，不执行单元测试、不执行构建验证，仅完成初始化与过程沉淀。

## 2026-04-24 VFS Process 主键化与引用化（Node.process_ids）

1. VFS `Process` 已从 Node 内嵌列表升级为独立实体：
   - `ProcessItem` 新增 `id`。
   - `Node` 改为 `process_ids[]` 引用关系。
   - 状态文件新增 `processes` 集合承载 Process 实体。
2. 兼容口径：
   - `vos node update --process-json` 与 HTTP `PATCH /api/v1/nodes/{id}` 的 `process` 入参语义保持不变。
   - 读取 `process` 明细统一走服务层解析（`process_ids -> processes`）。
3. 影响范围：
   - `context snapshot`、`vos process list/compact`、HTTP `include=process` 已全部切换到引用解析路径。
4. 回归结果：
   - `go test ./internal/vos/...` 通过。
   - `go test ./...` 通过。

## 2026-04-24 master 初始化（不跑测试/不切分支，第五次）

1. 已确认当前工作分支为 `master`，并按本次要求保持不创建/切换分支。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 本轮遵循当前指令，不执行单元测试、不执行构建验证，仅完成初始化与过程沉淀。

## 2026-04-26 前端先行接入 Topic-Workspace 绑定契约

1. 前端已按新语义收敛为 `Topic -> Workspace(单值)`，并补齐 `NodeResponse.topic_id` 以支持从会话节点反查 topic。
2. 新增前端软依赖接口：
   - `GET /api/v1/topics/{topic_id}/workspace`
   - `PUT /api/v1/topics/{topic_id}/workspace`
3. 兼容策略：
   - 后端未接入（`404/501`）时前端仅 warning，不阻塞聊天、建 topic、任务树链路。
4. 交互策略：
   - 无当前 topic 时使用 `pendingWorkspaceRoot`（`sessionStorage`）暂存；
   - 下一个新 topic 创建成功后自动绑定一次并清空 pending；
   - 已有绑定允许覆盖，但提交前必须二次确认。
5. 验证：`frontend npm run build` 通过。

## 2026-04-24 frontend 初始化（不跑测试/不切分支）

1. 已按指令在 `frontend` 分支继续执行初始化，不创建、不切换分支。
2. 前端初始化已完成：`cd frontend && npm install`，结果 `up to date in 2s`。
3. 本轮仅做初始化沉淀，未执行单元测试与构建验证。

## 2026-04-24 VFS Process 驱动的上下文窗口改造

1. VFS `ProcessItem` 已扩展为上下文窗口载体：
   - 新增 `SessionRange`（`start_session_id / end_session_id / start_event_seq / end_event_seq`）。
   - 新增 `ProcessItem.memory`（压缩记忆）和 `ProcessItem.session_range`（Session 区间）。
2. `ContextSnapshot` 新增 `process_contexts` 字段：
   - 已完成 Process → 注入其 `memory`（压缩态）。
   - 进行中 Process → 注入其 `session_range` 内的完整 SessionEvent（详细态，当前焦点窗口）。
3. 上下文注入分层策略：
   - Layer 1: Topic.UserMemory + TopicMemory + GlobalIndex（持久记忆）
   - Layer 2: parent.Memory via `_child_memory_cache`（父级记忆，保持现有机制）
   - Layer 3: Process 记忆窗口（已完成=压缩，进行中=展开）
   - Layer 4: node.Memory + node.Input（当前节点）
4. Python Agent 侧同步：
   - `ContextSnapshotRecord` 新增 `process_contexts` 字段。
   - `VosContextInjector._render_payload()` SystemPrompt 新增 `process_contexts` 节。
5. 边界保证：CLI + JSON 契约自动兼容（新字段通过 JSON tag 反序列化）。

## 2026-04-24 CompactAgent + 压缩触发

1. `ProcessItem` 新增 `compacted_session_ids` 字段，追踪已压缩 Session，支持增量压缩。
2. Go 侧新增 `internal/vos/service/compact_service.go` + `internal/vos/cli/process_cli.go`，含 `vos process list/compact` CLI + `POST /api/v1/nodes/{id}/compact` HTTP。
3. Python 侧新增 `CompactAgentService`（固定工作流，遍历 processes 调 LLM 压缩）。
4. 三个触发时机：手动、>70% 上下文自动、`request_too_large` 恢复。
5. 回归：Go 测试 + Python 69 项全部通过。

## 2026-04-22 VOS Node 增加 process 对象（对话进度）

1. `vos` 侧 `Node` 进度结构已收敛为新 `process` 列表字段（完全替换旧 `progress`）：
   - `name`
   - `status`（`todo|done`）
   - `timestamp`
2. 旧字段处理：
   - `progress` 与旧 `process(current/history/updated_at)` 已下线，不再作为读写契约。
3. 接口变化：
   - `vos node update` 新增 `--process-json`。
   - `PATCH /api/v1/nodes/{id}` 支持 `process`。
   - `GET /api/v1/nodes/{id}?include=process` 可按需读取 `process`。
4. 验证结果：
   - `go test ./internal/vos/...` 通过（仓库内 `GOCACHE/GOMODCACHE`）。

## 2026-04-22 master 初始化（不跑测试/不创分支，第四次）

1. 已确认当前工作分支为 `master`，并按本次要求保持不创建新分支。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 本轮按指令不执行单元测试、不执行构建验证，仅完成初始化与过程沉淀。

## 2026-04-22 去除 default-topic 特例（首条对话独立 Topic）

1. 已完成跨模块改造：移除 `default-topic` 兜底语义，统一改为“首条对话即独立 Topic”。
2. 后端收敛：
   - `chat` 无 `node_id/topic_id` 时改为自动创建新 Topic，并直接使用该 Topic 的 root 节点承载会话。
   - `POST /api/v1/nodes` 与 `vos node create` 在 `topic_id/parent_id` 同时缺省时，自动创建新 Topic 后再创建节点。
   - `/api/v1/tree/roots` 回归 Topic root 聚合，不再做 `default-topic` 特殊展开。
3. 前端收敛：
   - Home 左侧历史项点击改为由 `children_count` 决策：`0` 留在 Home 切会话并回填历史，`>0` 跳转 Workspace。
4. 共享契约同步：
   - 已更新 `architecture/sharedInfo/模块契约.md`，删除 `default-topic` 描述并写入新语义。
5. 验证结果：
   - `go test ./internal/vos/service ./internal/vos/httpapi ./internal/vos/cli` 通过（使用仓库内 `GOCACHE/GOMODCACHE`）。
   - `go test ./...` 通过。
   - `cd frontend && npm run build` 通过。
   - Python 全量回归存在既有失败：`tests.test_store_sqlite.SqliteConcurrencyTestCase.test_concurrent_invoke_respects_max_concurrent`（期望 2，实际 3），本次未改 Python/Pool 并发逻辑。

## 2026-04-22 master 初始化（不跑测试）

1. 已确认当前工作分支为 `master`，并保持在 `master` 上继续开发。
2. 按当前协作约束，分支维持现状，仅保留 `master` 与 `frontend`，本次不新增 `vos/schedule/pool/agent` 分支。
3. 已完成初始化前置检查与契约读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
4. 按本次指令不执行单元测试与冒烟测试，本次仅完成初始化沉淀。

## 2026-04-21 master 初始化基线

1. 已确认当前工作分支为 `master`，并完成仓库结构与当前状态检查。
2. 已读取共享契约与各模块内部过程文档，包含：
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 已完成 Go 基线单元测试：`go test ./...`，结果通过。
4. 已完成 Python 基线单元测试：`.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v`，结果通过（63 项）。
5. 已完成 CLI 帮助冒烟校验，以下入口均可正常输出帮助：
   - `go run ./cmd/vos --help`
   - `go run ./cmd/openmate-schedule --help`
   - `go run ./cmd/openmate-pool --help`
   - `.\\.venv\\Scripts\\python.exe -m openmate_agent.cli --help`
6. 本次为初始化沉淀，不涉及业务逻辑修改。

## 2026-04-21 结构收敛（无功能改动）

1. 新增 Go 共享路径模块：`internal/openmate/paths/defaults.go`，统一默认路径与命令常量来源：
   - `.openmate/runtime/openmate.db`
   - `.openmate/runtime/vos_state.json`
   - `.openmate/bin/vos(.exe)`
   - worker 默认命令（优先 `.venv/Scripts/python.exe`）
2. 已将以下模块的硬编码默认路径收敛到共享路径模块：
   - `cmd/openmate-pool/main.go`
   - `cmd/openmate-vos-api/main.go`
   - `internal/vos/cli/cli.go`
   - `internal/schedule/cli.go`
   - `internal/openmate/runtime/runtime.go`
3. 新增 Python 共享路径模块：`openmate_shared/runtime_paths.py`，统一 `agent/pool` 的运行时默认路径解析。
4. 已将以下 Python 代码中的重复路径拼接替换为共享模块调用：
   - `openmate_agent/context_gateway.py`
   - `openmate_agent/session_gateway.py`
   - `openmate_agent/service.py`
   - `openmate_pool/pool.py`
5. 清理前端残留文件（不影响现有入口与路由）：
   - 删除空文件 `frontend/src/pages/Home/index_new.tsx`
   - 删除重复配置产物 `frontend/vite.config.js`
6. 回归验证结果：
   - `go test ./...` 通过
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
   - `frontend npm run build` 通过（首次在沙箱内触发 `spawn EPERM`，提权重跑后通过）

## 2026-04-21 CLI 帮助输出一致性修复（无功能改动）

1. 修复 Go CLI 在 `--help` 场景的重复输出问题：去掉 `flag.ErrHelp` 分支里的二次 `Usage()` 调用，避免帮助文本重复打印。
2. 影响模块：
   - `cmd/openmate-pool/main.go`
   - `internal/schedule/cli.go`
3. CLI 参数、命令名与业务 JSON 输出契约保持不变，仅调整帮助输出行为一致性。
4. 验证结果：
   - `go test ./...` 通过
   - `go run ./cmd/openmate-schedule --help`（使用仓库内 `GOCACHE/GOMODCACHE`）输出单次帮助
   - `go run ./cmd/openmate-pool --help`（使用仓库内 `GOCACHE/GOMODCACHE`）输出单次帮助
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）

## 2026-04-21 master 初始化（后端范围）

1. 已确认当前工作区分支为 `master`，并完成仓库状态检查。
2. 已按协作要求读取内部过程文档（后端范围）：
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
3. 已完成后端基线测试与帮助冒烟：
   - `go test ./...` 通过（使用仓库内 `GOCACHE/GOMODCACHE`）
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
   - `go run ./cmd/vos --help` 正常
   - `go run ./cmd/openmate-schedule --help` 正常
   - `go run ./cmd/openmate-pool --help` 正常
   - `.\\.venv\\Scripts\\python.exe -m openmate_agent.cli --help` 正常
4. 本次仅做初始化与验证，不涉及前端范围，也未修改业务逻辑代码。

## 2026-04-21 VOS 默认 Topic 收敛（default-topic）

1. 为降低 Topic 过度碎片化导致的调度不稳定，VOS 新增全局默认 Topic 语义：
   - 默认 Topic ID 固定为 `default-topic`。
   - 未显式传入 `topic_id` 的对话/节点创建默认落到该 Topic。
2. 行为变化：
   - `POST /api/v1/chat*` 无 `node_id` 时，不再每轮自动创建新 Topic；改为在目标 Topic（显式或默认）下创建会话 Node。
   - `POST /api/v1/nodes` 在 `topic_id`、`parent_id` 同时缺省时，默认落 `default-topic`。
   - `vos node create` 保持命令不变，但 `--topic-id` 语义调整为可选。
3. 调度边界保持不变：
   - `schedule enqueue` 仍要求有效 `topic_id`，由 VOS 上游注入保证。
4. 回归结果：
   - `go test ./...` 通过
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）

## 2026-04-21 首页历史展示根节点接口收敛（/tree/roots）

1. VOS `/api/v1/tree/roots` 语义调整为“展示根节点”：
   - 普通 Topic 显示其结构 root。
   - `default-topic` 显示其一级会话节点（不显示 default 结构 root）。
2. 该调整用于支持首页侧边栏“Topic roots + default 会话 roots”统一展示。
3. 实现由服务层 `ListDisplayRootNodes()` 承担，采用单次状态加载与内存筛选，减少重复 I/O。
4. 回归结果：`go test ./...` 通过。

## 2026-04-22 Agent 拆解落盘 + Schedule 脏化默认值对齐（CLI 优先）

1. `agent`：
   - `DecomposeAgentService` 改为真实模型驱动，不再走静态模板。
   - 输出要求为结构化 JSON `tasks`，解析失败/空任务返回 `status=failed`。
   - `DecomposeRequest` 新增可选 `context_snapshot` 透传字段。
2. `vos`：
   - 新增 `vos node decompose`（CLI-only）：
     - `--node-id`（必填）
     - `--hint`、`--max-items`、`--agent-command`（可选）
   - 执行链路：读取节点与 context snapshot -> 调 agent decompose run -> 创建直接子节点（默认 `ready`）。
   - 输出：`DecomposeResponse + created_nodes[]`。
   - HTTP `/api/v1/nodes/{id}/decompose` 继续保持 `501`，本轮未改。
3. `schedule`：
   - 业务节点 enqueue 标准化阶段强制 `MarkPriorityDirty=true`，避免同 Topic 新节点入队后不触发重排。
4. 回归结果：
   - Go：`go test ./internal/vos/cli ./internal/schedule` 通过
   - Go：`go test ./...` 通过
   - Python：`.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）
   - 帮助冒烟通过：
     - `python -m openmate_agent.cli --help`
     - `python -m openmate_agent.cli decompose run --help`
     - `python -m openmate_agent.cli priority run --help`
     - `go run ./cmd/openmate-schedule --help`
     - `go run ./cmd/vos --help`

## 2026-04-22 VOS Decompose HTTP 落地与前端契约对齐

1. VOS `node decompose` 已从 CLI-only 扩展到 HTTP：
   - 新增 `POST /api/v1/nodes/{id}/decompose`
   - 请求：`hint?`、`max_items?`
   - 响应：沿用 `{code,message,data}` envelope，`data` 为后端 `decompose` 输出（含 `tasks`、`created_nodes`）
2. 复用层收敛：
   - 新增服务层 `Service.DecomposeNode(...)`
   - CLI 与 HTTP 共用同一核心链路与错误语义
   - 新增可注入 `NodeDecomposeRunner`，便于 HTTP/Service 单测替换执行器
3. 前端契约以当前后端为准（不做旧字段双兼容）：
   - `frontend/src/types/models.ts` 中 `NodeDecomposeRequest/Response` 已切到新结构
   - Home/Workspace 的“生成任务树”成功提示改为 `created_nodes.length`
4. 未实现接口 `501` 保持在 `/api/v1/tree/generate` 等路径；`/nodes/{id}/decompose` 不再属于未实现列表。
5. 回归结果：
   - `go test ./internal/vos/httpapi ./internal/vos/cli ./internal/vos/service` 通过
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）
   - `frontend npm run build` 通过

## 2026-04-22 Chat 长输出超时策略修复（后端）

1. 修复前端长输出场景下出现 `context deadline exceeded` 的后端根因：流式调用链存在固定超时截断。
2. 变更点：
   - `internal/vos/httpapi/chat.go`
     - 移除 `chat.stream.invoke` 的 `context.WithTimeout(..., 2m)` 包装，改为由上层取消控制。
     - 移除 `waitChatTurn()` 的固定截止时间判定，不再因为本地时钟到期直接返回 `chat session timed out`。
   - `internal/poolgateway/providers.go`
     - Responses/ChatCompletions 在 `stream=true` 且未显式设置 `timeout_ms` 时，HTTP client 超时改为 `0`（不限时），避免客户端默认超时切断流式输出。
3. 影响范围：仅调整超时与取消策略，不改动 `chat/result`、SSE 事件结构和业务字段契约。
4. 回归结果：`go test ./internal/poolgateway/... ./internal/vos/httpapi/...` 通过。

## 2026-04-22 master 初始化（不跑测试/不加分支，第三次）

1. 已确认当前工作分支为 `master`，并按本次指令保持不切分支。
2. 已完成初始化前置读取：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Process.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 本轮遵循当前要求，不执行单元测试、不执行构建验证，仅完成初始化与过程沉淀。

## 2026-04-24 Compact 双动作共享变更（summary + memory proposal pending）

1. VOS compact 流程改为双动作：
   - Process 压缩摘要写入 `Process.summary`
   - 生成 `topic_memory` 提案并先落 pending，待用户确认后 apply
2. 硬切字段：`ProcessItem.memory -> ProcessItem.summary`（Go/Python/CLI/HTTP/上下文注入链路同步）。
3. 新增专用确认通道，避免复用 `topic update` 全量 metadata 覆盖：
   - CLI: `vos memory proposal list --topic-id ...`
   - CLI: `vos memory apply --topic-id ... --proposal-id ... --decision confirm|reject`
   - HTTP: `GET /api/v1/topics/{topic_id}/memory/proposals`
   - HTTP: `POST /api/v1/topics/{topic_id}/memory/apply`
4. 服务端门槛校验：空条目/低置信/证据不足/敏感键均丢弃，仅保留 summary 回写。
5. 回归：
   - `go test ./internal/vos/...` 通过
   - `go test ./...` 通过
   - `python -m unittest discover -s tests` 通过（71 项）


## 2026-04-26 Tool Monitor 最小可插拔增强（Agent + VOS HTTP）

1. Agent 工具运行时已新增旁路监控（AOP before/after），落盘 `.openmate/runtime/tool_monitor.jsonl`（UTF-8 JSONL，append-only）。
2. Agent CLI 新增监控查询分组：
   - `openmate-agent tools monitor list`
   - `openmate-agent tools monitor summary`
3. VOS HTTP v1 新增监控查询接口：
   - `GET /api/v1/tools/monitor/events`
   - `GET /api/v1/tools/monitor/summary`
4. 监控设计边界：仅记录与查询，不参与权限裁决与工具执行判定；写监控失败不影响主链路。
5. 验证结果：
   - Python：`\.venv\Scripts\python.exe -m unittest tests.test_service tests.test_tool_monitor tests.test_cli.AgentCliTests` 通过（58 项）。
   - Go：`go test ./internal/vos/httpapi/...`、`go test ./internal/vos/...` 通过（仓库内 `GOCACHE/GOMODCACHE`）。











