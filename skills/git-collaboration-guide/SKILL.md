---
name: git-collaboration-guide
description: 用于当前仓库 Git 协作、worktree 并行开发、分支集成与合并冲突处理的技能。
---

# Git协作指南

## 概览

使用这个技能处理当前仓库的 Git 协作与多 worktree 分支集成。默认采用“先各模块分支提交，再在总集成 worktree 合并”的策略，而不是直接把一个 worktree 的目录内容覆盖到另一个 worktree。

## 合并指南

原则是：先检查再行动

1. 先在主分支运行：
   - `git worktree list --porcelain`
   - `git branch -vv`
   - `git status --short`
2. 确认各模块 worktree 是否还有未提交改动；如果有，优先要求先提交或明确暂存策略，再进入 merge。

## 标准流程

1. 在各自模块 worktree 内完成开发并提交到对应分支。
2. 切到总集成 worktree，通常是 `master` 分支对应目录。
3. 按顺序执行 `git merge --no-ff <branch>`。
4. 按仓库规则解决冲突。
5. 跑合并后的验证命令。
6. 如果合并改变了协作约定、技术栈结构或共享依赖，需要显式告诉用户。
