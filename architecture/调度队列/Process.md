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
>>>>>>> ebcb66ca322d2db64ce94bf7a712961cd2a3e997
