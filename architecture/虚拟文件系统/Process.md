# 虚拟文件系统 Process

## 2026-04-22 Node 新增 Process 对象（对话进度）

1. VOS `Node` 数据结构收敛为新 `process` 列表（替换旧 `progress`）：
   - `name`
   - `status`（`todo|done`）
   - `timestamp`
2. 旧字段处理：
   - 旧 `progress` 与旧 `process(current/history/updated_at)` 已移除。
3. 更新入口扩展：
   - CLI：`vos node update` 新增 `--process-json`（JSON array）。
   - HTTP：`PATCH /api/v1/nodes/{id}` 新增 `process` 字段。
   - HTTP 按需加载新增 `include=process`。
4. 回归结果：
   - `go test ./internal/vos/...` 通过（使用仓库内 `GOCACHE/GOMODCACHE`）。

## 2026-04-24 Process 驱动的上下文窗口改造

1. 扩展 `ProcessItem` 数据结构（`internal/vos/domain/types.go`）：
   - 新增 `SessionRange` 结构体：`start_session_id / end_session_id / start_event_seq / end_event_seq`。
   - `ProcessItem` 新增 `session_range`（`*SessionRange`，可选）和 `memory`（`map[string]any`，可选）。
   - `SessionRange.Closed()` 方法：`EndSessionID != ""` 表示区间已收敛（已完成）。
   - `Normalize()` 方法适配新字段。

2. 扩展 `ContextSnapshot`（`internal/vos/domain/context.go`）：
   - 新增 `ProcessContext` 结构体：`name / status / memory / session_events`。
   - `ContextSnapshot` 新增 `process_contexts` 字段，承载每个 Process 的上下文片段。

3. 改造 `GetContextSnapshot()`（`internal/vos/service/context_service.go`）：
   - 新增 `buildProcessContexts()`：按 Process 分层组装上下文。
     - 已完成 Process（有 `memory`）→ 注入其 Memory（压缩态）。
     - 进行中 Process（`session_range` 未收敛）→ 注入 `session_range` 内的完整 SessionEvent（详细态）。
   - 新增 `loadSessionRangeEvents()`：按 session_range 的 event_seq 区间过滤加载 Session 事件。
   - `resolveContextNodeMemory()` 保持现有行为不变（父节点 Memory 读取）。

4. 深拷贝适配：
   - `service.cloneProcessItems()` 和 `httpapi.cloneProcessItems()` 改为逐项深拷贝（`SessionRange` 指针 + `Memory` map）。
   - `httpapi.server.go` 新增 `cloneMapNil()` 辅助函数。

5. Python Agent 层适配：
   - `openmate_agent/context_reader.py`：
     - 新增 `ProcessContextRecord` Pydantic 模型。
     - `ContextSnapshotRecord` 新增 `process_contexts` 字段。
   - `openmate_agent/context_injector.py`：
     - `_render_payload()` 改为分层注入——SystemPrompt 新增 `process_contexts` 节。
     - 已完成 Process 输出其 `memory`，进行中 Process 输出其 `session_events`。

6. 架构文档同步：
   - `architecture/虚拟文件系统/虚拟文件系统.md`：更新 `process` 字段语义，新增 5.8 Process 驱动的上下文窗口章节。

7. 回归结果：
   - Go：`go test ./...` 通过。
   - Python：`.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）。

## 2026-04-24 CompactAgent + 三个压缩触发时机

1. 新增 `ProcessItem.compacted_session_ids` 字段，记录已压缩的 Session ID，支持增量压缩追踪。

2. **Process CLI（Go）**：
   - `vos process list --node-id X`：列出 Node 的所有 Process 摘要。
   - `vos process compact --node-id X`：触发压缩 Agent。
   - 核心逻辑：`findUncompactedSessions()` 遍历 SessionHistory，找出未被 `compacted_session_ids` 覆盖的未压缩 Session。

3. **CompactAgent Service（Go）**：
   - `internal/vos/service/compact_service.go`：
     - `CompactAgentRequest/CompactAgentResponse` 请求/响应模型。
     - `CompactRunner` 接口 + `CommandCompactRunner` 实现（shell-out to `python -m openmate_agent.cli compact run`）。
     - `Service.CompactProcesses()` 编排方法。
   - `POST /api/v1/nodes/{id}/compact` HTTP 端点，遵循 `/decompose` 路由模式。

4. **CompactAgent（Python 固定工作流）**：
   - `openmate_agent/models.py`：新增 `CompactRequest/CompactResponse/CompactedProcess/CompactProcessInput` 模型。
   - `openmate_agent/agent_services.py`：新增 `CompactAgentService`，遍历 processes 提取无压缩 Session 事件，调用 LLM 压缩为结构化 Memory。
   - `openmate_agent/cli.py`：新增 `compact run` 子命令。
   - `openmate_agent/service.py`：`AgentCapabilityService` 新增 `compact_agent()` 方法。

5. **三个压缩触发时机**：
   - **手动触发**：`POST /api/v1/nodes/{id}/compact` + `vos process compact --node-id X`。
   - **上下文 >70% 自动触发**：`VosContextInjector.inject()` 检测 payload 超过 `compact_threshold_chars`（默认 180K），自动调 `VosContextReader.compact_node()` 再重新注入。
   - **request_too_large 恢复**：`ResponsesExecutionRunner` 捕获 `InvocationFailedError` 并检测 `too_long/context_length_exceeded/request_too_large` 等错误码，调 `compact_trigger`，抛出 `ContextTooLargeError`，`ExecutionAgentService` 重建上下文后重试。

6. 新增文件：
   - `internal/vos/cli/process_cli.go`（CLI 命令）
   - `internal/vos/service/compact_service.go`（Compact 服务 + Runner）

7. 回归结果：
   - Go：`go test ./internal/vos/...` 通过。
   - Python：`python -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）。

