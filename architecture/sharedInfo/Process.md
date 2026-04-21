# SharedInfo Process

## 2026-04-21 master 初始化基线

1. 已确认当前工作分支为 `master`，并完成仓库结构与当前状态检查。
2. 已读取共享契约与各模块内部过程文档，包含：
   - `architecture/sharedInfo/模块契约.md`
   - `architecture/虚拟文件系统/Process.md`
   - `architecture/调度队列/Process.md`
   - `architecture/Agent池/Process.md`
   - `architecture/Agent能力/Process.md`
   - `architecture/frontend/Process.md`
3. 已完成 Go 基线单元测试：`go test ./...`，结果通过。
4. 已完成 Python 基线单元测试：`.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v`，结果通过（63 项）。
5. 已完成 CLI 帮助冒烟校验，以下入口均可正常输出帮助：
   - `go run ./cmd/vos --help`
   - `go run ./cmd/openmate-schedule --help`
   - `go run ./cmd/openmate-pool --help`
   - `.\\.venv\\Scripts\\python.exe -m openmate_agent.cli --help`
6. 本次为初始化沉淀，不涉及业务逻辑修改。

## 2026-04-21 结构收敛（无功能改动）

1. 新增 Go 共享路径模块：`internal/openmate/paths/defaults.go`，统一默认路径与命令常量来源：
   - `.openmate/runtime/openmate.db`
   - `.openmate/runtime/vos_state.json`
   - `.openmate/bin/vos(.exe)`
   - worker 默认命令（优先 `.venv/Scripts/python.exe`）
2. 已将以下模块的硬编码默认路径收敛到共享路径模块：
   - `cmd/openmate-pool/main.go`
   - `cmd/openmate-vos-api/main.go`
   - `internal/vos/cli/cli.go`
   - `internal/schedule/cli.go`
   - `internal/openmate/runtime/runtime.go`
3. 新增 Python 共享路径模块：`openmate_shared/runtime_paths.py`，统一 `agent/pool` 的运行时默认路径解析。
4. 已将以下 Python 代码中的重复路径拼接替换为共享模块调用：
   - `openmate_agent/context_gateway.py`
   - `openmate_agent/session_gateway.py`
   - `openmate_agent/service.py`
   - `openmate_pool/pool.py`
5. 清理前端残留文件（不影响现有入口与路由）：
   - 删除空文件 `frontend/src/pages/Home/index_new.tsx`
   - 删除重复配置产物 `frontend/vite.config.js`
6. 回归验证结果：
   - `go test ./...` 通过
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
   - `frontend npm run build` 通过（首次在沙箱内触发 `spawn EPERM`，提权重跑后通过）

## 2026-04-21 CLI 帮助输出一致性修复（无功能改动）

1. 修复 Go CLI 在 `--help` 场景的重复输出问题：去掉 `flag.ErrHelp` 分支里的二次 `Usage()` 调用，避免帮助文本重复打印。
2. 影响模块：
   - `cmd/openmate-pool/main.go`
   - `internal/schedule/cli.go`
3. CLI 参数、命令名与业务 JSON 输出契约保持不变，仅调整帮助输出行为一致性。
4. 验证结果：
   - `go test ./...` 通过
   - `go run ./cmd/openmate-schedule --help`（使用仓库内 `GOCACHE/GOMODCACHE`）输出单次帮助
   - `go run ./cmd/openmate-pool --help`（使用仓库内 `GOCACHE/GOMODCACHE`）输出单次帮助
   - `.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）
