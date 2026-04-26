# Frontend Process

## 2026-04-26 Home 内嵌工具监控页（左栏入口切换）

1. 类型与 API：
   - `frontend/src/types/models.ts` 新增 `ToolMonitorSource`、`ToolMonitorEvent`、`ToolMonitorSummaryItem`、`ToolMonitorQuery`。
   - 新增 `frontend/src/services/api/toolMonitor.ts`：
     - `listToolMonitorEvents(query?)`
     - `listToolMonitorSummary(query?)`
   - 新增正整数前置校验：`limit/window_minutes` 非正整数时前端直接抛错阻断请求。
   - `frontend/src/services/api/index.ts` 增加 `toolMonitor` 导出。
2. Home 左栏双入口：
   - `frontend/src/pages/Home/components/ProjectPanel.tsx` 扩展为 `history/tool_monitor` 两个入口。
   - 新增受控参数 `activeSection`、`onSectionChange`，由 Home 维护当前 section。
   - 选中 `tool_monitor` 时左栏显示“右侧查看监控”提示；`history` 保持原历史会话列表行为。
3. Home 主区视图切换：
   - `frontend/src/pages/Home/index.tsx` 新增 `activeSection` 状态并传入 `ProjectPanel`。
   - `activeSection='history'` 保持原聊天页面能力不变。
   - `activeSection='tool_monitor'` 时主区渲染 `ToolMonitorPanel`，不新增路由。
4. 监控面板实现：
   - 新增 `frontend/src/pages/Home/components/ToolMonitorPanel.tsx`。
   - 筛选项：`tool_name/node_id/source/success/limit/window_minutes`。
   - `window_minutes` 支持预设按钮（15/60/1440）+ 自定义输入。
   - 支持手动刷新、默认 30s 自动刷新、默认窗口 60 分钟。
   - 使用 `Tabs` 展示“汇总表/事件表”。
   - 展示规则：
     - `before` 阶段的 `success/error_code/duration_ms` 显示 `-`。
     - `ts` 本地化显示，原值通过 tooltip 展示。
     - `success_rate` 按百分比渲染。
5. 验证：
   - `cd frontend && npm run build` 通过。

## 2026-04-26 frontend 分支初始化（安装 + 构建校验）

1. 已确认当前工作分支为 `frontend`（`git branch --show-current` 输出 `frontend`）。
2. 已完成前端依赖初始化：`cd frontend && npm install`，结果 `up to date in 3s`。
3. 已执行前端构建校验：`cd frontend && npm run build`，构建通过并产出 `frontend/dist`。
4. 构建过程中存在既有体积告警：`vendor-antd` chunk 超过 700 kB 阈值（warning），不影响本次初始化完成。

## 2026-04-26 Topic 级 Workspace 绑定（客户端先行）

1. 类型收敛：
   - `frontend/src/types/models.ts` 新增 `TopicWorkspaceBinding`、`TopicWorkspaceUpdateRequest`。
   - `NodeResponse` 补齐 `topic_id` 字段，支持由当前 node 反查所属 topic。
2. 新增 Topic Workspace API（软依赖）：
   - `frontend/src/services/api/topic.ts` 新增
     - `GET /api/v1/topics/{topic_id}/workspace`
     - `PUT /api/v1/topics/{topic_id}/workspace`
   - 对 `404/501` 统一抛出 `TopicWorkspaceUnavailableError`，由上层按“后端未接入”warning处理，不阻塞主流程。
3. Home 交互改造：
   - 新增状态：`activeTopicId`、`backendTopicWorkspaceRoot`、`pendingWorkspaceRoot`（`sessionStorage` 持久化）。
   - 点击“连接本地工作区”时：
     - 有当前 topic：直接同步到 topic，若已有绑定且将覆盖，先二次确认。
     - 无当前 topic：仅记录 pending，提示“下一个新 topic 自动绑定一次”。
   - 首条消息触发新 topic 或“开启新会话”创建成功后，若存在 pending，自动执行一次绑定并清空 pending。
   - 展示策略：有 topic 时按钮文案/提示优先按后端读取值；无 topic 时展示 pending 状态。