## 2026-04-24 CompactAgent + 三个压缩触发时机

1. 新增  字段，记录已压缩的 Session ID，支持增量压缩追踪。

2. **Process CLI（Go）**：
   - ：列出 Node 的所有 Process 摘要。
   - ：触发压缩 Agent。
   - 核心逻辑： 遍历 SessionHistory，找出未被  覆盖的未压缩 Session。

3. **CompactAgent Service（Go）**：
   - ：
     -  请求/响应模型。
     -  接口 +  实现（shell-out to {"status": "failed", "error": "compact run requires exactly one of --request-json or --request-file"}）。
     -  编排方法。
   -  HTTP 端点，遵循  路由模式。

4. **CompactAgent（Python 固定工作流）**：
   - ：新增  模型。
   - ：新增 ，遍历 processes 提取无压缩 Session 事件，调用 LLM 压缩为结构化 Memory。
   - ：新增 
 Áгö D:\XuKai\Project\openmate Ð¼ӵ½´ËĿ¼µÄÎļþ½«²»»áµõ½ѹËõ¡£


ÔÚĿ¼ 1 ÏµÄ 0 ¸öÎļþÖУ¬ÓÐ
0 ¸öÒѱ»ѹËõ£¬0 ¸öδѹËõ¡£
×ܹ² 0 ×ֽÚÊý¾ݱ£´æÔÚ 0 ×ֽÚÖС£
ѹËõÂÊΪ 1.0 µ½ 1¡£ 子命令。
   - ： 新增  方法。

5. **三个压缩触发时机**：
   - **手动触发**： + 。
   - **上下文 >70% 自动触发**： 检测 payload 超过 （默认 180K），自动调  再重新注入。
   - **request_too_large 恢复**： 捕获  并检测  等错误码 → 调  → 抛出  →  重建上下文后重试。

6. 新增文件：
   - （CLI 命令）
   - （Compact 服务 + Runner）

7. 回归结果：
   - Go：ok  	vos/internal/vos/cli	(cached)
?   	vos/internal/vos/domain	[no test files]
ok  	vos/internal/vos/httpapi	(cached)
ok  	vos/internal/vos/service	(cached)
ok  	vos/internal/vos/store	(cached) 通过。
   - Python：usage: pool [--db-file DB_FILE] [--model-config MODEL_CONFIG] {invoke,cap,records,usage,sync} ...

