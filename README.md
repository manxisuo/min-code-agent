# Min Code Agent

Min Code Agent 是一个面向学习、实验和研究的轻量级 Code Agent Runtime。

它的目标不是复制 Claude Code、Codex CLI、Cursor Agent 或 OpenCode，也不是追求功能数量，而是通过一个结构清晰、行为透明、可观测、可扩展的实现，理解现代 Code Agent 的核心工作机制。

## 核心目标

项目围绕三个关键词设计：

- **Build**：实现一个最小但完整的 Code Agent Runtime。
- **Observe**：清楚看到 Agent 在运行过程中发生了什么。
- **Experiment**：方便替换模型、Prompt、Tool、Context 策略和 Agent 策略，并比较不同方案。

因此，本项目不仅是一个 Code Agent，也是一个：

> Code Agent 实验平台 + 调试器 + 学习工具。

## 设计原则

1. **可理解优先**  
   架构、调用链和模块边界必须清晰，不为了“工程高级感”引入过度抽象。

2. **可观测性是一等公民**  
   Agent、LLM、Context、Tool、Permission、Session、Compression 等核心行为都应产生结构化事件。

3. **模型只是组件**  
   Agent 能力来自 LLM、Context Engineering、Tool Design、Agent Loop、环境反馈、安全边界和可观测性的组合。Provider 必须可替换。

4. **默认安全**  
   模型输出视为不可信输入。文件、Shell、Git 等能力必须受 workspace 和权限策略约束。

5. **小步演进**  
   先完成最小闭环，再逐步增加修改、验证、持久化、Context Compression、Skill、Memory 等能力。

## 非目标

初期明确不做：

- 完整 IDE 或 Cursor 类编辑器
- 云端 SaaS
- 多用户或企业权限系统
- 完整 MCP Host
- IDE 插件
- 复杂 Multi-Agent
- Computer Use
- 通用 AST 重构系统
- 一次性支持所有 LLM Provider
- 长时间完全自主运行的 Agent

## 推荐技术栈

```text
Language        Go
CLI             Cobra 或标准 flag
Logging         slog
Config          YAML
Storage         JSON / JSONL
Session         JSON
Trace           JSONL
Testing         Go testing
LLM             OpenAI-compatible API
```

TUI 暂不作为 MVP 目标。后续如果 Inspector 需要更强交互，可考虑 Bubble Tea。

## MVP 能力

第一版 MVP 应包含：

```text
CLI REPL
OpenAI-compatible Provider

Agent Loop

read_file
list_dir
glob
grep
edit_file
shell

Permission

Basic Context

Event Bus
Trace Recorder
Timeline
Context Snapshot
Tool Inspector
Basic Metrics
```

Min Code Agent 与普通练习型 Code Agent 最大的区别，是从 MVP 开始就包含 **Observation Layer**。

## 使用示例

```bash
mincode
```

指定工作区：

```bash
mincode ./project
```

单次执行：

```bash
mincode -p "分析这个项目"
```

Inspector 模式：

```bash
mincode --inspect
```

恢复会话：

```bash
mincode --continue
```

回放 Trace：

```bash
mincode replay <session-id>
```

## 典型执行流程

```text
用户输入
    ↓
Prompt / Instructions
    ↓
Context Construction
    ↓
LLM Request
    ↓
Tool Selection
    ↓
Environment Feedback
    ↓
Context Update
    ↓
下一轮决策
    ↓
代码修改
    ↓
测试验证
    ↓
最终回答
```

Min Code Agent 的目标不是只让 Agent “能工作”，而是让开发者能够理解：

- 模型这一轮到底看到了什么
- Agent 为什么调用某个 Tool
- Tool 对下一轮 Context 产生了什么影响
- Token 如何增长
- Context 何时被裁剪或压缩
- 失败后 Agent 如何恢复
- 不同模型和策略之间的差异

## 文档

- [架构设计](docs/architecture.md)
- [可观测性设计](docs/observability.md)
- [开发路线图](docs/roadmap.md)
- [Agent 开发约束](AGENTS.md)

## 第一阶段验收

第一版应至少能稳定完成以下任务：

```text
分析这个 Go 项目的入口和整体结构。
```

```text
找出仓库中的所有 TODO。
```

```text
新增一个 /health 接口，并补测试。
```

```text
运行测试；如果失败，根据错误修改代码后重新测试。
```

同时，开发者应能通过 Trace / Inspector 查看完整执行链，而不是只看到最终答案。
