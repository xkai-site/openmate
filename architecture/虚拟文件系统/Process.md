# 虚拟文件系统 Process

## 2026-04-11 初始化与 Go 迁移

1. 完成 VFS 从 Python 到 Go 的主迁移：
   - 入口：`cmd/vos`
   - 核心：`internal/vos/domain|service|store|cli`
2. 完成 Topic/Node 基础能力与 CLI：
   - `topic create/get/list`
   - `node create/get/list/children/move/delete/update/leaf`
3. 保留 `.vos_state.json` 作为 Node/Topic 状态存储。
4. 移除 Python 版 `openmate_vos` 源码实现，避免双栈维护。

## 2026-04-13 master 对齐与能力补齐

1. 补齐 Topic `update/delete` 能力，统一 CRUD 基线。
2. `node list` 增加调度向过滤能力：
   - `--leaf-only`
   - `--status`
   - `--exclude-status`
3. `node update` 增加 `--expected-version` 乐观并发校验。
4. 补齐 `memory` 聚合：
   - 子节点记忆写入父节点 `memory._child_memory_cache`
   - 纯结构变化不触发重算
5. 测试通过：
   - `go test ./internal/vos/...`
   - `go test ./...`

## 2026-04-13 Session SQLite 落地（历史阶段）

1. Session 从 `.vos_state.json` 拆出，落独立 SQLite（`.vos_sessions.db`）。
2. 历史第一版为 `kind` 事件模型（该模型已废弃，不再作为当前规范）。
3. `Node.session` 仅保存 `session_id` 引用。

## 2026-04-14 Session 模型收敛（当前生效）

1. Session 状态收敛为：
   - `active | waiting | completed | failed`
2. SessionEvent 收敛为：
   - `item_type`
   - `provider_item_id`
   - `role`
   - `call_id`
   - `payload_json`
3. 兼容策略收敛：
   - 不再支持旧 `kind`
   - 不再支持旧状态值 `open/closed`
   - 旧 schema 需手工迁移或重建
4. 详情以文档为准：
   - [Session与SessionEvent字段收敛.md](D:/XuKai/Project/vos/architecture/虚拟文件系统/Session与SessionEvent字段收敛.md)

## 2026-04-14 工具调用契约对齐

1. 已接入共享契约文档，明确 Agent 与 VOS 的 SessionEvent 对齐方式。
2. VOS 目录内不重复维护跨模块契约细节，统一引用：
   - `architecture/sharedInfo/工具调用-SessionEvent契约.md`

## 后续事项（VOS 侧）

1. 按 `node_id` 提供 Session 摘要查询入口（CLI/API 形态待定）。
2. 评估 `payload_json` 超大体积时的外部 artifact 引用方案。
3. 评估 Session 热路径摘要字段，降低纯回放查询成本。
=======
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