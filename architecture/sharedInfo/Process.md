# SharedInfo Process

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