OpenMate LLM gateway CLI. All commands print JSON to stdout.
{
  "invocation_id": "bebd3928-3d87-18a9-bebd-3d87bebd3928",
  "request_id": "req-node-fail",
  "node_id": "node-fail",
  "status": "failure",
  "route": {
    "api_id": "api-1",
    "provider": "openai_compatible",
    "model": "gpt-4.1"
  },
  "response": null,
  "output_text": null,
  "usage": null,
  "timing": {
    "started_at": "2026-04-24T08:26:21.2043226Z",
    "finished_at": "2026-04-24T08:26:21.2201462Z",
    "latency_ms": 15
  },
  "error": {
    "code": "provider_http_error",
    "message": "provider returned HTTP 500",
    "retryable": false,
    "provider_status_code": 500,
    "details": {
      "body": "{"error": {"message": "gateway failed"}}",
      "path": "/v1/responses"
    }
  }
}
{
  "invocation_id": "c34ce498-3d87-18a9-c34c-3d87c34ce498",
  "request_id": "req-node-fail-2",
  "node_id": "node-fail-2",
  "status": "failure",
  "route": {
    "api_id": "api-1",
    "provider": "openai_compatible",
    "model": "gpt-4.1"
  },
  "response": null,
  "output_text": null,
  "usage": null,
  "timing": {
    "started_at": "2026-04-24T08:26:21.280847Z",
    "finished_at": "2026-04-24T08:26:21.2997236Z",
    "latency_ms": 18
  },
  "error": {
    "code": "provider_http_error",
    "message": "provider returned HTTP 500",
    "retryable": false,
    "provider_status_code": 500,
    "details": {
      "body": "{"error": {"message": "gateway failed"}}",
      "path": "/v1/responses"
    }
  }
}
{
  "invocation_id": "e6745e38-3d87-18a9-e674-3d87e6745e38",
  "request_id": "req-node-1",
  "node_id": "node-1",
  "status": "success",
  "route": {
    "api_id": "api-1",
    "provider": "openai_compatible",
    "model": "gpt-4.1"
  },
  "response": {
    "id": "resp-cli",
    "model": "gpt-4.1",
    "object": "response",
    "output": [
      {
        "content": [
          {
            "text": "echo:hello-cli",
            "type": "output_text"
          }
        ],
        "role": "assistant",
        "status": "completed",
        "type": "message"
      }
    ],
    "status": "completed",
    "usage": {
      "input_tokens": 2,
      "input_tokens_details": {
        "cached_tokens": 0
      },
      "output_tokens": 3,
      "output_tokens_details": {
        "reasoning_tokens": 0
      },
      "total_tokens": 5
    }
  },
  "output_text": "echo:hello-cli",
  "usage": {
    "input_tokens": 2,
    "output_tokens": 3,
    "total_tokens": 5,
    "cached_input_tokens": 0,
    "reasoning_tokens": 0,
    "latency_ms": 19,
    "cost_usd": null
  },
  "timing": {
    "started_at": "2026-04-24T08:26:21.8706366Z",
    "finished_at": "2026-04-24T08:26:21.8897456Z",
    "latency_ms": 19
  },
  "error": null
}
[
  {
    "invocation_id": "e6745e38-3d87-18a9-e674-3d87e6745e38",
    "request": {
      "request_id": "req-node-1",
      "node_id": "node-1",
      "request": {
        "input": "hello-cli"
      },
      "timeout_ms": null,
      "route_policy": {
        "api_id": null
      }
    },
    "status": "success",
    "route": {
      "api_id": "api-1",
      "provider": "openai_compatible",
      "model": "gpt-4.1"
    },
    "response": {
      "id": "resp-cli",
      "model": "gpt-4.1",
      "object": "response",
      "output": [
        {
          "content": [
            {
              "text": "echo:hello-cli",
              "type": "output_text"
            }
          ],
          "role": "assistant",
          "status": "completed",
          "type": "message"
        }
      ],
      "status": "completed",
      "usage": {
        "input_tokens": 2,
        "input_tokens_details": {
          "cached_tokens": 0
        },
        "output_tokens": 3,
        "output_tokens_details": {
          "reasoning_tokens": 0
        },
        "total_tokens": 5
      }
    },
    "output_text": "echo:hello-cli",
    "usage": {
      "input_tokens": 2,
      "output_tokens": 3,
      "total_tokens": 5,
      "cached_input_tokens": 0,
      "reasoning_tokens": 0,
      "latency_ms": 19,
      "cost_usd": null
    },
    "timing": {
      "started_at": "2026-04-24T08:26:21.8706366Z",
      "finished_at": "2026-04-24T08:26:21.8897456Z",
      "latency_ms": 19
    },
    "error": null,
    "attempts": [
      {
        "attempt_id": "e6745e38-3d87-18a9-e674-3d87e6745e38",
        "route": {
          "api_id": "api-1",
          "provider": "openai_compatible",
          "model": "gpt-4.1"
        },
        "status": "success",
        "timing": {
          "started_at": "2026-04-24T08:26:21.8706366Z",
          "finished_at": "2026-04-24T08:26:21.8897456Z",
          "latency_ms": 19
        },
        "usage": {
          "input_tokens": 2,
          "output_tokens": 3,
          "total_tokens": 5,
          "cached_input_tokens": 0,
          "reasoning_tokens": 0,
          "latency_ms": 19,
          "cost_usd": null
        },
        "error": null
      }
    ]
  }
]
{
  "node_id": null,
  "limit": null,
  "invocation_count": 1,
  "success_count": 1,
  "failure_count": 0,
  "attempt_count": 1,
  "retry_count": 0,
  "input_tokens": 2,
  "output_tokens": 3,
  "total_tokens": 5,
  "cached_input_tokens": 0,
  "reasoning_tokens": 0,
  "total_cost_usd": null,
  "avg_latency_ms": 19,
  "max_latency_ms": 19,
  "generated_at": "2026-04-24T08:26:22.0108519Z"
}
{
  "total_apis": 1,
  "total_slots": 1,
  "available_slots": 1,
  "leased_slots": 0,
  "offline_apis": 0,
  "throttled": false,
  "updated_at": "2026-04-24T08:26:22.0812203Z"
}
{
  "synced": true,
  "capacity": {
    "total_apis": 1,
    "total_slots": 1,
    "available_slots": 1,
    "leased_slots": 0,
    "offline_apis": 0,
    "throttled": false,
    "updated_at": "2026-04-24T08:26:22.1470642Z"
  }
}
usage: pool [--db-file DB_FILE] [--model-config MODEL_CONFIG] {invoke,cap,records,usage,sync} ...

OpenMate LLM gateway CLI. All commands print JSON to stdout. 通过（69 项）。

## 2026-04-22 去除 default-topic 特例，切换到独立 Topic 语义

1. 服务层移除 `default-topic` 相关逻辑：
   - 删除 `EnsureDefaultTopic()` 兜底路径。
   - `CreateNode()` 在 `topic_id/parent_id` 同时缺省时，自动创建新 Topic（名称取节点名），并在该 Topic root 下创建节点。
2. `tree/roots` 聚合收敛为“按 Topic root 返回”：
   - `ListDisplayRootNodes()` 不再对历史会话做 `default-topic` 子节点展开。
3. HTTP 适配层同步：
   - `POST /api/v1/nodes` 去掉 `default-topic` 推断，改为透传 `topic_id/parent_id` 给服务层，由服务层完成自动建 Topic。
