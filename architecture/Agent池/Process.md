# Agent池内部开发过程（收敛版）

## 2026-04-09 当前有效决策

1. 用户只维护 `model.json`，不直接管理租约与注册。
2. CLI 主命令为：`get / done / cap / tickets / usage / sync`。
3. 核心写路径只有 `get` 与 `done`。
4. `done` 必须携带 `result=success|failure`，用于健康状态更新。
5. 运行态存储采用 SQLite，不再使用 JSON 状态文件。

## 2026-04-09 当前实现基线

1. 配置驱动：每次执行前按 `model.json` 同步 API 通道（upsert + 下线未配置项）。
2. 并发门控：支持单 API `max_concurrent` 与全局 `global_max_concurrent`。
3. 状态机：`available / leased / offline` + 失败阈值自动摘除。
4. 追踪链路：`node_id -> ticket_id -> api_id`。
5. usage 记录：token、延迟、成本、执行结果。

## 2026-04-09 并发与稳定性验证

1. `get/done` 在 SQLite `BEGIN IMMEDIATE` 事务中执行，防止并发超配。
2. 新增并发测试：多线程并发 `get`，成功数不超过配置并发上限。
3. 当前测试结果：`python -m unittest discover -s tests -v` 共 14 项通过。

## 后续待办（仅保留有效项）

1. 统一错误码与错误语义（便于调度层稳定消费）。
2. 增加 usage 聚合视图（不改变原始记录层）。
3. 与调度模块冻结调用契约（字段与重试语义）。
