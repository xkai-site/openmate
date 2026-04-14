# Agent 能力架构

## 1. 模块目标

Agent 能力层只做一件事：把能力注入给 Agent。

这里的“能力”收敛为四个抽象：

1. `ContextInjector`：上下文注入。
2. `ToolInjector`：工具注入（专用工具 `read/write/query` + 通用 `shell`），并负责权限裁决入口。
3. `SkillInjector`：skill 注入。
4. `Assembler`：把上述注入结果组装为 Agent 可执行输入。

## 2. 模块边界

能力层负责“注入与组装”，不负责以下事项：

1. 不定义 `Topic / Node / memory / session` 语义，这些由 VFS 负责。
2. 不做调度决策与优先级算法，这些由调度队列负责。
3. 不做资源分配、模型路由、限流、熔断，这些由 Agent 池负责。

## 3. 核心接口抽象

当前阶段优先冻结接口边界，不冻结复杂策略细节。

```python
from typing import Protocol


class ContextInjector(Protocol):
    def inject(self, node_id: str) -> "ContextBundle":
        ...


class ToolInjector(Protocol):
    def inject(self, node_id: str) -> "ToolBundle":
        ...

    def authorize(self, action: "ToolAction") -> "GuardDecision":
        ...


class SkillInjector(Protocol):
    def inject(self, node_id: str) -> "SkillBundle":
        ...


class Assembler(Protocol):
    def assemble(
        self,
        context: "ContextBundle",
        tools: "ToolBundle",
        skills: "SkillBundle",
    ) -> "AgentInput":
        ...
```

说明：

1. `ContextInjector` 与 `SkillInjector` 分离，避免把上下文压缩策略和 skill 选择策略耦合。
2. `ToolInjector` 内部最小工具集合默认支持 `read/write/query/shell`，权限机制通过 `authorize` 抽象暴露。
3. `Assembler` 只负责组装，不负责执行。

## 4. 最小数据模型

为保证接口稳定，能力层先冻结最小通用数据模型（建议以 Python + Pydantic 实现）：

1. `Build`：最小字段 `node_id`。
2. `ContextBundle`：可注入上下文载荷。
3. `ToolBundle`：可注入工具定义与权限元数据。
4. `SkillBundle`：可注入 skill 列表与顺序信息。
5. `AgentInput`：Assembler 输出的可执行输入。
6. `ToolAction`：工具行为描述（用于权限裁决）。
7. `GuardDecision`：权限裁决结果（如 `allow/deny/confirm`）。

## 5. 执行接缝

能力层不直接定义资源执行系统，只保留执行接缝：

```python
class AgentExecutor(Protocol):
    def execute(self, agent_input: "AgentInput") -> str:
        ...
```

`AgentExecutor` 是能力层到 Agent 池的适配点，返回原始字符串结果即可。

## 6. 对外最小契约

对外最小接口保持不变，仅作为能力层调用面：

```python
class Build:
    node_id: str


def build(node_id: str) -> Build: ...
def execute(build: Build) -> str: ...
def priority(node_ids: list[str], hint: str | None = None) -> bool: ...
```

约束：

1. `build` 只构造执行目标，不做复杂推断。
2. `execute` 返回原始内容，避免过早绑定统一结果协议。
3. `priority` 仅作为优先级调整请求入口，不承载调度算法本身。

## 7. MVP 范围

MVP 只要求“可替换、可调用、可测试”：

1. 三类 Injector 与 Assembler 有默认实现。
2. `execute(build)` 能串起注入与组装并调用执行接缝。
3. 暴露 CLI 命令，并支持 `--help`。
4. 有基础单元测试覆盖接口连通性。

## 8. 后续扩展（非当前冻结范围）

以下能力保留为后续扩展，不在当前文档冻结细节：

1. 上下文预算算法、压缩策略、摘要策略。
2. 复杂工具注册中心策略与副作用分级。
3. skill 生命周期、冲突消解、依赖图。
4. 输出解析、持久化、下一步建议、审计与观测细则。

## 9. VOS 落盘配置（当前实现）

`AgentCapabilityService.execute()` 已支持将模型/工具事件写入 VOS `SessionEvent`。

启用方式（二选一）：

1. 直接注入 `session_gateway`。
2. 传入 `vos_state_file / vos_session_db_file / vos_binary_path`，由服务内部自动创建 `VosSessionGateway`。

示例：

```python
service = AgentCapabilityService(
    workspace_root="D:/XuKai/Project/agent",
    vos_state_file="D:/XuKai/Project/agent/.vos_state.json",
    vos_session_db_file="D:/XuKai/Project/agent/.vos_sessions.db",
    # vos_binary_path 可选，不传则自动构建/定位 cmd/vos
)
```

说明：

1. 若未配置 VOS 网关，`execute()` 仅执行推理与工具，不会落 Session/Event。
2. 若配置了 VOS 网关，`execute()` 会先 `ensure_session`，然后按 Responses 语义写事件：
   - 有工具调用：`function_call -> function_call_output -> message`
   - 无工具调用：直接 `message`
3. `function_call / function_call_output` 要求 `call_id`，`message` 不要求 `call_id`。
4. `session create` 依赖 `node_id` 已存在于 VOS state（需先有 topic/node 基础数据）。
