---
name: git-collaboration-guide
description: 用于当前仓库 Git 协作、worktree 并行开发、分支集成与合并冲突处理的技能。适用于需要检查 `git worktree` 布局、规划或执行 `vos/pool/schedule/agent` 等模块分支合并、统一单个根级 `go.mod/go.sum`、处理 `cmd/`、`internal/`、`.gitignore`、`architecture/sharedInfo/*` 冲突，或在合并后补共享文档时。
---

# Git协作指南

## 概览

使用这个技能处理当前仓库的 Git 协作与多 worktree 分支集成。默认采用“先各模块分支提交，再在总集成 worktree 合并”的策略，而不是直接把一个 worktree 的目录内容覆盖到另一个 worktree。

## 先检查再行动

1. 先运行：
   - `git worktree list --porcelain`
   - `git branch -vv`
   - `git status --short`
2. 优先读取仓库内现有约定：
   - `architecture/sharedInfo/Go仓库约定.md`
   - `architecture/sharedInfo/Worktree合并流程.md`
   - `architecture/sharedInfo/Go依赖.md`
3. 确认各模块 worktree 是否还有未提交改动；如果有，优先要求先提交或明确暂存策略，再进入 merge。

## 标准流程

1. 在各自模块 worktree 内完成开发并提交到对应分支。
2. 切到总集成 worktree，通常是 `master` 分支对应目录。
3. 按顺序执行 `git merge --no-ff <branch>`。
4. 按仓库规则解决冲突。
5. 跑合并后的验证命令。
6. 如果合并改变了协作约定、Go 结构或共享依赖，补 `sharedInfo` 文档。

## 本仓库专用规则

1. 合并对象是分支，不是 worktree 目录。
2. 仓库根目录只保留一份 `go.mod` 和一份 `go.sum`。
3. Go 代码按模块边界放置：
   - `cmd/<module>` 放 CLI 入口
   - `internal/<module>` 放内部实现
4. `.gitignore` 采用并集合并，不直接覆盖对方忽略项。
5. `architecture/sharedInfo/*` 需要保留双方都重要的内容，尤其是架构和依赖约定。
6. 如果双方都改了 Go 结构，默认收敛到单仓库、单 `go.mod`、多 `cmd/*`、多 `internal/*` 的布局，除非用户明确要求更大范围的架构调整。

## 合并后验证

1. 运行 `go test ./...`
2. 对已存在的命令入口运行：
   - `go run ./cmd/vos --help`
   - `go run ./cmd/pool --help`
3. 如果默认 Go cache 目录不可写，先设置仓库内缓存目录再执行测试。

## 参考资料

1. 需要具体 worktree 合并步骤时，读取 [references/worktree-merge-playbook.md](references/worktree-merge-playbook.md)
2. 需要处理单 `go.mod`、`cmd/*`、`internal/*` 结构冲突时，读取 [references/go-structure-rules.md](references/go-structure-rules.md)
