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
