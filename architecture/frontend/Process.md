# Frontend Process

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
