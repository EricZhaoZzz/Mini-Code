# Requirements: Mini-Code Telegram Channel 修复

**Defined:** 2026-04-20
**Core Value:** Telegram Bot 用户每次交互只收到一条干净的回复，系统不会因并发滥用或慢网络造成资源泄漏。

## v1 Requirements

### Notification

- [ ] **NOTIF-01**: 回复结束时用户只收到一条消息（不再出现多余的"✅ 任务已完成"第二条消息）
- [ ] **NOTIF-02**: 流式更新的最终内容正确呈现在同一条消息中

### Concurrency

- [ ] **CONC-01**: 同一用户有任务正在执行时，新消息立即收到"⏳ 任务进行中，请稍后再试"提示
- [ ] **CONC-02**: 任务完成后，该用户可以正常发送新消息

### Reliability

- [ ] **RELY-01**: 下载 Telegram 文件时使用带 30s 超时的 HTTP 客户端
- [ ] **RELY-02**: 超时或下载失败时，Bot 向用户返回明确的错误提示（而非静默失败）

## v2 Requirements

### Rate Limiting

- **RATE-01**: 每用户每分钟请求频率上限（计数式限流）
- **RATE-02**: 全局并发任务上限（多用户同时使用时的总量控制）

### Observability

- **OBS-01**: 记录每次被限流拒绝的日志（user_id, timestamp）

## Out of Scope

| Feature | Reason |
|---------|--------|
| Discord / Slack 接入 | 本次聚焦 Telegram 修复 |
| CLI 频道改动 | 问题仅存在于 Telegram |
| 消息排队（任务等待队列）| 用户明确选择拒绝模式，不排队 |
| Channel 接口新增方法 | 不改动接口定义，仅修复实现 |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| NOTIF-01 | Phase 1 | Pending |
| NOTIF-02 | Phase 1 | Pending |
| CONC-01 | Phase 1 | Pending |
| CONC-02 | Phase 1 | Pending |
| RELY-01 | Phase 2 | Pending |
| RELY-02 | Phase 2 | Pending |

**Coverage:**
- v1 requirements: 6 total
- Mapped to phases: 6
- Unmapped: 0 ✓

---
*Requirements defined: 2026-04-20*
*Last updated: 2026-04-20 after roadmap creation*
