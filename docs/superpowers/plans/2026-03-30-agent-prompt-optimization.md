# Agent Prompt Optimization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 统一 Mini-Code 的基础系统提示来源，并强化 `coder`、`reviewer`、`researcher` 三类 agent 的角色提示词。

**Architecture:** 抽出共享基础提示词构建器，由 `engine` 和 `orchestrator` 共同复用；`BaseAgent` 将角色提示词追加到基础提示词后执行，而不是整体覆盖。

**Tech Stack:** Go 标准库、现有 `memory` 模块、现有 agent / orchestrator 测试体系。

---

### Task 1: 为提示词重构补测试

**Files:**
- Create: `pkg/agent/prompt_test.go`
- Modify: `pkg/agent/agents_test.go`
- Test: `pkg/orchestrator/orchestrator_test.go`

- [ ] **Step 1: 写失败测试，覆盖基础提示词关键规则**
- [ ] **Step 2: 写失败测试，验证专化 agent 不再覆盖基础提示词**
- [ ] **Step 3: 写失败测试，验证三个角色提示词包含关键约束**
- [ ] **Step 4: 运行 `go test ./pkg/agent ./pkg/orchestrator`，确认失败原因正确**

### Task 2: 提取共享基础提示词构建器

**Files:**
- Create: `pkg/agent/prompt.go`
- Modify: `pkg/agent/engine.go`
- Modify: `pkg/orchestrator/orchestrator.go`

- [ ] **Step 1: 提取基础提示词与记忆拼接逻辑到共享构建器**
- [ ] **Step 2: 让 `engine` 改为调用共享构建器**
- [ ] **Step 3: 让 `orchestrator` 改为调用共享构建器**
- [ ] **Step 4: 运行相关测试，确认通过**

### Task 3: 强化角色提示词并修正组合方式

**Files:**
- Modify: `pkg/agent/base.go`
- Modify: `pkg/agent/coder.go`
- Modify: `pkg/agent/reviewer.go`
- Modify: `pkg/agent/researcher.go`

- [ ] **Step 1: 修改 `BaseAgent.Run()`，将角色提示词追加到基础 system prompt**
- [ ] **Step 2: 更新 `coder` 的执行型提示词**
- [ ] **Step 3: 更新 `reviewer` 的 findings-first 提示词**
- [ ] **Step 4: 更新 `researcher` 的证据型提示词**
- [ ] **Step 5: 运行 `go test ./pkg/agent ./pkg/orchestrator`，确认通过**

### Task 4: 回归验证

**Files:**
- Test: `pkg/agent/...`
- Test: `pkg/orchestrator/...`

- [ ] **Step 1: 运行 `go test ./pkg/agent/... -count=1`**
- [ ] **Step 2: 运行 `go test ./pkg/orchestrator/... -count=1`**
- [ ] **Step 3: 如有失败，修复后重新验证**
