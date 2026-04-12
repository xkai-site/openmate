# Go 结构收敛规则

## 适用场景

当合并涉及 Go 代码布局、依赖文件或模块边界时，优先读取本文件。

## 目标结构

仓库采用单仓库、单模块结构：

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

## 处理规则

1. 只保留一份根级 `go.mod`
2. 只保留一份根级 `go.sum`
3. `vos` 代码只放 `cmd/vos`、`internal/vos`
4. `pool` 代码只放 `cmd/pool`、`internal/pool`
5. 如果双方都新增依赖，把依赖合并进同一个根级模块文件
6. 如果合并修改了全局 Go 布局，补 `architecture/sharedInfo` 文档
