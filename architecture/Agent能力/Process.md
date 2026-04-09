# Process 记录

## 2026-04-09

### 本次决策

1. 将 Agent 能力文档从“细粒度流水线分段”收敛为“能力注入核心抽象”。
2. 核心接口统一为：`ContextInjector`、`ToolInjector`、`SkillInjector`、`Assembler`。
3. 明确 `ToolInjector` 需内置权限裁决入口，最小工具集合为 `read/write/query`。
4. `Context` 与 `Skill` 明确拆分，避免上下文压缩策略与 skill 策略耦合。
5. 保留对外最小契约：`build/execute/priority`，并继续约束能力层不承载调度与资源路由。

### 当前状态

1. `architecture/Agent能力/Agent能力.md` 已重写为接口层优先版本。
2. 复杂执行后处理能力（解析、持久化、建议、审计）暂归入后续扩展，不在当前冻结。

### 下一步建议

1. 基于新文档先实现 Pydantic 数据模型与 Protocol 接口骨架。
2. 先提供默认注入实现与 CLI 对外入口，随后补单元测试。

### 本次实现落地

1. 已新增 `openmate_agent` 包，完成最小数据模型（Pydantic）与接口层（Protocol）骨架。
2. 已实现默认能力组件：
   - `DefaultContextInjector`
   - `DefaultToolInjector`（`read/write/query` + `authorize`）
   - `DefaultSkillInjector`
   - `DefaultAssembler`
   - `DefaultAgentExecutor`
3. 已实现 `AgentCapabilityService`，对外提供 `build/execute/priority`。
4. 已新增 CLI 入口 `openmate_agent.cli`，支持 `build/execute/priority` 与 `--help`。
5. 已新增单元测试并通过：
   - `tests/test_service.py`
   - `tests/test_cli.py`
   - 命令：`.\\.venv\\Scripts\\python.exe -m unittest discover -s tests -p "test_*.py" -v`

### 增量实现（ToolRuntime）

1. 新增 `ToolRuntime` 抽象，并落地本地实现 `LocalToolRuntime`，对外统一 `read/write/query` 语义。
2. 工具实现采取“封装一层”方案：CLI 不直接绑定系统命令，而是调用 `AgentCapabilityService.run_tool`。
3. 能力语义修正为：`read/write` 针对文件系统，`query` 针对网络调用（HTTP GET/POST）。
4. 加入工作目录边界约束，避免越界读写。
5. CLI 新增 `tool` 子命令：
   - `tool read <node_id> --path ...`
   - `tool write <node_id> --path ... --content ... [--mode overwrite|append]`
   - `tool query <node_id> --url ... [--method GET|POST] [--params '{}'] [--headers '{}'] [--body '{}']`
6. 增补测试覆盖 `run_tool` 与 CLI tool 命令，包含本地 HTTP 测试服务，当前测试总数 11，全部通过。

### 增量重构（Tool 抽象层）

1. 工具层重构为注册式抽象：`ToolRegistry + Tool 抽象 + PermissionGateway`。
2. 新增专用工具实现：`ReadTool / WriteTool / QueryTool`，并新增通用 `ShellTool`。
3. 语义固定为：
   - `read/write` 针对文件系统
   - `query` 针对网络调用（HTTP GET/POST）
   - `shell` 承接系统命令（如 `git`）
4. `AgentCapabilityService.run_tool` 已改为：
   - 构建 `ToolAction`
   - 走 `PermissionGateway`
   - 通过 `ToolRegistry` 分发执行
5. CLI 已支持：
   - `tool shell <node_id> --cmd "..." [--cwd ...] [--timeout-seconds ...]`
6. 删除旧 `runtime.py` 路径，避免与新抽象并存造成歧义。
7. 单测已更新并通过，当前测试总数 13，全部通过。

### 增量更新（权限标记位）

1. 工具调用模型新增两个布尔入参：`is_safe`、`is_read_only`，默认值均为 `false`。
2. 权限网关改为两阶段：
   - 当两个标记均为 `false` 时返回 `confirm`（先记录确认需求，后续接 LangGraph 人机确认流）。
   - 当两个标记均为 `true` 时才进入黑白名单路由。
3. 黑名单仍作为兜底策略：即使标记为 `true`，危险 shell 命令仍会被拒绝。
4. CLI 所有 `tool` 子命令已支持 `--is-safe` 与 `--is-read-only`。
5. 已补充对应测试用例，当前测试总数 15，全部通过。

### 对外暴露面收敛

1. 根据当前阶段目标（聚焦 Agent 手脚/Tool），CLI 对外暴露面已收敛为仅 `tool` 命令。
2. `build/execute/priority` 仍保留为服务层能力，但不再作为 CLI 命令直接暴露。

### 文件工具安全重构

1. 新增共享机制：
   - `FileTimeStore`：记录 `read` 后文件 mtime 基线。
   - `FileLockManager`：提供 `with_lock` 文件互斥锁。
2. `read` 升级为上下文限流阅读器：
   - 支持 `offset/limit` 分页。
   - 硬限制：最多 2000 行、单行最多 2000 字符、总输出最多 50KB。
   - 文件输出增加 `|line|` 行号前缀。
   - 目录路径返回结构化列表（名称/类型/大小）。
   - 二进制文件检测并拒绝输出。
   - 成功读取后写入 FileTime 基线。
3. `write` 升级为全量覆盖写入：
   - 写入前执行 FileTime 冲突校验。
   - 生成 unified diff 预览并返回在输出中。
   - 写入后更新 FileTime 基线。
   - 对 Python 文件执行基础编译诊断并回传。
4. 新增 `edit` 工具：
   - 参数为 `old_string -> new_string`，不依赖行号。
   - 匹配策略：`ExactMatch`、`WhitespaceNormalizer`、`BlockAnchorReplacer`、`SimilarityMatcher`、`LineTrimMatcher`。
   - 相似度候选策略：低于 30% 丢弃；单候选高于 70% 才自动通过；多候选时拒绝并提示歧义。
   - 执行前校验 FileTime，执行过程持有文件锁。
5. CLI 工具子命令新增 `tool edit`，`tool read` 新增 `offset/limit` 参数。
6. 测试覆盖冲突拦截、read/write/edit 主链路与 CLI，当前测试总数 14，全部通过。

### 检索工具新增（grep / glob）

1. 新增 `grep` 工具：
   - 基于 `ripgrep (rg)` 做内容检索。
   - 支持正则表达式匹配（pattern）。
   - 支持 `scope`、`max_results`、`file_glob` 参数。
2. 新增 `glob` 工具：
   - 基于 `rg --files` 获取文件列表。
   - 使用 Python `fnmatch` 按通配模式过滤（如 `**/*.ts`）。
   - 保持 `.gitignore` 过滤语义。
3. 修复 `glob` 忽略规则问题：
   - 不再直接将 `-g pattern` 交给 `rg --files`，避免把忽略规则覆盖。
   - 改为先获取已忽略过滤后的候选，再本地匹配 pattern。
4. `ToolRegistry`、`PermissionGateway`、CLI 子命令已同步注册：
   - `tool grep ...`
   - `tool glob ...`
5. 共享依赖已沉淀至 `architecture/sharedInfo/依赖.md`，明确 `ripgrep (rg)` 为前置依赖。
6. 单测已覆盖 `grep/glob`，当前测试总数 16，全部通过。
