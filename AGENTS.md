## 开发

- 使用`UTF-8`读取文件
- 默认使用强类型实现，当前冻结技术栈以 `architecture/sharedInfo/模块契约.md` 为准：
  - `vos / pool / schedule` 使用 Go
  - `agent` 使用 Python + Pydantic
- Python 环境搭建，使用项目内环境搭建方式，已搭建项目内虚拟环境（.venv）。不使用 conda/docker 等外部环境管理。
  - 若需要新的 Python 依赖，更新 requirements.txt
  - 然后使用 pip install -r requirements.txt
- Go 模块
   - 复用仓库根 `go.mod / go.sum`，若新增 Go 依赖，同时更新 `architecture/sharedInfo/Go依赖.md`
   - 需要按仓库约定切到工作区内`GOCACHE/GOMODCACHE`，而不是本机 Go 缓存目录。
- 开发规范：
   - 遵循 Git 提交信息规范（feat/fix/docs/...）
  - 单元测试完成后，才进行下一步操作


- 开发功能点用cli形式暴露，并支持 `-- help`查询帮助。暂时不需要使用api或mcp等形式暴露。

## 协作

使用git开发，不同层级采用不同分支，安排如下：

- 虚拟文件系统开发使用vos分支
- 调度队列开发使用schedule分支
- Agent池开发使用pool分支
- Agent能力开发使用agent分支

当一个开发阶段完成并认为能够被其余模块复用时（即不存在开发中间态，不可用却发布），才进行push

## 沉淀

architecture文件夹下有你们各自对应的文档区，一般情况下你们只需要操作自己分工对应的文档区即可，也可以对sharedinfo做操作，详情如下：

- 内部读取，每次开发前应该读取一次内部Process，开发后应更新一次内部Process，表示内部开发进展。

- 当你发现需要跨部门合作时，也可以从sharedInfo中尝试读取内容。当然，你也需要在你觉得你的操作对全局重要的时候，去sharedInfo中更新共享内容。