4. Chat 会话入口同步：
   - `resolveChatNode()` 在无 `node_id/topic_id` 时创建新 Topic，并直接返回其 root 节点作为会话节点。
   - 首条消息摘要命名去掉 `Chat:` 前缀，保留原 24 字符截断策略。
5. 测试更新：
   - `internal/vos/service/service_test.go`
   - `internal/vos/httpapi/server_test.go`
   - `internal/vos/cli/cli_test.go`
6. 验证结果：
   - `go test ./internal/vos/service ./internal/vos/httpapi ./internal/vos/cli` 通过（仓库内 `GOCACHE/GOMODCACHE`）。
   - `go test ./...` 通过。

## 2026-04-11 初始化

1. 已读取当前 `Process.md`（初始化时为空）和 [虚拟文件系统.md](D:/XuKai/Project/vos/architecture/虚拟文件系统/虚拟文件系统.md) 作为本轮开发输入。
2. 新建 `openmate_vos` 包，完成 `Topic/Node/VfsState` 的 Pydantic 强类型模型。
3. 已补基础服务层 `VirtualFileSystemService`，覆盖：
   - Topic 创建、查询、列表
   - Node 创建、查询、列表、移动、删除
   - 叶子节点可操作校验
   - `session/memory/input/output/progress/status` 的基础挂载更新
4. 已补 `vos` CLI，当前支持：
   - `topic create/get/list`
   - `node create/get/list/children/move/delete/update/leaf`
   - 所有命令支持 `--help`
5. 当前状态存储采用轻量 JSON 文件 `.vos_state.json`，用于初始化阶段先把对象边界和树约束固定下来。
6. 已新增单元测试：
   - `tests/test_vos_service.py`
   - `tests/test_vos_cli.py`
7. 环境侧发现仓库快照中没有现成 `.venv`，已在项目根创建 `.venv`，并执行 `pip install -r requirements.txt` 完成依赖安装。
8. 验证结果：
   - `.\.venv\Scripts\python.exe -m unittest tests.test_vos_service tests.test_vos_cli` 通过
   - 全量 `unittest discover -s tests` 未全部通过，失败点位于现有 `openmate_pool` 的 SQLite 文件句柄清理，不属于本次 VFS 初始化改动

## 下一步建议

1. 将 VFS 状态存储从 JSON 升级到更稳定的存储层（SQLite 或等价方案）。
2. 细化 `status` 状态机和叶子节点操作约束。
3. 继续补 `memory` 的“子写父读”聚合流程，而不是只停留在字段挂载层。

## 2026-04-11 Go 迁移

1. 依据明确需求，将虚拟文件系统实现从 Python 切换为 Go；该调整只针对 VFS 模块，不影响仓库中其他 Python 模块。
2. 已在仓库根新增 Go 模块与 CLI 入口：
   - `go.mod`
   - `cmd/vos`
   - `internal/vos/domain`
   - `internal/vos/store`
   - `internal/vos/service`
   - `internal/vos/cli`
3. 已完成 Go 版 `Topic/Node/VfsState` 强类型结构、领域错误、JSON 文件存储和树操作服务。
4. 已完成 Go 版 `vos` CLI，当前命令覆盖：
   - `topic create/get/list`
   - `node create/get/list/children/move/delete/update/leaf`
5. 当前仍使用 `.vos_state.json` 作为状态文件；本轮只迁语言和运行时，不升级到 SQLite。
6. 已补 Go 测试：
   - `internal/vos/service/service_test.go`
   - `internal/vos/cli/cli_test.go`
7. 已删除 Python 版 `openmate_vos` 与对应 Python 测试，避免仓库长期维护两套 VFS 实现。
8. 验证结果：
   - `go test ./...` 通过
   - 由于本机默认 Go 缓存目录无写权限，测试执行时使用仓库内 `GOCACHE` 目录
9. 已在 `architecture/sharedInfo/Go依赖.md` 新建 Go 依赖说明，明确当前 VFS Go 模块无第三方依赖，供其他模块后续复用与维护。

## 当前建议

1. 下一阶段优先补 `go run ./cmd/vos ...` 的实际使用文档和示例。
2. 等 CLI 接口稳定后，再评估将 `.vos_state.json` 升级到 SQLite。
3. `memory` 的父节点聚合逻辑仍未实现，后续要在 Go 服务层继续补齐。

## 2026-04-11 共享约定补充

1. 已在共享文档区新增 [Go仓库约定.md](D:/XuKai/Project/vos/architecture/sharedInfo/Go仓库约定.md)，明确单仓库单 `go.mod` 的 Go 合并策略。
2. 已在共享架构文档中补充当前协作状态：`vos`、`pool` 均已从 Python 转向 Go，但 `pool` 代码以其分支为准。
3. 此次为文档沉淀，不涉及 VFS 代码逻辑变更。

## 2026-04-11 Worktree 合并文档补充

1. 已在共享文档区新增 [Worktree合并流程.md](D:/XuKai/Project/vos/architecture/sharedInfo/Worktree合并流程.md)，记录当前仓库的多 worktree 分支合并步骤。
2. 文档明确了 `vos/pool/openmate` 三个 worktree 下的推荐合并路径，以及 `go.mod`、`cmd`、`internal` 等高冲突区域的收敛规则。
3. 此次仍为文档沉淀，不涉及代码逻辑变更。

