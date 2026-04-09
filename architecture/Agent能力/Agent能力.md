# Agent 能力架构

## 1. 模块定位

Agent 能力层负责把一次 `Node` 级工作组装成可执行内容，并在执行前后完成能力编排。

它回答的问题是：

1. 如何从已有系统对象中取出本次执行需要的上下文。
2. 如何为本次执行挂接可用的 `tool` 与 `skill`。
3. 如何把这些能力与 Prompt 风格组装成模型可执行输入，并形成不同 Agent 工作方式。
4. 如何把执行结果解析、落盘，并反馈后续动作建议。

它不负责：

1. 不定义 `Topic / Node / memory / session` 的结构语义。
2. 不决定哪个 `Topic` 或 `Node` 先执行，这由调度层负责。
3. 不管理执行资源、模型路由、限流和原始 usage 计量，这由 Agent 池负责。

## 2. 设计原则

1. 接口优先：先冻结能力边界与最小契约，再逐步补充具体策略。
2. 编排优先：能力层的职责是组装与协调，而不是过早固化具体实现细节。
3. 单一真源：上下文来源与记忆读写语义以虚拟文件系统定义为准，能力层只消费这些定义。
4. 可插拔：上下文、tool、skill、prompt、解析、存储都应支持替换实现。
5. 可治理：权限、输出校验、审计应作为能力层公共横切接口存在。
6. 可观测：每个阶段都应具备独立耗时、命中率、失败率等基础观测能力。

## 3. 与其他模块的边界

模块边界可以收敛成三句话：

1. 虚拟文件系统定义“系统里有什么对象，以及上下文从哪里读”。
2. 能力层定义“本次执行如何组装这些对象与能力”。
3. Agent 池定义“组装好的执行内容交给哪个模型通道运行”。

因此：

2. 如果资源分配或模型路由发生变化，应在 Agent 池演进，而不是下沉到能力层。
3. 能力层应尽量避免重新定义 VFS 已经明确的记忆分层、读写路径和对象语义。

## 4. 执行流水线

建议把能力层表达为一条接口化流水线：

```text
NodeRef
  -> Context Provider
  -> Tool Resolver
  -> Skill Resolver
  -> Prompt Assembler
  -> Agent Executor
  -> Output Parser
  -> Persistence Writer
  -> NextAction Planner
```

这条链路中的每一段都应该满足：

1. 可单独替换实现。
2. 可独立测试。
3. 可记录输入摘要、输出摘要和耗时。

## 5. 上下文装配与 Prompt 接口

上下文接口和 Prompt 接口建议放在同一组讨论，因为两者本质上描述的是同一条装配链路的两个视角：

1. 上下文是设计视角，回答“本次执行需要哪些信息”。
2. Prompt 是技术实现视角，回答“这些信息最终如何投影成模型输入”。

上下文的来源与读取哲学不在本章重新定义；本章只定义能力层如何向上游取上下文，并把这些上下文装配为模型输入。

```ts
interface ContextProvider {
  load(nodeId: string, budgetTokens?: number): Promise<ExecutionContext>;
}

interface PromptAssembler {
  assemble(input: AssemblyInput): Promise<ModelInput>;
}
```

说明：

1. `ContextProvider` 面向的是“取上下文包”，而不是自行发明新的记忆模型。
2. `ExecutionContext` 的内容来源由 VFS 决定，能力层只负责按预算和场景做装配。
3. `PromptAssembler` 负责把上下文、tool、skill 等能力产物投影成模型可消费输入。
4. 同一套底层执行资源，可以通过不同 Prompt 模板、提示策略和装配方式，表现出不同 Agent 风格；这属于能力层，而不是资源层。
5. 当前阶段不在本章冻结上下文内部层次、摘要策略、提示词模板和裁剪细则。
6. 实现上这两者可以由同一组件承担，只是在接口上拆开，便于替换和测试。

## 6. Tool 接口

工具能力建议通过解析器和权限守卫接入，而不是在本章提前冻结完整工具模型。

```ts
interface ToolResolver {
  resolve(build: Build, context: ExecutionContext): Promise<ToolBinding[]>;
}

interface PermissionPolicy {
  filterTools(nodeId: string, tools: ToolBinding[]): Promise<ToolBinding[]>;
  requireConfirmation(nodeId: string, action: RiskyAction): Promise<GuardDecision>;
}
```

说明：

1. `ToolResolver` 只负责“本次执行可以挂哪些工具”。
2. 权限、确认、白名单等治理逻辑通过独立接口承载。
3. 具体 `tool schema`、副作用分级、注册中心细节可以后续单列文档细化。

## 7. Skill 接口

