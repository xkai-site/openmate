# Agent池内部开发过程（收敛版）

## 2026-04-12 usage 聚合视图落地

1. 在保留 `records` 原始 invocation/attempt 输出的前提下，新增只读 `usage` 聚合视图命令。
2. `usage` 当前按 invocation 级结果聚合，并补充 `attempt_count / retry_count`，用于快速看调用消耗与执行开销。
3. 支持沿用 `records` 的 `node_id / limit` 过滤口径，避免 summary 与原始记录查询窗口不一致。
4. Python 侧 `PoolGateway` 已同步暴露 `usage()`，保持 CLI + JSON 契约不变。
5. 当前测试结果：`go test ./...` 与 `python -m unittest discover -s tests -v` 均通过；Python 侧共 30 项通过。

## 2026-04-11 重试与错误语义收敛

1. `invoke` 执行流已从“单次 reserve 后直接终结 invocation”调整为“attempt 先收尾，再按策略决定是否继续重试，最后统一产出 invocation 最终结果”。
2. 重试策略已开放到 `model.json.retry`：当前支持 `max_attempts / base_backoff_ms`，缺省时仍沿用网关默认值。
3. provider 错误分类已细化为 `provider_unreachable / provider_timeout / provider_rate_limited / provider_http_error / provider_invalid_json / gateway_internal_error` 这一组内部语义。
4. API 摘除计数已收紧为仅统计健康恶化型失败；`429`、返回 JSON 非法、网关内部错误不会直接把 API 打成 `offline`。
5. 已补 Go 单测、Python 契约测试与可选真实 provider 集成测试骨架；当前 `go test ./...` 与 `python -m unittest discover -s tests -v` 均通过。

## 2026-04-11 invoke 网关落地

1. 已取消对外 `get / done / tickets / usage` 旧语义，CLI 主命令先收敛为 `invoke / cap / records / sync`，后续再按新定义补充聚合视图命令。
2. `openmate_pool` 已完成请求/响应模型重构：从租约模型切到 `InvokeRequest / InvokeResponse / InvocationRecord / InvocationAttempt`。
3. 运行态存储已从 `tickets + usage_records` 转为 `invocations + invocation_attempts`，并保留 `apis` 作为底层并发与健康治理状态机。
4. 新增 provider adapter 分层，当前先落地 `openai_compatible` 调用实现。
5. `service.execute()` 已改为通过网关发起模型调用，不再向上暴露 endpoint 或持有 provider 细节。
6. 当前测试结果：`python -m unittest discover -s tests -v` 共 30 项通过。

## 2026-04-11 Go 迁移完成

1. Agent池运行时已迁移为 Go 实现，代码入口为 `cmd/openmate-pool`，核心逻辑位于 `internal/poolgateway`。
2. Python 侧 `openmate_pool` 已降为契约模型 + Go CLI adapter，不再承担 SQLite/provider/runtime 逻辑。
3. `openmate_agent.service` 默认通过 Python adapter 调用 Go 二进制，边界固定为 `CLI + JSON`。
4. Go 侧已通过 `go test ./...`；Python 侧已通过 `python -m unittest discover -s tests -v`。
5. 为避免权限问题，Go 构建缓存与模块缓存固定落在仓库内 `.openmate/`，并已加入 `.gitignore`。

## 2026-04-11 网关方向更新

1. 已明确将 Agent池的目标定位上收为“LLM 调用网关 + 可观测体系”，不再把它仅视为 API 租约池。
2. 方向性调整为：完整请求与完整响应都应经过 Agent池；上层 service 只消费标准化结果中的必要部分，再做业务解析。
3. 当前 `get/done` 仍是现网兼容基线，但后续主写路径应迁移到 `invoke` 一类统一调用契约。
4. 这意味着 provider 适配、凭证托管、重试/限流/熔断、usage/latency/error 采集都需要进一步收口到 Agent池内部。
5. 该变化已同步到 `sharedInfo/架构.md`，作为跨模块边界更新。

## 2026-04-11 接手初始化

1. 当前工作分支确认为 `pool`，职责边界继续按 Agent 池模块推进。
2. 仓库根目录原先缺少本地 `.venv`，已按项目约定补建，并完成 `pip install -r requirements.txt`。
3. 初始化验证发现 `PoolStateStore._init_db()` 在 Windows 下未及时关闭 SQLite 连接，导致 CLI 测试清理临时目录时 `.db` 文件句柄残留。
4. 已修复为显式关闭初始化连接。
5. 当前可直接继续在 `openmate_pool` 上开发。

## 后续待办（仅保留有效项）

1. 将默认重试策略参数化到配置层，但在开放前先保持当前代码内默认值稳定。
2. 视需要继续扩 `usage` 维度，例如按 api/provider/model 的分桶聚合，但保持 `records` 作为原始事实来源。