## 2026-04-11 项目内 Skill 沉淀

1. 已在项目根新增 `skills/git-collaboration-guide`，作为项目内可复用的 Git 协作技能目录。
2. 该 skill 已完成中文化，内容覆盖：
   - 多 worktree 场景下的分支合并流程
   - 单仓库单 `go.mod` 的收敛规则
   - `cmd/`、`internal/`、`.gitignore`、`sharedInfo` 的冲突处理重点
3. 当前 skill 文件包含：
   - `skills/git-collaboration-guide/SKILL.md`
   - `skills/git-collaboration-guide/agents/openai.yaml`
   - `skills/git-collaboration-guide/references/worktree-merge-playbook.md`
   - `skills/git-collaboration-guide/references/go-structure-rules.md`

## 2026-04-13 master 对齐补齐

1. 对齐最新 `master` 后的 Go VFS 契约，补齐 Topic 侧 `update/delete`，让当前 CLI 与共享文档中的 Topic CRUD 基线一致。
2. 为 `node list` 补齐面向调度的叶子节点查询能力，新增：
   - `--leaf-only`
   - `--status`
   - `--exclude-status`
3. 为 `node update` 补齐运行态原子更新约束，新增 `--expected-version`，基于 `Node.version` 做乐观并发校验。
4. 在 Go 服务层补齐 `memory` 的“子写父读”聚合流程：当子节点 `memory` 发生变化时，把子节点快照写入父节点 `memory._child_memory_cache`；纯结构变化（如 move/delete）不自动重算父节点记忆。
5. 新增/补齐 Go 测试覆盖：
   - Topic update/delete
   - 叶子节点过滤查询
   - 版本冲突拒绝
   - 父节点 memory 聚合
   - 结构变更不触发 memory 重算
6. 验证结果：
   - `go test ./internal/vos/...` 通过
   - `go test ./...` 通过
   - `go run ./cmd/vos --help` 正常输出
7. 测试与构建继续使用仓库内缓存：
   - `GOCACHE=D:\XuKai\Project\vos\.openmate\go-build-cache`
   - `GOMODCACHE=D:\XuKai\Project\vos\.openmate\go-mod-cache`

## 2026-04-13 Session SQLite 落地

1. 已为 VFS 新增独立 Session 存储层，不再把会话正文塞进 `.vos_state.json`。
2. 当前 Session 数据模型冻结为：
   - `Session(id, node_id, status, created_at, updated_at, last_seq)`
   - `SessionEvent(id, session_id, seq, kind, role, call_id, payload_json, created_at)`
3. 当前 Session 持久化采用独立 SQLite 文件 `.vos_sessions.db`，并复用仓库现有 `github.com/mattn/go-sqlite3`。
4. `Node.session` 继续只保存 `session_id` 引用；创建 Session 时会同步把 `session_id` 挂到对应 Node 上。
5. 当前 CLI 已补：
   - `session create`
   - `session get`
   - `session append-event`
   - `session events`
6. 当前 `SessionEvent.kind` 第一版冻结为：
   - `user_message`
   - `assistant_message`
   - `tool_call`
   - `tool_result`
   - `status`
   - `error`
7. 当前实现默认不持久化 token 级流式 `delta`，只持久化稳定事件；若后续确实需要精细回放，再评估新增事件类型。
8. 验证结果：
   - `go test ./internal/vos/...` 通过
   - `go test ./...` 通过
   - `go run ./cmd/vos --help` 正常输出

## 后续查询方向

1. 按 `node_id` 列出该 Node 下的 Session 摘要。
2. 按 `session_id` 分页读取 `SessionEvent`，支持 `after_seq` 增量获取。
3. 评估是否需要给 Session 增加轻量热路径摘要字段，而不是每次都依赖事件流回放。
4. 评估是否需要把超大 `payload_json` 拆成外部 artifact 引用。
## 2026-04-14 工具调用 SessionEvent 契约补充

1. 已在 sharedInfo 新增《工具调用-SessionEvent契约》文档，明确 Agent 层接入 VOS Session 的字段与时序约定。
2. 契约明确当前 `item_type` 仅支持 `function_call` 与 `function_call_output`，并要求 `call_id` 必填。
3. 契约同步明确幂等建议（`session_id + call_id + item_type`）与失败载荷结构，避免上层实现分歧。
4. 兼容策略已收口：不再接受旧 `kind` 与 `open/closed`，旧 schema 需手工迁移或重建库。

## 2026-04-15 Context 聚合快照接口

1. 新增 VOS 聚合读取能力：`vos context snapshot --node-id <NODE_ID>`。
2. 新增领域输出结构 `ContextSnapshot`，对外固定字段：
   - `node_id`
   - `user_memory`
   - `topic_memory`
   - `node_memory`（优先父节点 `memory`，无父节点时回退当前节点）
   - `global_index`
   - `session_history`
3. `session_history` 聚合规则：
   - 按 `Node.session` 顺序遍历全部 session（不分页、不限流）
   - 每个 session 内按 `seq` 升序返回全部事件