skill 应被视为可组合的行为扩展，而不是预绑定到某个 Agent 身上的固定属性。

```ts
interface SkillResolver {
  resolve(build: Build, context: ExecutionContext): Promise<SkillBinding[]>;
}
```

说明：

1. `SkillResolver` 只回答“当前执行应注入哪些 skill”。
2. skill 的排序、冲突消解、依赖关系可以作为实现策略逐步补充。
3. 本章不提前冻结 skill manifest、hook 生命周期等完整细节。

## 8. 执行接口

能力层把上下文、tool、skill 装配为 `ModelInput` 后，再通过 Agent 池执行。

```ts
interface AgentExecutor {
  execute(ticketId: string, modelInput: ModelInput): Promise<ModelOutput>;
}
```

说明：

1. `AgentExecutor` 是对 Agent 池执行接口的适配，而不是新的资源层。
2. 模型选择、路由、限流、熔断等问题仍属于 Agent 池。

## 9. 输出与存储接口

执行后的解析、回写与后续动作建议，也应保持接口化。

```ts
interface OutputParser {
  parse(nodeId: string, output: ModelOutput): Promise<ParsedExecutionResult>;
}

interface PersistenceWriter {
  write(nodeId: string, result: ParsedExecutionResult): Promise<PersistenceResult>;
}

interface NextActionPlanner {
  suggest(nodeId: string, result: ParsedExecutionResult): Promise<NextActionSuggestion[]>;
}
```

说明：

1. `OutputParser` 负责把模型原始输出转换成系统可消费结果。
2. `PersistenceWriter` 负责把会话、产物、记忆写回 VFS 或相关存储。
3. `NextActionPlanner` 负责向调度层提供下一步建议，而不是直接代替调度决策。

## 10. 治理与审计接口

能力层需要预留统一治理接口，避免每个能力点各自实现安全逻辑。

```ts
interface OutputGuard {
  validate(nodeId: string, result: ParsedExecutionResult): Promise<GuardDecision>;
}

interface AuditSink {
  record(event: CapabilityEvent): Promise<void>;
}
```

建议当前阶段至少覆盖：

1. 工具白名单与风险操作确认。
2. 输出结构校验。
3. 调用链审计。
4. 输入摘要与输出摘要记录。

## 11. 对外最小接口与执行契约

对外仍建议只暴露最小能力层接口，而不暴露内部实现细节：

```ts
interface Build {
  nodeId: string;
}

Build(nodeId: string): Build
Execute(build: Build): string
Priority(nodeIds: string[], hint?: string): boolean
```

对应关系是：

1. `Build` 的入参是 `nodeId`，出参是 `Build` 本身，也就是当前这次执行的装配对象；当前阶段最小只冻结 `nodeId`，其余字段不提前写死。
2. `Execute` 的入参是 `Build`，出参是 LLM API 返回的原始 `content`，而不是在这一层先强行包装成统一结果对象。
3. `Priority` 的入参是多个 `nodeId` 和可选 `hint`，出参是 `true / false`，表示这次优先级调整是否已经完成。
4. 这三个接口更适合作为能力层的最小调用面，而不是被理解为固定 Agent 类型或固定模块链路。

## 12. 可观测性

当前阶段建议只冻结观测类别，不提前冻结过细指标口径。

至少应覆盖：

1. 上下文加载耗时。
2. tool 解析与挂接成功率。
3. skill 解析与挂接成功率。
4. 执行成功率与失败分类。
5. 结果回写成功率。
6. 端到端执行耗时。

## 13. MVP 范围

MVP 重点是把接口链路跑通，而不是把每个能力点一次性做满。

建议只覆盖：

1. 一个基础 `ContextProvider`，能够消费 VFS 已定义的上下文读取结果。
2. 一个基础 `ToolResolver` 和最小权限守卫。
3. 一个基础 `SkillResolver`。
4. 一个基础 `PromptAssembler`。
5. 一个基础 `OutputParser` 与 `PersistenceWriter`。
6. 一个基础 `NextActionPlanner`。

当前阶段暂不在本章展开以下细节：

1. 上下文预算算法。
2. 复杂 tool 元信息模型。
3. 复杂 skill 生命周期与冲突规则。
4. 记忆提炼、压缩、合并的具体策略。
5. 外部检索与高级知识注入机制。

## 14. 并行开发分工建议

如果按接口并行推进，建议这样拆分：

1. A 组：`ContextProvider + PromptAssembler`
2. B 组：`ToolResolver + PermissionPolicy + OutputGuard`
3. C 组：`SkillResolver + OutputParser + PersistenceWriter`
4. D 组：`NextActionPlanner + AuditSink + 可观测性埋点`
