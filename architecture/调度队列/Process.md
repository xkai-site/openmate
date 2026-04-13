# Process 记录

## 2026-04-12 技术栈决策补录

1. 已重新读取 `architecture/sharedInfo` 与 `调度队列.md`，收口当前跨模块契约。
2. 已确认调度层采用 Go 技术栈推进，不再沿用 Python 方向继续扩展。
3. 当前冻结的边界是：
   - `schedule -> vos`：CLI + JSON
   - `schedule -> pool`：CLI + JSON
   - `schedule -> agent`：未来的 worker CLI + JSON 契约，当前尚未冻结
4. 已同步更新共享文档：
   - `architecture/sharedInfo/架构.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Go仓库约定.md`
   - `architecture/sharedInfo/Go依赖.md`
   - `architecture/sharedInfo/依赖.md`
5. 已落地 `cmd/openmate-schedule` 与 `internal/schedule` 目录骨架，并新增最小 CLI：
   - `openmate-schedule help`
   - `openmate-schedule plan`
6. 已执行 `go test ./...`，当前通过；`internal/schedule` 基线测试已补齐。
7. 下一步进入调度层开发前，应先冻结 worker 请求/响应契约，再开始 `TopicRuntimeState` 与 dispatch 主循环实现。
 
## 2026-04-13 Python 原型迁移到 Go

1. 已将 Python `openmate_schedule` 原型的最小控制面模型迁移到 Go：
   - `TopicSnapshot`
   - `TopicRuntimeState`
   - `TopicNode`
   - `DispatchPlan`
2. 已在 `internal/schedule` 落地等价 planner：
   - 终态过滤
   - active layer 选择
   - continuation-first
   - `last_worked_node_id` 回退
   - `stalled` 判定
   - `available_slots` 派发限制
3. 已将 `openmate-schedule plan` 从 scaffold 升级为真实 CLI：
   - 输入：`--input-file` + `TopicSnapshot JSON`
   - 输出：`DispatchPlan JSON`
4. 已删除 Python `openmate_schedule` 原型与对应测试，后续调度模块仅继续维护 Go 实现。
5. 已同步修正仓库文档中的技术栈与边界冲突：
   - `AGENTS.md`
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/sharedInfo/Go依赖.md`
6. 当前仍未进入完整调度 MVP，以下内容继续留在后续阶段：
   - 一级 Topic MLFQ
   - PriorityAgent 接入
   - `schedule -> agent` worker 契约冻结
   - 与 `vos/pool` 的真实 shell-out 联调