4. 新增服务层实现：
   - `Service.GetContextSnapshot(nodeID string)`
   - 对 `sessionStore` 未配置、`node_id` 缺失等错误路径给出显式返回
5. 新增 CLI 子命令：
   - root 资源新增 `context`
   - `vos context <snapshot> [flags]`
6. 测试覆盖新增：
   - `internal/vos/service/context_service_test.go`
   - `internal/vos/cli/context_cli_test.go`
   - 覆盖父节点记忆优先、根节点回退、多 session/事件顺序、参数校验
7. 验证结果：
   - `go test ./internal/vos/...` 通过
   - `go test ./...` 通过

## 2026-04-17 Session 数据并入统一 SQLite（一期）

1. `vos` CLI 新增统一参数 `--db-file`，默认值为 `.openmate/runtime/openmate.db`。
2. `--session-db-file` 改为覆盖参数；未显式设置时默认跟随 `--db-file`。
3. `state_file` 默认路径同步调整为 `.openmate/runtime/vos_state.json`，保持状态文件与会话库同目录管理。
4. `internal/vos/store/session_store.go` 增补 SQLite 并发稳定性：
   - 写路径 busy/locked 重试。
   - `PRAGMA busy_timeout = 5000`。
   - `PRAGMA journal_mode = WAL` 持续启用。
5. 新增 `internal/vos/cli/session_cli_test.go` 用例，覆盖 `--db-file` 驱动 session 命令成功链路。
6. 回归结果：`go test ./...` 通过。

## 2026-04-17 Dead Code Cleanup

1. Removed unused service wrapper `Service.ListNodes(topicID)` and kept `ListNodesByFilter` as the single list entrypoint.
2. Verified VOS module tests with repo-local `GOCACHE/GOMODCACHE` and full `go test ./...` passed.

## 2026-04-17 VOS Web 前端与 HTTP JSON 适配（预览）

1. 新增独立命令入口 `cmd/openmate-vos-web`，用于同时启动：
   - 内嵌前端页面（`internal/vos/httpapi/web`）
   - VOS JSON HTTP API（`internal/vos/httpapi`）
2. 当前前端 MVP 能力：
   - Topic 列表与创建
   - Topic 维度 Node 列表与创建
   - 基础状态提示与刷新
3. 当前 HTTP API 已覆盖 Topic/Node 主链路：
   - `GET/POST /api/topics`
   - `GET/PATCH/DELETE /api/topics/{topic_id}`
   - `GET /api/topics/{topic_id}/nodes`
   - `POST /api/nodes`
   - `GET/PATCH/DELETE /api/nodes/{node_id}`
   - `GET /api/nodes/{node_id}/children`
   - `POST /api/nodes/{node_id}/move`
   - `GET /api/nodes/{node_id}/leaf`
4. 错误语义保持与领域层一致：用户可见错误映射到 `400/404/409`，其余归 `500`。
5. 本轮保持 `cmd/vos` 现有 CLI + JSON 语义不变，Web/API 为新增适配层，不替换既有调用链。
6. 回归与冒烟结果：
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过
   - 本地启动 `openmate-vos-web` 后，`GET /api/health` 返回 `{"status":"ok"}`，`GET /` 返回 `200`
7. 修复 `openmate-vos-web --help` 重复输出 usage 的问题（保留单次输出）。

## 2026-04-17 VOS 前后端分离改造（HTML+JS）

1. 后端 API 与前端静态页面已拆分为两个独立进程：
   - `cmd/openmate-vos-api`：仅提供 `/api/*` JSON 接口
   - `cmd/openmate-vos-web`：仅提供 `frontend/vos` 静态页面
2. `internal/vos/httpapi` 已改为 API-only：
   - 移除内嵌静态资源与 `/`、`/assets/*` 路由
   - 新增 API CORS 头与 `OPTIONS` 预检支持，便于分离前端跨域访问
3. 前端迁移到独立目录 `frontend/vos`，保持原生 `HTML + CSS + JS`，并新增：
   - `API Base URL` 可配置（本地保存到 `localStorage`）
   - 前端主动请求 `{apiBaseUrl}/api/*`
4. 已清理旧内嵌前端文件目录：`internal/vos/httpapi/web/*`，避免双份前端来源。
5. 测试与冒烟：
   - `go test ./...` 通过
   - `go run ./cmd/openmate-vos-api --help` 正常
   - `go run ./cmd/openmate-vos-web --help` 正常
   - API(`18080`) + Frontend(`18081`) 双进程启动后：
     - `GET /api/health` 返回 `{"status":"ok"}`
     - `GET /` 返回 `200`

## 2026-04-17 HTTP v1 适配收口（CLI 保持不变）

1. VOS HTTP 适配层收口到 `/api/v1/*`，不再保留旧 `/api/*` 路由面向前端联调。
2. `internal/vos/httpapi` 已按前端约定统一响应包裹：
   - 成功/失败均返回 `{code, message, data}`。
   - 现阶段未实现接口（`/chat*`、`/topic*`、`/planlist*`、`/stats*`、`/tree/generate`、`/nodes/{id}/decompose`）统一返回 `501` 结构化响应。
