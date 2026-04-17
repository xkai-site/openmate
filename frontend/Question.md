# 前后端对齐问题记录

## 2026-03-19

1. `GET /api/v1/nodes/{node_id}/detail` 当前文档字段包含 `progress`，但前端规划中有独立 `Progress/Step` 双面板需求。
   - 已知 API 中没有独立 `steps` 字段或 `steps` 查询端点。
   - 待确认：是否将 `steps` 与 `progress` 合并展示，或补充独立 `steps` 输出字段。

2. Topic 页面需要“任务调度列表（含 retry_count / max_retries）”，但当前 API 仅提供：
   - `GET /api/v1/topic/{id}/task/{task_id}`（单任务查询）
   - `GET /api/v1/topic/{id}/logs`（可批量但字段偏日志）
   - `GET /api/v1/topic/{id}/results`（可批量但字段偏结果）
   - 待确认：是否补充 `GET /api/v1/topic/{id}/tasks` 批量任务接口，便于前端直接展示调度视图。

3. 目前前端已实现从 Topic 通过 `node_id` 跳转 AITree（`/aitree?nodeId=...`），
   但后端尚无 `node_id -> topic_id` 反查接口，导致 AITree 暂无法准确反跳到对应 Topic。
   - 待确认：是否提供 `GET /api/v1/topic/by-node/{node_id}` 或等价查询能力。

4. **一句话拆解树形结构 API 缺失**
   - 新规划的 Home 页面需要接收用户输入的一句话，并在后端拆解生成一颗 AITree，返回初始的树结构信息或 Topic/Tree ID 以供前端跳转展示。
   - 待确认：需要新增 `POST /api/v1/tree/generate`（或类似端点）处理输入并返回构建好的节点关联信息。

5. **Home 页面“直接开始”能力需要后端契约确认**
   - 新增“直接开始”按钮：该入口不调用拆解 API，而是直接创建单个 Node 并进入 Workspace。
   - 当前前端计划走 `POST /api/v1/nodes` 创建根节点（`parent_id = null`）。
   - 待确认：后端是否保证最小字段（`id` + 可选 `name/input`）可直接创建可工作的初始节点，并返回可用于跳转的 `node_id`。

