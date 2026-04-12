# Go 仓库约定

## 1. 当前协作状态

截至 2026-04-11，以下模块已经从 Python 转向 Go：

1. `vos`（虚拟文件系统）
2. `pool`（Agent 池）

说明：

1. 当前 `vos` 的 Go 实现已经落在本分支。
2. `pool` 的 Go 实现由其对应分支维护，可能尚未合并到当前分支代码快照。
3. 因此，本文件记录的是**跨分支协作约定**，不是只描述当前工作区快照。

## 2. 总体原则

为降低后续分支合并成本，仓库中的 Go 代码统一遵循以下结构：

1. 仓库根目录只保留一份 `go.mod`
2. 仓库根目录只保留一份 `go.sum`
3. 各模块通过 `cmd/<module>` 暴露可执行入口
4. 各模块通过 `internal/<module>` 放置内部实现
5. 不允许每个模块各自维护独立根级 Go 模块

## 3. 目录约定

推荐结构如下：

```text
go.mod
go.sum
cmd/
  vos/
  pool/
  schedule/
  agent/
internal/
  vos/
  pool/
  schedule/
  agent/
```

当前含义：

1. `cmd/vos`：虚拟文件系统 CLI 入口
2. `cmd/pool`：Agent 池 CLI 入口
3. `internal/vos`：VFS 内部实现
4. `internal/pool`：Pool 内部实现

后续模块如转 Go，也沿用同样结构。

## 4. 合并规则

后续各分支合并时，统一按以下规则处理：

1. 如果分支中新增 Go 代码，不新建第二个根级 `go.mod`
2. 如果分支中新增依赖，直接合并到仓库根 `go.mod`
3. 如果出现 `go.sum`，也只保留仓库根这一份
4. `vos` 相关代码只放在 `cmd/vos`、`internal/vos`
5. `pool` 相关代码只放在 `cmd/pool`、`internal/pool`
6. 不把 `vos` 代码写进 `pool` 目录，也不把 `pool` 代码写进 `vos` 目录

## 5. 依赖维护规则

1. 当前 Go 依赖明细见 [Go依赖.md](D:/XuKai/Project/vos/architecture/sharedInfo/Go依赖.md)
2. 多 worktree 的实际合并操作见 [Worktree合并流程.md](D:/XuKai/Project/vos/architecture/sharedInfo/Worktree合并流程.md)
3. 新增第三方依赖时，必须同步更新：
   - `go.mod`
   - `go.sum`
   - [Go依赖.md](D:/XuKai/Project/vos/architecture/sharedInfo/Go依赖.md)
4. 如果只是新增标准库使用，不需要更新 `go.mod`，但若对全局协作有影响，建议更新共享文档

## 6. 当前建议

1. `vos` 和 `pool` 后续都按单仓库单模块方式继续推进
2. 后续 `schedule`、`agent` 若转 Go，也不要再新建独立 Go 模块
3. 若未来确实要拆多模块仓库，应作为单独架构决策处理，而不是在各分支内各自演化
