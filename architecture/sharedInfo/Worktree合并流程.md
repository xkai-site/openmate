# Worktree 合并流程

## 1. 适用场景

本文件用于当前仓库在多 worktree 并行开发时的标准合并流程。

当前典型场景：

1. `vos` 在 `D:\XuKai\Project\vos`
2. `pool` 在 `D:\XuKai\Project\pool`
3. 总集成 worktree 在 `D:\XuKai\Project\openmate`

核心原则：

1. worktree 不是合并对象，**分支**才是合并对象。
2. 不直接把一个 worktree 合并进另一个 worktree。
3. 统一在总集成 worktree 中做正式 merge。

## 2. 当前分支映射

按当前仓库约定：

1. `D:\XuKai\Project\vos` 对应 `vos` 分支
2. `D:\XuKai\Project\pool` 对应 `pool` 分支
3. `D:\XuKai\Project\schedule` 对应 `schedule` 分支
4. `D:\XuKai\Project\agent` 对应 `agent` 分支
5. `D:\XuKai\Project\openmate` 对应 `master` 分支

说明：

1. `master` 是总集成分支。
2. 各模块分支先各自完成、提交、验证，再合并到 `master`。

## 3. 合并前检查

正式合并前，先分别在模块 worktree 中检查是否存在未提交改动。

### 3.1 在 `vos` worktree

```powershell
cd D:\XuKai\Project\vos
git status
```

### 3.2 在 `pool` worktree

```powershell
cd D:\XuKai\Project\pool
git status
```

要求：

1. 如果仍有未提交改动，先提交到对应模块分支。
2. 不要在“未提交状态”下直接去 `master` 做 merge。

## 4. 标准合并步骤

### 4.1 先在各自模块分支提交

#### `vos`

```powershell
cd D:\XuKai\Project\vos
git add .
git commit -m "feat(vos): ..."
```

#### `pool`

```powershell
cd D:\XuKai\Project\pool
git add .
git commit -m "feat(pool): ..."
```

### 4.2 再到总集成 worktree 合并

```powershell
cd D:\XuKai\Project\openmate
git status
git branch --show-current
```

确认当前分支为 `master` 后，执行：

```powershell
git merge --no-ff vos
git merge --no-ff pool
```

建议顺序：

1. 先合并 `vos`
2. 再合并 `pool`

如果只想先验证一个模块，也可以先只 merge 一个分支。

## 5. 高冲突区域

当前仓库在 Go 化过程中，以下位置最容易冲突：

1. `.gitignore`
2. `go.mod`
3. `go.sum`
4. `cmd/`
5. `internal/`
6. `architecture/sharedInfo/*`

## 6. 冲突处理规则

### 6.1 `go.mod`

只保留一份仓库根级 `go.mod`。

规则：

1. 不允许 `vos`、`pool` 各自保留独立根级 `go.mod`
2. 如果双方都新增依赖，合并到同一个根级 `go.mod`
3. 后续统一使用单仓库单模块方式维护

### 6.2 `go.sum`

规则：

1. 只保留仓库根这一份
2. 如果双方都引入第三方依赖，合并为同一个 `go.sum`

### 6.3 `cmd/`

最终目标结构：

```text
cmd/
  vos/
  pool/
```

规则：

1. `vos` 入口放 `cmd/vos`
2. `pool` 入口放 `cmd/pool`
3. 不互相覆盖

### 6.4 `internal/`

最终目标结构：

```text
internal/
  vos/
  pool/
```

规则：

1. `vos` 代码只放 `internal/vos`
2. `pool` 代码只放 `internal/pool`
3. 不把一个模块的实现混进另一个模块目录

### 6.5 `.gitignore`

规则：

1. 合并双方新增的本地缓存/构建产物忽略项
2. 保留 `.openmate/` 等本地运行缓存目录
3. 不删对方已验证有用的忽略规则

### 6.6 `architecture/sharedInfo/*`

规则：

1. 不直接覆盖对方内容
2. 保留双方都重要的迁移信息
3. 至少保留这些结论：
   - `vos` 已从 Python 转 Go
   - `pool` 已从 Python 转 Go
   - 仓库采用单 `go.mod`
   - 仓库采用 `cmd/*` + `internal/*`

## 7. 合并完成后的验证

在总集成 worktree 中执行：

```powershell
cd D:\XuKai\Project\openmate
go test ./...
go run ./cmd/vos --help
go run ./cmd/pool --help
```

如果默认 Go cache 目录无写权限，可使用仓库内缓存：

```powershell
$env:GOCACHE='D:\XuKai\Project\openmate\.openmate\go-build'
$env:GOMODCACHE='D:\XuKai\Project\openmate\.openmate\go-mod'
go test ./...
```

## 8. 冲突处理与回退

查看冲突状态：

```powershell
git status
```

冲突解决后：

```powershell
git add <文件>
git commit
```

如果本次 merge 不继续：

```powershell
git merge --abort
```

## 9. 当前建议

1. `vos`、`pool` 分支都先保持“已提交、可复用、可测试”的状态
2. 只在 `master` 对应的 `openmate` worktree 做正式集成
3. 合并时严格按单仓库单 `go.mod` 结构收敛
