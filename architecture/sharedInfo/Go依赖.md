# Go 依赖说明

## 1. 当前结论

1. 按当前协作状态，`vos` 与 `pool` 都已从 Python 转向 Go。
2. 当前 Go 模块 **没有第三方依赖**。
3. `go list -m all` 的结果只有：
   - `vos`
4. 也就是说，当前 Go 代码只依赖：
   - Go 工具链本身
   - Go 标准库

补充说明：

1. 上述 `go list -m all` 结果来自当前分支代码快照。
2. 当前分支已落地的是 `vos`；`pool` 的 Go 代码以其对应分支为准。

## 2. 模块信息

当前 `go.mod`：

```go
module vos

go 1.26
```

说明：

1. 模块名当前为 `vos`。
2. 当前未引入 `require` 项。
3. 如后续新增第三方库，应同步更新本文件。

## 3. 当前使用的标准库

### 3.1 CLI 层

主要使用：

1. `flag`
2. `encoding/json`
3. `fmt`
4. `io`
5. `strings`
6. `errors`

### 3.2 领域与服务层

主要使用：

1. `time`
2. `fmt`
3. `sort`
4. `slices`

### 3.3 ID 与存储层

主要使用：

1. `crypto/rand`
2. `encoding/hex`
3. `bytes`
4. `os`
5. `path/filepath`
6. `runtime`

### 3.4 测试层

主要使用：

1. `testing`
2. `bytes`
3. `strings`

## 4. 环境依赖

当前必需环境：

1. Go 1.26 或兼容版本
2. 可用的 `go` 命令
3. 可访问的模块代理或直连能力

建议验证：

```powershell
go version
go env GOPROXY
```

## 5. 当前构建与测试命令

常规执行：

```powershell
go test ./...
go run ./cmd/vos --help
```

如果默认 Go cache 目录无写权限，可使用仓库内缓存目录：

```powershell
$env:GOCACHE='D:\XuKai\Project\vos\.openmate\go-build'
$env:GOMODCACHE='D:\XuKai\Project\vos\.openmate\go-mod'
go test ./...
```

## 6. 后续维护规则

1. 如果新增第三方依赖，必须更新 `go.mod`。
2. 如果新增第三方依赖，必须同步更新本文件的“当前结论”和“模块信息”。
3. 如果 Go 版本要求变化，也要同步更新本文件。
