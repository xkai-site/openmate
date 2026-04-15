# 虚拟文件系统文档索引

## 当前建议阅读顺序

1. [虚拟文件系统.md](D:/XuKai/Project/vos/architecture/虚拟文件系统/虚拟文件系统.md)
   - VOS 的长期架构语义：Topic/Node/Memory/Leaf 约束。
2. [Session与SessionEvent字段收敛.md](D:/XuKai/Project/vos/architecture/虚拟文件系统/Session与SessionEvent字段收敛.md)
   - 当前已落地的 Session 存储模型与兼容策略。
3. [Process.md](D:/XuKai/Project/vos/architecture/虚拟文件系统/Process.md)
   - VOS 迁移与实现里程碑（按时间）。

## 去重说明

- Session/SessionEvent 字段定义以 `Session与SessionEvent字段收敛.md` 为唯一来源。
- 工具调用跨模块契约不在本目录重复维护，统一引用：
  - `architecture/sharedInfo/工具调用-SessionEvent契约.md`