4. 失败策略：
   - Topic workspace 同步/读取失败仅 warning，不阻塞聊天与建 topic。
5. 验证：
   - `cd frontend && npm run build` 通过。

## 2026-04-25 frontend 分支初始化（免测，不切分支）

1. 已确认当前工作分支为 `frontend`（`git branch --show-current` 输出 `frontend`）。
2. 已完成前端依赖初始化：`cd frontend && npm install`，结果为 `up to date in 1s`。
3. 本轮按要求未执行测试命令，未创建或切换分支，仅完成初始化准备。

## 2026-04-25 流式 Markdown 渲染修复（未闭合代码块容错）

1. 新增工具函数 `frontend/src/utils/markdown.ts`：
   - `closeOpenCodeFences(content)` 在检测到 ``` fence 数量为奇数时，临时补一个闭合 fence，避免流式中段 markdown 渲染异常。
2. Home 流式渲染接入：
   - `frontend/src/pages/Home/index.tsx` 对 `streamingText` 渲染前执行 `closeOpenCodeFences(...)`。
3. Workspace 流式渲染接入：
   - `frontend/src/pages/AITree/components/SessionPanel.tsx` 对流式 `streamingText` 执行同样预处理。
   - 对流式 `thinking` 渲染也接入预处理，减少中途 markdown 半结构导致的 UI 抖动。
4. 约束：该预处理仅用于流式阶段，历史完整消息渲染链路不变。
5. 验证：`cd frontend && npm run build` 通过。

## 2026-04-24 聊天页新增“压缩上下文”按钮（Home + Workspace）

1. 前端新增节点压缩接口封装：`POST /api/v1/nodes/{id}/compact`（`frontend/src/services/api/nodes.ts`）。
2. Home 聊天页输入区新增“压缩上下文”按钮：
   - 无会话节点时禁用并提示。
   - 支持 `skipped/failed/success` 三类结果提示。
3. Workspace 聊天页（`SessionPanel`）输入区新增“压缩”按钮：
   - 压缩成功后刷新当前 `node` 数据，并触发 `onAIReply` 以联动外部面板刷新。
4. 类型契约补齐：`frontend/src/types/models.ts` 新增 `NodeCompactResponse / CompactedProcess`。
5. 构建验证：`cd frontend && npm run build` 通过。

## 2026-04-24 frontend 分支初始化（免测，不切分支）

1. 按本轮要求在 `frontend` 分支直接初始化，不执行分支创建与切换。
2. 已完成前端依赖初始化：`cd frontend && npm install`，结果为 `up to date in 2s`。
3. 本轮未执行测试命令与构建命令，仅完成初始化准备。

## 2026-04-22 Home 新会话入口文案优化

1. 左侧历史栏头部新建入口由图标语义强化为文字按钮：`开启新会话`。
2. 保持既有行为不变：仍走 Home 侧 `createNode` 新建会话逻辑，创建中显示 loading 并禁用重复点击。
3. 本轮仅调整交互可读性，不改动会话创建契约与路由行为。

## 2026-04-22 Home 新对话入口位置调整（顶栏 -> 左侧历史栏）

1. 按交互要求将“新对话”按钮从 Home 顶栏移动到左侧 `ProjectPanel` 头部，与历史会话操作聚合。
2. 行为保持不变：仍复用 Home 的 `createNode` 新建会话逻辑，创建成功后重置当前对话状态并刷新历史列表高亮。
3. 交互状态保持一致：创建中显示 loading 并禁用重复点击，不影响既有“连接本地工作区 / 生成任务树”按钮语义。

## 2026-04-22 Home 新对话入口

1. 首页顶栏新增“新对话”按钮，支持用户在 Home 直接开启独立新会话。
2. 交互语义：
   - 调用 `createNode({ name: '新对话' })` 创建新的根节点会话。
   - 创建成功后重置当前对话区状态，切换 `activeNodeId` 到新会话，并刷新左侧历史列表。
   - 会话创建中禁用重复点击，并与发送中/生成任务树中的状态互斥。
3. 本轮前端仅调整 Home 页面交互，不改动 Workspace 路由与既有聊天发送协议。

## 2026-04-22 frontend 初始化（免测，不创分支，第三次）

1. 已确认当前分支为 `frontend`（`git branch --show-current` 输出 `frontend`）。
2. 已在前端目录完成依赖初始化：`cd frontend && npm install`，结果为 `up to date in 3s`。
3. 本轮按协作要求未执行测试命令，未创建或切换分支，仅完成初始化准备。

## 2026-04-22 Home 历史项点击语义收敛（children_count 判定）

1. Home 左侧历史面板点击行为改造：
   - `children_count === 0`：不跳转 Workspace，留在 Home 切换当前会话节点。
   - `children_count > 0`：保持跳转 `/workspace/:nodeId`。
2. 会话切换补齐历史回填：
   - 新增切换会话后调用 `getNodeSession(nodeId)` 拉取历史消息并刷新对话面板，避免只切 `nodeId` 不回填内容。
3. 组件协同调整：
   - `ProjectPanel` 从内部 `navigate` 改为上抛 `onProjectSelect(project)` 回调，由 Home 统一决策跳转或留在当前页。
   - 增加当前会话项高亮（`activeNodeId`）。
4. 构建验证：
   - `cd frontend && npm run build` 通过。

## 2026-04-17 前端基线与联调对齐

1. 当前 `frontend` 已切换为你提供的既有前端实现（React + Vite），页面形态以现有版本为准，不回退到此前的 HTML+JS 预览页。
2. 已完成与 VOS 后端适配层的路由语义对齐确认：
   - 前端联调统一使用 `/api/v1/*`。
   - 响应结构统一为 `{ code, message, data }`。
3. 兼容策略约定已收口：
   - `cmd/vos` CLI 交互链路继续保留，不受前端 HTTP 适配影响。
   - 当前未落地能力（如 `chat/topic/planlist/stats` 等）由后端返回结构化 `501`，前端按业务错误处理链路兜底。
4. 前端开发代理已对齐后端 API 端口：
   - `frontend/vite.config.ts` 与 `frontend/vite.config.js` 的 `/api` 代理目标调整为 `http://127.0.0.1:8080`。
5. 前端仓库清理规则已补齐：
   - `.gitignore` 新增 `frontend/node_modules`、`frontend/dist`、`frontend/.vite`、`frontend/*.tsbuildinfo`、`frontend/vite.config.d.ts`，避免构建产物与依赖目录入库。

## 2026-04-17 Chat 对接落地（无需改动前端调用）

1. 后端已实现此前占位的聊天接口：
   - `POST /api/v1/chat`
   - `POST /api/v1/chat/stream`
2. 现有前端 `SessionPanel` 调用链保持不变：
   - `sendChatMessageStream()` 优先走 SSE
   - 流式失败时自动回退 `sendChatMessage()`
3. SSE 事件已对齐当前前端消费字段：
   - `phase`
   - `tool_call`
   - `assistant_delta`
   - `assistant_done`
   - `summary`
   - `fatal`

## 2026-04-18 Electron 本地文件能力接入（MVP）

1. 新增 Electron 桌面壳基础结构：
   - `frontend/electron/main.cjs`
   - `frontend/electron/preload.cjs`
   - `frontend/scripts/electron-dev.mjs`
2. 桌面端主进程已接入本地文件能力通道（IPC）并统一走 CLI：
   - `permission`: 选择/查询工作区
   - `file`: `read/list/write/edit/patch/glob/grep`
3. 本地文件权限策略已落地：
   - 工作区白名单约束（仅允许工作区内路径）
   - 高风险写操作确认弹窗（`write/edit/patch`）
   - 操作审计日志落盘（`.openmate/runtime/electron_audit.log`）
4. 前端已新增 Electron 桥接类型与服务封装：
   - `frontend/src/types/electron.d.ts`
   - `frontend/src/services/localFile.ts`
5. Home 页面新增桌面端入口按钮：
   - 可连接本地工作区并显示连接状态
   - 浏览器模式无该入口，不影响原有 Web 行为
6. 前端脚本与依赖已补充：
   - `electron:start`
   - `electron:dev`
   - `electron:build`
   - 新增 `electron` / `electron-builder`
7. 当前验证状态：
   - `node --check frontend/electron/main.cjs` 通过
   - `node --check frontend/electron/preload.cjs` 通过
   - `npm run build` 仍受既有 TS 历史错误影响（非本轮新增）

## 2026-04-18 Electron 接入后高强度自测与修复

1. 修复了前端既有类型链路问题，恢复 `npm run build` 可用：
   - `frontend/src/services/api.ts`：将 `api` 实例类型显式收敛为业务包体返回，消除 `ApiResponse<T>`/`T` 误配。
   - `frontend/src/pages/Home/components/ProjectPanel.tsx`：修复 `Dropdown menu` 回调的隐式 `any` 与菜单项类型不兼容。
2. Electron 打包链路修复：
   - `frontend/package.json` 的 `build.win` 新增 `signAndEditExecutable=false`，规避 Windows 环境下 `winCodeSign` 解压符号链接权限失败。
3. 高强度回归结果：
   - `npm run build` 通过（Vite + TS 全量）。
   - `npm run electron:build` 通过，产出 `frontend/dist/win-unpacked`。
   - `go test ./...` 通过。
   - `.\.venv\Scripts\python.exe -m unittest discover -s tests -p "test_*.py" -v` 通过（63 项）。

## 2026-04-18 Skill 沉淀：Electron快速入门

1. 新增技能文档：`skills/Electron快速入门/SKILL.md`。
2. 文档覆盖内容：
   - Electron 概念与多进程职责（main/preload/renderer）
   - Electron 语法与语言定位（JavaScript/TypeScript）
   - 本仓库目录与能力映射
   - 编译/测试/运行命令（`npm run electron:dev/start/build` 等）
   - 常见问题排查（权限、桥接、工作区白名单）

## 2026-04-21 前端结构清理（无行为改动）

1. 已删除空白遗留文件：`frontend/src/pages/Home/index_new.tsx`。
2. 已删除重复配置产物：`frontend/vite.config.js`（保留 `vite.config.ts` 作为唯一配置源）。
3. 本轮未改动页面路由与 API 调用逻辑，仅清理历史残留文件以降低结构噪音。
4. 构建验证：`npm run build` 通过。

## 2026-04-21 frontend 分支初始化（Web + Electron）

1. 已确认当前工作分支为 `frontend`，本阶段聚焦 Web 与 Electron 桌面壳能力。
2. 已完成前端依赖初始化：`cd frontend && npm install`。
3. Web 构建基线验证通过：`npm run build`。
4. Electron 入口语法验证通过：
   - `node --check electron/main.cjs`
   - `node --check electron/preload.cjs`
5. Electron 打包链路验证通过：`npm run electron:build`，产物输出 `frontend/dist/win-unpacked`。
6. 为避免构建重复产物回归，已完成配置收敛：
   - `frontend/tsconfig.node.json` 将输出重定向到 `node_modules/.tmp`，避免 `tsc -b` 在项目根产出 `vite.config.js`。
   - `.gitignore` 增加 `frontend/vite.config.js` 兜底忽略规则。

## 2026-04-22 frontend 分支初始化（免测）

1. 已确认当前仍在 `frontend` 分支开展初始化工作。
2. 已按前端工作区完成依赖初始化：`cd frontend && npm install`（结果 `up to date`）。
3. 本轮遵循当前协作要求，未执行测试命令，未进行分支创建或切换操作。

## 2026-04-22 Chat 流式超时恢复修复（避免长输出截断）

1. 修复长文本流式响应在中途超时后被前端直接判失败的问题，核心策略为“保留 invocation 并主动补拉最终结果”。
2. 变更点：
   - `frontend/src/services/api/chat.ts`：新增 `waitChatResult()` 轮询等待最终状态；`sendChatMessage()` 超时提升至 180s。
   - `frontend/src/pages/Home/index.tsx`：`fatal` 事件不再清理 pending invocation；流结束未收到 `summary` 时触发恢复链路，不再直接提交半截回复。
   - `frontend/src/pages/AITree/components/SessionPanel.tsx`：同样接入恢复链路，保证会话页行为一致。
3. 验证结果：`cd frontend && npm run build` 通过。

## 2026-04-22 Chat 输出链路去硬超时（看门狗化）

1. 针对“长输出被前端超时误判失败”的风险，前端输出链路改为以后端状态为准，不使用固定截止时间判定失败。
2. 变更点：
   - `frontend/src/services/api/chat.ts`：
     - `waitChatResult()` 去掉 `timeoutMs`，改为持续轮询直到状态不再是 `running`。
     - `waitChatResult()` 增加 `AbortSignal` 支持，页面卸载/中断时可终止轮询。
     - `sendChatMessage()` 与 `getChatResult()` 显式配置 `timeout: 0`（禁用请求超时），避免长输出链路被固定时长截断。
   - `frontend/src/pages/Home/index.tsx` 与 `frontend/src/pages/AITree/components/SessionPanel.tsx`：
     - 恢复链路调用 `waitChatResult()` 统一传入 `controller.signal`，保证仅由用户/页面中断控制，而非固定时长。
3. 验证结果：`cd frontend && npm run build` 通过。

## 2026-04-22 测试清理脚本（历史数据与前端缓存）

1. 新增脚本：`scripts/clean-test-history.ps1`，用于测试前一键清理历史数据与前端缓存。
2. 清理范围（仅仓库内）：
   - 运行态历史：`.openmate/runtime/openmate.db*`、`.openmate/runtime/vos_state.json`、`.openmate/runtime/electron_audit.log`
   - 兼容旧文件：`.vos_state.json`、`.pool_state.json`、`.pool_state.db*`
   - 前端缓存：`frontend/dist`、`frontend/.vite`、`frontend/release`、`frontend/electron-dist`、`frontend/*.tsbuildinfo`、`frontend/vite.config.d.ts`
3. 保护策略：
   - 显式保留 `model.json`
   - 不清理 `.openmate/bin`、`.venv`、`frontend/node_modules`
   - 脚本内含“仅允许删除 workspace 根目录内路径”的安全检查
4. 校验结果：
   - 已执行 `.\scripts\clean-test-history.ps1 -WorkspaceRoot . -WhatIf`，干跑通过。

## 2026-04-22 测试清理脚本路径解析修复

1. 修复 `scripts/clean-test-history.ps1` 的根路径解析方式：由 `.NET GetFullPath(.)` 改为 `PowerShell Provider` 解析，避免在部分终端环境下把 `.` 误解析为进程启动目录（如 `C:\Users\HP`）。
2. 修复后 `-WorkspaceRoot .` 在 `D:\XuKai\Project\openmate_frontend` 下可正确解析为仓库根目录，清理目标与预期一致。
3. 验证：`.\scripts\clean-test-history.ps1 -WorkspaceRoot . -WhatIf` 输出 `Workspace root: D:\XuKai\Project\openmate_frontend`。

## 2026-04-22 长输出中断联调结论（前后端联合）

1. 复盘结论：前端已去硬超时后，长输出仍中断的直接触发点在后端流式链路固定超时，而非前端请求超时。
2. 后端配合修复已落地：
   - `internal/vos/httpapi/chat.go` 去除流式调用和会话等待中的固定 2 分钟超时截断。
   - `internal/poolgateway/providers.go` 对流式请求默认使用不限时 HTTP client（未显式 `timeout_ms` 时）。
3. 联调结果：后端单测 `go test ./internal/poolgateway/... ./internal/vos/httpapi/...` 通过，前端输出恢复策略继续沿用“以后端状态为准”。

## 2026-04-22 frontend 分支初始化（免测，第二次）

1. 已确认当前工作分支为 `frontend`（`git status --short --branch` 显示 `## frontend...origin/frontend`）。
2. 已完成前端依赖初始化：`cd frontend && npm install`，结果为 `up to date in 4s`。
3. 本轮按协作要求未执行测试命令，未创建/切换分支，仅完成初始化准备。
