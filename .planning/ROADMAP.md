# Roadmap: Mini-Code Telegram Channel 修复

## Overview

修复 Telegram Bot 频道的三个已知问题：消除双消息通知噪声、为每个用户添加并发任务限制、为文件下载添加 HTTP 超时。两个阶段交付全部修复：Phase 1 处理消息流（通知去重 + 并发控制），Phase 2 处理资源安全（下载超时 + 错误反馈）。

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [ ] **Phase 1: 消息流修复** - 消除双消息通知，添加每用户并发任务限制
- [ ] **Phase 2: 下载可靠性** - 文件下载添加超时和明确的错误反馈

## Phase Details

### Phase 1: 消息流修复
**Goal**: Telegram 用户每次交互只收到一条干净的回复，同一用户有任务进行中时新请求被立即拒绝
**Depends on**: Nothing (first phase)
**Requirements**: NOTIF-01, NOTIF-02, CONC-01, CONC-02
**Success Criteria** (what must be TRUE):
  1. 用户发送一条消息后，对话结束时只收到一条消息（不再出现第二条"✅ 任务已完成"）
  2. 流式响应的最终完整内容出现在同一条消息中，不被截断或覆盖
  3. 用户在任务执行期间发送新消息，立即收到"⏳ 任务进行中，请稍后再试"提示
  4. 任务完成后，同一用户可以正常发送新消息并得到响应
**Plans**: TBD
**Key files**: `pkg/orchestrator/orchestrator.go`, `pkg/channel/telegram/telegram.go`, `pkg/channel/telegram/runner.go`

### Phase 2: 下载可靠性
**Goal**: 文件下载使用带超时的 HTTP 客户端，超时或失败时向用户返回明确错误提示
**Depends on**: Phase 1
**Requirements**: RELY-01, RELY-02
**Success Criteria** (what must be TRUE):
  1. Bot 下载 Telegram 文件时使用 30s 超时的 HTTP 客户端（不再使用默认 http.Get）
  2. 文件下载超时时，Bot 向用户发送可读的错误消息而非静默失败
  3. 文件下载网络错误时，Bot 向用户发送可读的错误消息
**Plans**: TBD
**Key files**: `pkg/channel/telegram/telegram.go`

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. 消息流修复 | 0/? | Not started | - |
| 2. 下载可靠性 | 0/? | Not started | - |
