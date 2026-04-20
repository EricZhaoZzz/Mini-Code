# Mini-Code Telegram Channel 修复

## What This Is

Mini-Code 的 Telegram Bot 频道存在三个影响可用性的已知问题：双消息通知噪声、无并发任务限制导致 API 滥用风险、文件下载无超时导致 goroutine 泄漏。本项目针对性修复这三个问题，提升 Telegram 频道的可靠性和用户体验。

## Core Value

Telegram Bot 用户每次交互只收到一条干净的回复，系统不会因并发滥用或慢网络造成资源泄漏。

## Requirements

### Validated

- ✓ Telegram Bot 基本消息收发（Send / EditMessage）— existing
- ✓ 流式回复（每 1.5s EditMessage 刷新）— existing
- ✓ 用户白名单过滤（TELEGRAM_ALLOWED_USERS）— existing
- ✓ 文件附件接收与下载 — existing
- ✓ Bot 命令系统（/start, /help, /memory, /status）— existing

### Active

- [ ] 修复双消息通知：回复结束时只发一条消息，移除多余的"✅ 任务已完成" NotifyDone
- [ ] 每用户并发任务限制：同一用户有任务在执行时，新消息立即返回"任务进行中"提示，拒绝入队
- [ ] 文件下载超时：`downloadFile` 改用带 30s 超时的 `http.Client`，防止慢响应永久阻塞

### Out of Scope

- 速率限制（每分钟 N 条）— 用户明确选择"每用户同时 1 个任务"模式，不做频率计数
- Discord / Slack 等新频道 — 本次聚焦 Telegram 修复
- CLI 频道改动 — 问题仅存在于 Telegram

## Context

代码库已有完整的 channel 抽象层（`pkg/channel/types.go`）。三个问题均在 `pkg/channel/telegram/` 内：

- 双消息：`orchestrator.go` 的 `handleTelegram` 在 `done=true` 时调用 `EditMessage` 后还额外调用 `NotifyDone`
- 无并发限制：`telegram.go` 只有 10 条消息缓冲，无任务状态跟踪
- 无超时：`telegram.go` `downloadFile` 使用默认 `http.Get`

## Constraints

- **Tech stack**: Go，不引入新的外部依赖
- **Scope**: 仅修改 `pkg/channel/telegram/` 和 `pkg/orchestrator/`，不改动 Channel 接口定义
- **Compatibility**: 修复后 CLI 频道行为不受影响

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| 每用户 1 并发任务（拒绝模式）| 用户明确选择，排队模式复杂度更高 | — Pending |
| 不引入新依赖 | 项目已有足够的原语（sync.Map / channel）实现限流 | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-20 after initialization*
