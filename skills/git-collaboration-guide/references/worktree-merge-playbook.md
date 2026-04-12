# Worktree 合并操作手册

## 适用场景

当仓库使用多个 worktree 并行开发时，使用本手册指导实际合并步骤。

当前典型映射：

1. `D:\XuKai\Project\vos` 对应 `vos`
2. `D:\XuKai\Project\pool` 对应 `pool`
3. `D:\XuKai\Project\schedule` 对应 `schedule`
4. `D:\XuKai\Project\agent` 对应 `agent`
5. `D:\XuKai\Project\openmate` 对应 `master`

## 合并前检查

1. 进入各模块 worktree，执行 `git status`
2. 如果还有未提交改动，先提交到对应模块分支
3. 不要在未提交状态下直接进入总集成 worktree 做 merge

## 推荐合并步骤

1. 在模块 worktree 内提交：
   - `vos` 在 `D:\XuKai\Project\vos`
   - `pool` 在 `D:\XuKai\Project\pool`
2. 切到总集成 worktree：

```powershell
cd D:\XuKai\Project\openmate
git branch --show-current
```

3. 确认当前为 `master` 后，依次合并：

```powershell
git merge --no-ff vos
git merge --no-ff pool
```

4. 解决冲突
5. 再执行：

```powershell
git status
```

6. 确认 merge 完成后再做测试

## 高冲突区域

1. `.gitignore`
2. `go.mod`
3. `go.sum`
4. `cmd/`
5. `internal/`
6. `architecture/sharedInfo/*`

## 回退方式

如果本次 merge 不继续：

```powershell
git merge --abort
```