3. 前端状态值与领域状态映射已在适配层处理：
   - `pending/waiting/running/completed/failed` 与 `draft/ready/active/done/blocked` 双向转换。
4. `internal/vos/httpapi/server_test.go` 已改造为 v1 断言：
   - Topic/Node 生命周期走 `/api/v1/*`。
   - 校验 envelope 结构、`501` 未实现接口、`/api/v1/*` CORS 预检。
   - 校验根路径 API-only 行为与旧 `/api/topics` 不暴露。
5. `frontend` 开发代理已对齐 API 进程端口：
   - `frontend/vite.config.ts`、`frontend/vite.config.js` 的 `/api` 代理目标改为 `http://127.0.0.1:8080`。
6. 回归结果：
   - `go test ./internal/vos/httpapi/...` 通过。
   - `go test ./...` 通过。
   - `go run ./cmd/openmate-vos-api --help` 正常输出。
7. 本次仅调整 HTTP 前后端适配层，不变更 `cmd/vos` CLI 既有交互链路。

## 2026-04-17 Chat API 落地（SessionEvent + Schedule 入队）

1. `internal/vos/httpapi` 已新增真实对话接口：
   - `POST /api/v1/chat`
   - `POST /api/v1/chat/stream`（SSE）
2. 聊天请求主链路收口为：
   - 创建新 Session（每次用户消息一轮）
   - 追加 user `message` 事件落库
   - 通过 `openmate-schedule enqueue` 入队（携带 `session_id`）
   - 轮询 SessionEvent 增量回放并对 SSE 输出
3. SSE 事件与前端对齐：
   - `phase`
   - `tool_call`
   - `assistant_delta`
   - `assistant_done`
   - `summary`
   - `fatal`
4. SessionEvent 扩展补充：
   - 新增 `assistant_delta`（用于 assistant 文本增量回放）
   - 最终仍写 `message` 作为稳定完成事件
5. 回归结果：
   - `go test ./internal/vos/httpapi/...` 通过
   - `go test ./...` 通过

## 2026-04-17 Chat node_id 兼容补充（Home 首轮对话）

1. `POST /api/v1/chat` 与 `POST /api/v1/chat/stream` 支持 `node_id` 为空：
   - 若传入有效 `node_id`，继续复用目标节点。
   - 若未传 `node_id`，后端自动创建 Topic + RootNode，随后创建 Session 并入队。
2. 该兼容用于首页首轮对话场景，避免前端首次发消息时报 `node_id is required`。
3. 回归结果：
   - `go test ./internal/vos/httpapi/...` 通过

## 2026-04-17 Chat 同进程调度内聚（inproc）

1. `internal/vos/httpapi` 新增调度模式：
   - 默认 `inproc`：直接调用同进程 `schedule.Engine.Enqueue/Tick`
   - 兼容 `shell`：保留原 `openmate-schedule` 命令调用路径
2. `vos-api` 构造阶段新增统一运行时装配，收敛：
   - `vos service + session store`
   - `schedule runtime store + engine`
   - `pool gateway`（统一 Go 运行时装配层）
3. `/api/v1/chat*` 保持对外协议不变，内部不再强依赖外部调度进程。
4. 回归结果：
   - `go test ./internal/vos/httpapi/...` 通过
   - `go test ./...` 通过

## 2026-04-17 VOS API 日志链路接入（slog）

1. `internal/vos/httpapi` 已接入请求级结构化日志，并贯穿 chat 主链路：
   - `prepare -> enqueue -> wait`
   - 同步与 SSE 流式路径均接入
2. `cmd/openmate-vos-api` 新增统一日志参数：
   - `--log-level`（`debug|info|warn|error`）
   - `--log-format`（`json|text`）
3. `request_id` 已支持从请求头透传（`X-Request-ID/X-Trace-ID/X-Correlation-ID`），未传时自动生成。
4. `chat.wait` 补齐空状态保护，避免日志字段读取导致空指针；同步聊天完成日志补齐真实 `duration_ms`。
5. 回归结果：
   - `go test ./internal/vos/httpapi/...` 通过
   - `go test ./...` 通过

## 2026-04-17 VOS API 启动阶段 Pool 初始化

1. `internal/openmate/runtime.Open` 新增启动初始化步骤：
   - 在运行时装配完成后立即调用 `poolGateway.Sync(context.Background())`
   - 启动时自动按 `model.json` 初始化/刷新 `pool` 配置到 SQLite
2. 行为变化：
   - `model.json` 缺失或非法时，`openmate-vos-api` 启动直接失败（fail-fast）
   - 避免首个 chat 请求才暴露 pool 配置问题
3. 测试更新：
   - `internal/openmate/runtime/runtime_test.go` 新增缺失 `model.json` 失败用例
   - `internal/vos/httpapi/server_test.go` 测试构造阶段补齐测试用 `model.json`
4. 回归结果：
   - `go test ./internal/openmate/runtime/...` 通过
   - `go test ./internal/vos/httpapi/...` 通过
   - `go test ./...` 通过

## 2026-04-21 默认 Topic 收敛（default-topic）

1. 在 VOS 服务层新增默认 Topic 收敛能力：
   - 固定默认 Topic ID：`default-topic`
   - 新增 `Service.EnsureDefaultTopic()`，确保默认 Topic 与 root node 幂等存在。
