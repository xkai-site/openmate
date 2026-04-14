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

## 2026-04-14 双队列 + SessionEvent 原子调度落地（MVP）

1. 已落地调度运行时持久化：`SQLite`（默认 `.openmate/runtime/schedule.db`），包含：
   - `topic_runtime`
   - `node_queue`
   - `dispatch_history`
2. 已落地调度引擎 `internal/schedule/engine.go`，实现：
   - 一级 Topic 选择（按队列层级 + 同层轮转顺序）
   - aging 提升（按阈值提级）
   - 二级 `planTopicDispatch` 复用
   - `priority_dirty` 时优先 `priority_node`
3. `priority_node` 语义已按当前共识实现：
   - 作为同 Topic 的真实 Node（通过 `vos node create` 确保存在）
   - 也必须走 `schedule` 派发，不走旁路
   - 同 Topic 触发重排时，若存在运行中 Node，则等待当前原子调用结束再执行 `priority_node`
4. 已补齐 `schedule -> vos` shell-out：
   - `EnsurePriorityNode`
   - `EnsureSession`
   - `AppendDispatchAuthorizedEvent`
   - `AppendDispatchResultEvent`
5. 已补齐 `schedule -> agent` worker 契约与 shell-out：
   - 请求/响应 JSON 模型已落地（`WorkerExecuteRequest/Response`）
   - `openmate-schedule` 通过 worker CLI 执行一次原子调用
6. 调度原子单位已收敛为 `SessionEvent`：
   - 每次派发前先在 `vos` 侧追加授权事件并拿到 `event_id`
   - worker 执行结束后追加结果事件
   - 同一 Session 可包含多次原子调用，调度按调用粒度推进
7. `openmate-schedule` CLI 已从单一 `plan` 扩展为：
   - `plan`
   - `enqueue`
   - `tick`
   - `run`
   - `state`
8. `openmate_agent` 已新增 worker CLI：
   - `python -m openmate_agent.cli worker run --request-file ...`
   - 支持普通 Node 与 `priority` Node 两种执行形态
9. 测试与回归：
   - Go：`go test ./...` 通过
   - Python：`python -m unittest discover -s tests -p "test_*.py" -v` 通过（42 项）

### 当前已知缺口（后续阶段）

1. `priority_node` 当前为启发式编排，尚未接入真实 LLM PriorityAgent 评分提示词与策略。
2. Topic 热度评分尚未完整接入交互事件流，当前一级仍偏工程侧轮转/层级机制。
3. worker 超时/取消/重试策略已留契约位，但取消 token 与强一致回收仍需增强。
