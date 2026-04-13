# Go 依赖说明

## 1. 当前结论

1. 当前已落地的 Go 模块为 `vos` 与 `pool`
2. `schedule` 已确认采用 Go 技术栈，并已落地 `cmd/openmate-schedule` / `internal/schedule` 基线代码
3. 当前根模块名为 `vos`
4. 当前仓库已经存在第三方 Go 依赖：`github.com/mattn/go-sqlite3`

## 2. 当前 `go.mod`

```go
module vos

go 1.26

require github.com/mattn/go-sqlite3 v1.14.32
```

说明：

1. `vos` 使用仓库根模块，不单独拆 Go module
2. `pool` 也复用同一根模块
3. 后续 `schedule` 若落地 Go，也继续复用当前根模块

## 3. 当前 Go 依赖分层

### 3.1 标准库

当前主要依赖 Go 标准库，包括但不限于：

1. `context`
2. `database/sql`
3. `encoding/json`
4. `errors`
5. `flag`
6. `fmt`
7. `net/http`
8. `os`
9. `path/filepath`
10. `sort`
11. `strings`
12. `time`

### 3.2 第三方依赖

当前第三方依赖只有：

1. `github.com/mattn/go-sqlite3`

用途：

1. 为 `pool` 的 SQLite 运行态提供驱动
2. 支撑并发治理、调用记录、usage 聚合等持久化逻辑

## 4. 环境依赖

当前必需环境：

1. Go 1.26 或兼容版本
2. 可用的 `go` 命令
3. 可用的 CGO / C 编译环境

建议验证：

```powershell
go version
go env CGO_ENABLED
gcc --version
```

## 5. 当前构建与测试命令

常规执行：

```powershell
go test ./...
go run ./cmd/vos --help
go run ./cmd/openmate-pool --help
```

若默认缓存目录无写权限，使用仓库内缓存：

```powershell
$env:GOCACHE = (Resolve-Path .\\.openmate).Path + '\\go-build-cache'
$env:GOMODCACHE = (Resolve-Path .\\.openmate).Path + '\\go-mod-cache'
New-Item -ItemType Directory -Force -Path $env:GOCACHE | Out-Null
New-Item -ItemType Directory -Force -Path $env:GOMODCACHE | Out-Null
go test ./...
```

## 6. 后续维护规则

1. 新增第三方依赖时，必须同步更新 `go.mod`
2. 新增第三方依赖时，必须同步更新 `go.sum`
3. 新增第三方依赖时，必须同步更新本文件
4. 若 Go 版本要求变化，也必须同步更新本文件