2. `Service.CreateNode()` 行为增强：
   - `topic_id` 为空时不再直接报错。
   - 若给了 `parent_id`，自动从父节点推导 `topic_id`。
   - 若未给 `parent_id`，自动落到 `default-topic`。
3. HTTP Chat 路径收敛：
   - `v1 chat` 请求新增可选 `topic_id`。
   - 无 `node_id` 时不再自动创建新 Topic；改为在目标 Topic（显式传入或 `default-topic`）下创建会话 Node。
4. HTTP Node 创建收敛：
   - `POST /api/v1/nodes` 在 `topic_id` 与 `parent_id` 都缺省时，默认落到 `default-topic`，不再隐式创建新 Topic。
5. CLI 对齐：
   - `vos node create` 的 `--topic-id` 从必选语义调整为可选说明，默认落 `default-topic`。
6. 新增/更新测试：
   - `internal/vos/service/service_test.go`
   - `internal/vos/httpapi/server_test.go`
   - `internal/vos/cli/cli_test.go`
7. 回归结果：
   - `go test ./internal/vos/service ./internal/vos/httpapi ./internal/vos/cli` 通过
   - `go test ./...` 通过
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）

## 2026-04-21 /tree/roots 展示语义收敛

1. `/api/v1/tree/roots` 已从“按 Topic 遍历 root_node_id”调整为“展示根节点列表”：
   - 普通 Topic：返回结构 root（`parent_id=nil` 且 `topic_id!=default-topic`）
   - `default-topic`：返回其结构 root 的一级子节点（用于首页历史会话展示）
2. 新增服务层方法 `Service.ListDisplayRootNodes()`，一次 `state store load` 完成筛选与排序，避免原 `ListTopics + GetNode*N` 的重复读取开销。
3. `default-topic` 的结构 root 不再出现在 `/tree/roots` 返回中，避免首页仅出现单一 default 条目。
4. 新增测试覆盖：
   - `internal/vos/service/service_test.go::TestListDisplayRootNodesIncludesDefaultEntriesAndTopicRoots`
   - `internal/vos/httpapi/server_test.go::TestServerV1TreeRootsReturnsDisplayRoots`
5. 回归结果：
   - `go test ./internal/vos/service ./internal/vos/httpapi` 通过
   - `go test ./...` 通过

## 2026-04-22 Node Decompose CLI 落地（CLI-only）

1. 新增 `vos node decompose` 命令（保持 CLI-only，对外 HTTP `/api/v1/nodes/{id}/decompose` 继续 `501` 不变）。
2. 新命令参数：
   - 必填：`--node-id`
   - 可选：`--hint`、`--max-items`、`--agent-command`
3. 运行链路收口：
   - 读取目标节点与 `context snapshot`
   - 调用 `openmate_agent.cli decompose run`（JSON in/out）
   - 将返回任务作为目标节点的直接子节点落盘（仅一层挂载）
   - 新建子节点默认状态统一为 `ready`
4. 输出结构扩展为：`DecomposeResponse + created_nodes[]`，用于 CLI 调用方直接拿到落盘结果。
5. 新增/更新测试：
   - `internal/vos/cli/cli_test.go`
   - 覆盖 `--help`、成功创建子节点、目标节点不存在、Agent 失败、空任务返回。
6. 回归结果：
   - `go test ./internal/vos/cli ./internal/schedule` 通过
   - `go test ./...` 通过

## 2026-04-22 HTTP 挂载 Node Decompose + 前端契约切换（以后端为准）

1. 新增 `/api/v1/nodes/{id}/decompose` 实现，不再返回 `501`：
   - 路由：`POST /api/v1/nodes/{id}/decompose`
   - 请求体：`hint?`、`max_items?`（默认与 CLI 一致为 `5`）
   - 响应保持 v1 envelope，`data` 返回 `request_id/topic_id/node_id/status/output/error/duration_ms/tasks/created_nodes`
2. 抽取并复用 decompose 核心到服务层：
   - 新增 `Service.DecomposeNode(...)`
   - 新增可注入执行器 `NodeDecomposeRunner`
   - CLI 与 HTTP 共用同一落盘逻辑（读取 node + context snapshot，调用 agent，创建直接子节点，默认 `ready`）
3. CLI `vos node decompose` 保持用户语义不变：
   - 仍支持 `--node-id/--hint/--max-items/--agent-command`
   - 改为调用服务层复用实现
4. HTTP 回归测试补齐：
   - 成功链路：返回新契约并实际创建子节点
   - 节点不存在：`404`
   - agent 失败与空任务：用户可见错误（`400`）
5. 仍未实现接口保持 `501`，测试改为断言 `/api/v1/tree/generate`。
6. 前端契约切换完成：
   - `NodeDecomposeRequest`：`hint?`、`max_items?`
   - `NodeDecomposeResponse`：改为后端当前结构（`tasks + created_nodes`）
   - Home/Workspace 成功提示改为 `created_nodes.length`
7. 验证结果：
   - `go test ./internal/vos/httpapi ./internal/vos/cli ./internal/vos/service` 通过
   - `go test ./...` 通过
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（69 项）
   - `frontend npm run build` 通过
