# Go 仓库约定

## 1. 当前协作状态

截至 2026-04-12，当前技术栈决策如下：

1. `vos` 使用 Go
2. `pool` 使用 Go 运行时
3. `schedule` 确认使用 Go
4. `agent` 暂不迁移 Go，继续保持 Python + Pydantic

## 2. 总体原则

为降低后续分支合并与跨模块调用成本，仓库中的 Go 代码统一遵循以下规则：

1. 仓库根目录只保留一份 `go.mod`
2. 仓库根目录只保留一份 `go.sum`
3. Go 模块之间不再各自维护独立根级 `go.mod`
4. 同语言同进程场景优先内部调用；CLI + JSON 作为兼容入口保留
5. `internal/*` 目录只供模块自身实现与本模块测试使用
6. CLI 既保留给人工操作与测试，也承担模块间调用边界

## 3. 当前根模块约束

1. 当前根模块名是 `vos`
2. 在没有单独架构决策前，不主动改动根模块名
3. 新合入的 Go 代码必须适配当前根模块，不再引入第二套模块名

## 4. 目录约定

当前与规划中的目录如下：

```text
go.mod
go.sum
cmd/
  vos/
  openmate-pool/
  openmate-schedule/
internal/
  vos/
  poolgateway/
  schedule/
openmate_pool/              # Python adapter
openmate_agent/             # Python capability layer
```

说明：

1. `cmd/vos`：VFS CLI 入口
2. `cmd/openmate-pool`：Pool CLI 入口
3. `cmd/openmate-schedule`：Schedule CLI 入口
4. `internal/vos`：VFS 内部实现
5. `internal/poolgateway`：Pool 内部实现
6. `internal/schedule`：Schedule 自身内部实现

## 5. 跨模块调用规则

### 5.1 模块间调用

1. `schedule -> vos`：通过 `vos` CLI + JSON
2. `schedule -> vos` 在同进程场景下默认走 `internal/vos/service` 直调
3. `schedule -> pool`：统一由 Go 运行时装配层管理（调用链可按场景选择内部/CLI 兼容路径）
4. `schedule -> agent`：通过独立 worker CLI + JSON 契约
5. `agent -> pool`：通过 `openmate_pool` adapter，对外仍体现为 CLI + JSON 边界
6. 不把 `internal/vos`、`internal/poolgateway`、`internal/schedule` 当成仓库外共享接口

## 6. 变更规则

1. `schedule` 新增 Go 代码时，放在 `cmd/openmate-schedule`、`internal/schedule`
2. `pool` 新增 Go 代码时，放在 `cmd/openmate-pool`、`internal/poolgateway`
3. `vos` 新增 Go 代码时，继续放在 `cmd/vos`、`internal/vos`
4. Python adapter 与 Python capability 代码继续分别留在 `openmate_pool`、`openmate_agent`
5. 若新增第三方 Go 依赖，必须同步更新：
   - `go.mod`
   - `go.sum`
   - `architecture/sharedInfo/Go依赖.md`

## 7. 构建与测试规则

1. Go 统一验证命令：`go test ./...`
2. 若默认缓存目录受限，显式设置仓库内缓存：
   - `GOCACHE=.openmate/go-build-cache`
   - `GOMODCACHE=.openmate/go-mod-cache`
3. Python 验证继续在 `.venv` 中执行，不与 Go 工具链混用

## 8. 当前结论

1. 单根模块解决仓库管理与依赖统一，同时允许 Go 模块在仓库内直调协作
2. 跨语言边界仍冻结在 worker CLI + JSON 契约
3. 后续若要拆仓库或拆进程，CLI 兼容入口仍可直接复用
