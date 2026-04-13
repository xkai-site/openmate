# 调度队列 Process

## 2026-04-11 初始化

### 已完成

1. 阅读 `AGENTS.md`、`architecture/调度队列/调度队列.md`、`architecture/sharedInfo/架构.md`、`architecture/sharedInfo/依赖.md`。
2. 确认当前开发分支为 `schedule`。
3. 补建项目内虚拟环境 `.venv`，并执行 `pip install -r requirements.txt`。
4. 初始化 `openmate_schedule` 模块，新增：
   - `models.py`：Topic/Node/Runtime/DispatchPlan 强类型模型。
   - `scheduler.py`：MVP 级 dispatch planning 逻辑。
   - `cli.py`：`schedule plan --input-file ... --available-slots ...`。
   - `__main__.py` / `__init__.py`：支持模块方式调用与导出。
5. 新增 `tests/test_schedule.py`，覆盖：
   - CLI `--help`
   - continuation-first 行为
   - `last_worked_node_id` 回退
   - active layer stalled 行为

### 测试结果

1. `.\.venv\Scripts\python.exe -m unittest tests.test_schedule` 通过。
2. `.\.venv\Scripts\python.exe -m unittest discover -s tests` 未全绿。

### 风险与待协作

1. 全量测试中，`openmate_pool` 相关 4 个用例在 Windows 临时目录清理阶段失败，现象为 `pool_state.db` 被占用，怀疑 SQLite 连接或文件句柄未及时释放。
2. 该问题属于 Agent池模块历史问题，本次未在 `schedule` 板块内处理；后续如需联调，应先和池模块确认资源释放约定。

### 下一步建议

1. 继续把一级 Topic MLFQ、二级 active layer 索引和 dirty 重算接到同一模块。
2. 为 TopicSnapshot 引入更贴近架构文档的事件输入模型与运行态变更入口。
3. 等 Agent池侧释放问题收敛后，再补跨模块集成测试。
