# Self Bootstrap Restart Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 Mini-Code 在 agent 修改完源码后，能够本地构建新版本并由父进程托管平滑切换到新 worker。

**Architecture:** 默认入口改为 supervisor，负责托管当前 worker、接收重启请求、启动 standby worker，并在新 worker 就绪后再切换。worker 继续承接现有 CLI 与 agent 逻辑，通过 `run_shell("mini-code restart")` 触发构建并登记待切换版本，任务完成后导出当前 session 快照并请求 supervisor 进行安全切换。

**Tech Stack:** Go, os/exec, net, encoding/json, current orchestrator/session/tool stack

---

### Task 1: 定义重启调度与会话快照边界

**Files:**
- Create: `pkg/supervisor/runtime.go`
- Modify: `pkg/orchestrator/session.go`
- Test: `pkg/orchestrator/session_snapshot_test.go`

- [ ] **Step 1: 写失败测试，覆盖 session 快照 round-trip**
- [ ] **Step 2: 运行测试，确认当前不存在快照能力**
- [ ] **Step 3: 为 Session 增加可序列化快照结构与恢复方法**
- [ ] **Step 4: 再次运行测试，确认快照行为通过**

### Task 2: 为 run_shell 增加精确重启拦截

**Files:**
- Create: `pkg/tools/restart.go`
- Modify: `pkg/tools/file.go`
- Test: `pkg/tools/run_shell_test.go`

- [ ] **Step 1: 写失败测试，覆盖 `mini-code restart` 被拦截而普通命令不受影响**
- [ ] **Step 2: 运行测试，确认拦截逻辑尚未实现**
- [ ] **Step 3: 增加 restart handler 注入点，并在 `RunShell` 中精确匹配命令**
- [ ] **Step 4: 再次运行测试，确认工具层行为正确**

### Task 3: 实现 worker 侧自举构建与待切换状态

**Files:**
- Create: `pkg/supervisor/worker_runtime.go`
- Modify: `pkg/orchestrator/orchestrator.go`
- Test: `pkg/supervisor/worker_runtime_test.go`

- [ ] **Step 1: 写失败测试，覆盖构建成功后登记 pending restart，构建失败时不登记**
- [ ] **Step 2: 运行测试，确认当前没有 worker runtime**
- [ ] **Step 3: 实现 build/restart 请求准备、快照导出、任务结束后触发切换**
- [ ] **Step 4: 再次运行测试，确认 pending restart 链路通过**

### Task 4: 实现 supervisor/worker 控制协议与入口拆分

**Files:**
- Create: `pkg/supervisor/control.go`
- Modify: `cmd/agent/main.go`
- Modify: `cmd/agent/main_test.go`
- Test: `pkg/supervisor/control_test.go`

- [ ] **Step 1: 写失败测试，覆盖 standby worker ready 后才能 promote，失败时保留旧 worker**
- [ ] **Step 2: 运行测试，确认当前没有 supervisor 控制协议**
- [ ] **Step 3: 拆分 main 为 supervisor/worker 两个角色，并实现控制消息与切换逻辑**
- [ ] **Step 4: 再次运行测试，确认主链路通过**

### Task 5: 定向验证端到端最小链路

**Files:**
- Modify: `pkg/tools/run_shell_test.go`
- Modify: `pkg/orchestrator/orchestrator_test.go`
- Modify: `cmd/agent/main_test.go`

- [ ] **Step 1: 增加回归测试，覆盖任务完成后触发 pending restart 请求**
- [ ] **Step 2: 运行相关包测试，确认行为稳定**
- [ ] **Step 3: 根据测试结果收尾修正，不跑 lint/build**
