# Mini-Code Gateway 架构设计

## 概述

本文档描述了将 mini-code 改造为 Gateway + Worker 架构的详细设计，参考 OpenClaw 的架构模式。

### 设计目标

- **远程访问能力**：支持从远程客户端连接到运行在服务器上的 mini-code
- **多客户端并发**：支持多个客户端同时连接和执行任务
- **解耦架构**：将核心逻辑与接入层分离
- **插件化扩展**：支持通过 Go 插件机制扩展 Agent 和 Tool

### 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 改造方式 | 完全重写 | 彻底解耦，实现完整的 Gateway 架构 |
| 通信协议 | WebSocket RPC | 与 OpenClaw 一致，支持流式响应 |
| 认证机制 | Token 认证 | 简单可靠，易于管理 |
| 会话持久化 | SQLite | 轻量级，支持断线重连 |
| Agent 扩展 | Go 插件 | 动态加载，灵活扩展 |
| GW-Worker 协议 | gRPC | 高性能，强类型，支持双向流 |

## 整体架构

### 架构图

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              Gateway Process                              │
│  ┌────────────┐ ┌────────────┐ ┌────────────┐ ┌────────────┐            │
│  │   Auth     │ │  Session   │ │   Router   │ │  Config    │            │
│  │  Manager   │ │  Manager   │ │            │ │  Manager   │            │
│  └────────────┘ └────────────┘ └────────────┘ └────────────┘            │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │                     WebSocket RPC Server                          │   │
│  │                     (Client Facing)                               │   │
│  └──────────────────────────────────────────────────────────────────┘   │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │                     Worker Manager                                │   │
│  │            (Worker Registration & Load Balancing)                 │   │
│  └──────────────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────────────┘
         ▲                    ▲                    ▲
         │                    │                    │
    ┌────┴────┐          ┌────┴────┐          ┌────┴────┐
    │   CLI   │          │Telegram │          │  HTTP   │
    │ Client  │          │ Client  │          │  API    │
    └─────────┘          └─────────┘          └─────────┘

         │                    │                    │
         ▼                    ▼                    ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                              Worker Pool                                   │
│  ┌────────────────┐  ┌────────────────┐  ┌────────────────┐             │
│  │    Worker 1    │  │    Worker 2    │  │    Worker N    │             │
│  │ ┌────────────┐ │  │ ┌────────────┐ │  │ ┌────────────┐ │             │
│  │ │   Agent    │ │  │ │   Agent    │ │  │ │   Agent    │ │             │
│  │ │  Executor  │ │  │ │  Executor  │ │  │ │  Executor  │ │             │
│  │ ├────────────┤ │  │ ├────────────┤ │  │ ├────────────┤ │             │
│  │ │   Tool     │ │  │ │   Tool     │ │  │ │   Tool     │ │             │
│  │ │  Runner    │ │  │ │  Runner    │ │  │ │  Runner    │ │             │
│  │ ├────────────┤ │  │ ├────────────┤ │  │ ├────────────┤ │             │
│  │ │  Plugin    │ │  │ │  Plugin    │ │  │ │  Plugin    │ │             │
│  │ │  Manager   │ │  │ │  Manager   │ │  │ │  Manager   │ │             │
│  │ └────────────┘ │  │ └────────────┘ │  │ └────────────┘ │             │
│  └────────────────┘  └────────────────┘  └────────────────┘             │
└──────────────────────────────────────────────────────────────────────────┘
```

### 核心组件职责

| 组件 | 职责 |
|------|------|
| **Gateway** | |
| - WebSocket RPC Server | 接受客户端连接，处理 RPC 请求 |
| - Auth Manager | Token 验证、设备认证、权限管理 |
| - Session Manager | 会话生命周期管理、持久化 |
| - Router | 请求路由、负载均衡 |
| - Worker Manager | Worker 注册、健康检查、任务分发 |
| - Config Manager | 配置热更新 |
| **Worker** | |
| - Agent Executor | Agent 运行时、LLM 调用 |
| - Tool Runner | 工具执行、沙箱隔离 |
| - Plugin Manager | Go 插件加载、生命周期管理 |

### 数据流

```
用户输入 → Client → Gateway (WS RPC) → Worker Selection → Worker (Agent 执行)
                    ↑                                              ↓
                    └──────── 流式响应/事件 ←───────────────────────┘
```

## 通信协议

### 客户端-Gateway 协议（WebSocket RPC）

#### 协议帧格式

```go
// 请求帧
type RequestFrame struct {
    ID     string         `json:"id"`      // 请求 ID (UUID)
    Method string         `json:"method"`  // 方法名
    Params map[string]any `json:"params"`  // 参数
}

// 响应帧
type ResponseFrame struct {
    ID      string `json:"id"`       // 对应请求 ID
    Status  string `json:"status"`   // "ok" | "error"
    Result  any    `json:"result"`   // 成功结果
    Error   *Error `json:"error"`    // 错误信息
}

// 事件帧（流式响应）
type EventFrame struct {
    ID      string `json:"id"`       // 对应请求 ID
    Event   string `json:"event"`    // 事件类型
    Data    any    `json:"data"`     // 事件数据
    Seq     int    `json:"seq"`      // 序列号
    Final   bool   `json:"final"`    // 是否最后一帧
}

// 连接帧（握手）
type ConnectFrame struct {
    Token     string `json:"token"`      // 认证 Token
    ClientID  string `json:"clientId"`   // 客户端 ID
    ClientType string `json:"clientType"` // "cli" | "telegram" | "api"
    Version   string `json:"version"`    // 协议版本
}
```

#### 核心 RPC 方法

```go
// 会话管理
"session.create"    // 创建新会话
"session.get"       // 获取会话信息
"session.list"      // 列出所有会话
"session.delete"    // 删除会话
"session.restore"   // 恢复会话

// Agent 执行
"agent.run"         // 执行 Agent（流式响应）
"agent.abort"       // 中止执行

// 工具管理
"tools.list"        // 列出可用工具
"tools.execute"     // 直接执行工具

// 配置管理
"config.get"        // 获取配置
"config.update"     // 更新配置

// 系统管理
"system.status"     // 系统状态
"system.health"     // 健康检查
"worker.list"       // 列出 Worker
```

### Gateway-Worker 协议（gRPC）

#### Proto 定义

```protobuf
syntax = "proto3";
package gateway;

// Worker 服务 - Worker 暴露给 Gateway
service WorkerService {
    // 执行 Agent（双向流，支持请求取消和流式响应）
    rpc ExecuteAgent(stream AgentRequest) returns (stream AgentResponse);
    
    // 执行单个工具
    rpc ExecuteTool(ToolRequest) returns (ToolResponse);
    
    // 健康检查
    rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
    
    // 获取 Worker 能力
    rpc GetCapabilities(CapabilitiesRequest) returns (CapabilitiesResponse);
}

// Agent 请求
message AgentRequest {
    string request_id = 1;
    string session_id = 2;
    string agent_type = 3;          // "coder" | "reviewer" | "researcher" | "custom"
    repeated Message messages = 4;  // 对话历史
    map<string, string> metadata = 5;
    
    // 控制命令
    oneof control {
        StartAgent start = 10;
        AbortAgent abort = 11;
    }
}

message StartAgent {
    string prompt = 1;
    repeated string allowed_tools = 2;
    string model = 3;
}

message AbortAgent {}

// Agent 响应（流式）
message AgentResponse {
    string request_id = 1;
    string session_id = 2;
    
    oneof response {
        ContentDelta delta = 10;      // 文本增量
        ToolCallEvent tool_call = 11; // 工具调用
        ToolResultEvent tool_result = 12;
        AgentComplete complete = 13;  // 完成
        AgentError error = 14;        // 错误
    }
}

message ContentDelta {
    string text = 1;
    int32 sequence = 2;
}

message ToolCallEvent {
    string tool_id = 1;
    string tool_name = 2;
    string arguments = 3;  // JSON
}

message ToolResultEvent {
    string tool_id = 1;
    string result = 2;
    bool is_error = 3;
}

message AgentComplete {
    string final_message = 1;
    repeated Message new_messages = 2;
}

message AgentError {
    string code = 1;
    string message = 2;
}

// 工具请求/响应
message ToolRequest {
    string request_id = 1;
    string tool_name = 2;
    string arguments = 3;  // JSON
    string session_id = 4;
}

message ToolResponse {
    string request_id = 1;
    string result = 2;
    bool is_error = 3;
}

// 消息（兼容 OpenAI 格式）
message Message {
    string role = 1;       // "system" | "user" | "assistant" | "tool"
    string content = 2;
    string tool_call_id = 3;
    repeated ToolCall tool_calls = 4;
}

message ToolCall {
    string id = 1;
    string type = 2;
    string function_name = 3;
    string function_arguments = 4;
}

// 健康检查
message HealthCheckRequest {}
message HealthCheckResponse {
    string status = 1;  // "healthy" | "degraded" | "unhealthy"
    int32 active_tasks = 2;
    map<string, string> details = 3;
}

// 能力
message CapabilitiesRequest {}
message CapabilitiesResponse {
    repeated string agent_types = 1;
    repeated string tools = 2;
    repeated string models = 3;
    int32 max_concurrent_tasks = 4;
}
```

#### Worker 注册流程

```
┌─────────┐                    ┌──────────┐
│ Worker  │                    │ Gateway  │
└────┬────┘                    └────┬─────┘
     │                              │
     │  1. 健康检查 (HealthCheck)   │
     │ ─────────────────────────────►│
     │                              │
     │  2. 获取能力 (GetCapabilities)│
     │ ─────────────────────────────►│
     │                              │
     │  3. 注册请求 (gRPC stream)    │
     │ ─────────────────────────────►│
     │                              │
     │  4. 心跳 (双向流 KeepAlive)   │
     │ ◄─────────────────────────────►│
     │                              │
     │  5. 任务分配 (AgentRequest)   │
     │ ◄─────────────────────────────│
     │                              │
     │  6. 执行结果 (AgentResponse)  │
     │ ─────────────────────────────►│
```

## 认证与会话管理

### Token 认证机制

#### Token 类型

| Token 类型 | 用途 | 存储 |
|-----------|------|------|
| `GatewayToken` | Gateway 主令牌，管理权限 | 环境变量 / 配置文件 |
| `ClientToken` | 客户端访问令牌 | 由 Gateway 颁发 |
| `WorkerToken` | Worker 注册令牌 | 配置文件 |

#### 认证流程

```
┌─────────┐                    ┌──────────┐
│ Client  │                    │ Gateway  │
└────┬────┘                    └────┬─────┘
     │                              │
     │  1. Connect (WebSocket)      │
     │ ─────────────────────────────►│
     │                              │
     │  2. Send ConnectFrame        │
     │    {token, clientId, ...}    │
     │ ─────────────────────────────►│
     │                              │
     │  3. Validate Token           │
     │                              │
     │  4. HelloOk / Error          │
     │ ◄─────────────────────────────│
     │                              │
     │  5. Ready for RPC            │
```

#### Token 验证实现

```go
// pkg/gateway/auth/token.go
package auth

import (
    "crypto/subtle"
    "errors"
    "time"
)

type TokenType string

const (
    TokenTypeGateway TokenType = "gateway"
    TokenTypeClient  TokenType = "client"
    TokenTypeWorker  TokenType = "worker"
)

type TokenClaims struct {
    ID        string    `json:"id"`
    Type      TokenType `json:"type"`
    ClientID  string    `json:"clientId,omitempty"`
    Scopes    []string  `json:"scopes"`
    ExpiresAt time.Time `json:"expiresAt"`
    IssuedAt  time.Time `json:"issuedAt"`
}

type TokenValidator struct {
    gatewayToken string
    workerTokens map[string]bool
    clientTokens map[string]*TokenClaims
}

func (v *TokenValidator) Validate(token string) (*TokenClaims, error) {
    // 1. 检查是否是 Gateway Token（完全权限）
    if subtle.ConstantTimeCompare([]byte(token), []byte(v.gatewayToken)) == 1 {
        return &TokenClaims{
            Type:   TokenTypeGateway,
            Scopes: []string{"*"},
        }, nil
    }
    
    // 2. 检查是否是 Worker Token
    if v.workerTokens[token] {
        return &TokenClaims{
            Type:   TokenTypeWorker,
            Scopes: []string{"agent:*", "tool:*"},
        }, nil
    }
    
    // 3. 检查是否是 Client Token
    if claims, ok := v.clientTokens[token]; ok {
        if time.Now().After(claims.ExpiresAt) {
            return nil, errors.New("token expired")
        }
        return claims, nil
    }
    
    return nil, errors.New("invalid token")
}
```

### 会话管理

#### Session 结构

```go
// pkg/gateway/session/session.go
package session

import (
    "context"
    "sync"
    "time"
    
    "github.com/sashabaranov/go-openai"
)

type SessionState string

const (
    SessionStateActive   SessionState = "active"
    SessionStateIdle     SessionState = "idle"
    SessionStateArchived SessionState = "archived"
)

type Session struct {
    ID          string                        `json:"id"`
    ChannelID   string                        `json:"channelId"`
    UserID      string                        `json:"userId"`
    AgentType   string                        `json:"agentType"`
    State       SessionState                  `json:"state"`
    Messages    []openai.ChatCompletionMessage `json:"messages"`
    Metadata    map[string]any                `json:"metadata"`
    CreatedAt   time.Time                     `json:"createdAt"`
    UpdatedAt   time.Time                     `json:"updatedAt"`
    LastSeenAt  time.Time                     `json:"lastSeenAt"`
    
    // 运行时状态
    mu          sync.RWMutex
    cancelFunc  context.CancelFunc
    workerID    string
}

type SessionManager struct {
    store      SessionStore
    active     map[string]*Session
    mu         sync.RWMutex
}

// SessionStore 接口（支持多种存储后端）
type SessionStore interface {
    Create(ctx context.Context, session *Session) error
    Get(ctx context.Context, id string) (*Session, error)
    Update(ctx context.Context, session *Session) error
    Delete(ctx context.Context, id string) error
    List(ctx context.Context, filter SessionFilter) ([]*Session, error)
}
```

#### 会话持久化 Schema

```sql
-- pkg/gateway/session/schema.sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    agent_type TEXT DEFAULT 'coder',
    state TEXT DEFAULT 'active',
    messages TEXT,  -- JSON serialized
    metadata TEXT,  -- JSON serialized
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sessions_channel_user ON sessions(channel_id, user_id);
CREATE INDEX idx_sessions_state ON sessions(state);
CREATE INDEX idx_sessions_last_seen ON sessions(last_seen_at);
```

#### 会话生命周期

```
┌──────────────────────────────────────────────────────────────┐
│                      Session Lifecycle                        │
│                                                              │
│  ┌─────────┐     ┌─────────┐     ┌─────────┐     ┌────────┐│
│  │ Created │────►│ Active  │────►│  Idle   │────►│Archived││
│  └─────────┘     └────┬────┘     └────┬────┘     └────────┘│
│                       │               │                      │
│                       ▼               │                      │
│                  ┌─────────┐          │                      │
│                  │Aborted  │◄─────────┘                      │
│                  │(error)  │                                 │
│                  └─────────┘                                 │
└──────────────────────────────────────────────────────────────┘

状态转换触发器：
- Created → Active: 用户发送第一条消息
- Active → Idle: 任务完成，超过 5 分钟无活动
- Idle → Active: 用户发送新消息
- Idle → Archived: 超过 24 小时无活动
- Active → Aborted: 错误或用户取消
```

## 插件系统

### Go 插件机制概述

Go 的插件系统允许在运行时加载 `.so`（Linux/macOS）或 `.dll`（Windows）文件。

**限制：**
- 插件必须使用与主程序相同的 Go 版本编译
- 插件和主程序的依赖版本必须一致
- 无法卸载已加载的插件

### 插件接口定义

```go
// pkg/plugin/types.go
package plugin

import (
    "context"
    
    "github.com/sashabaranov/go-openai"
)

// AgentPlugin Agent 插件接口
type AgentPlugin interface {
    // Info 返回插件信息
    Info() PluginInfo
    
    // Initialize 初始化插件
    Initialize(ctx context.Context, config map[string]any) error
    
    // Shutdown 关闭插件
    Shutdown(ctx context.Context) error
    
    // Run 执行 Agent（核心方法）
    Run(ctx context.Context, req AgentRequest) (<-chan AgentEvent, error)
}

// ToolPlugin 工具插件接口
type ToolPlugin interface {
    // Info 返回工具信息
    Info() ToolInfo
    
    // Definition 返回工具的 JSON Schema 定义
    Definition() openai.Tool
    
    // Execute 执行工具
    Execute(ctx context.Context, args map[string]any) (string, error)
}

// PluginInfo 插件元信息
type PluginInfo struct {
    Name        string            `json:"name"`
    Version     string            `json:"version"`
    Description string            `json:"description"`
    Author      string            `json:"author"`
    Type        PluginType        `json:"type"`  // "agent" | "tool"
    Capabilities []string         `json:"capabilities"`
    Config      map[string]ConfigField `json:"config"`
}

// ToolInfo 工具元信息
type ToolInfo struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Category    string `json:"category"`  // "file" | "shell" | "git" | "custom"
    Dangerous   bool   `json:"dangerous"` // 是否需要确认
}

// ConfigField 配置字段定义
type ConfigField struct {
    Type        string `json:"type"`        // "string" | "int" | "bool"
    Required    bool   `json:"required"`
    Default     any    `json:"default"`
    Description string `json:"description"`
}

// AgentRequest Agent 执行请求
type AgentRequest struct {
    SessionID     string                        `json:"sessionId"`
    AgentType     string                        `json:"agentType"`
    Messages      []openai.ChatCompletionMessage `json:"messages"`
    Prompt        string                        `json:"prompt"`
    Model         string                        `json:"model"`
    AllowedTools  []string                      `json:"allowedTools"`
    Config        map[string]any                `json:"config"`
}

// AgentEvent Agent 执行事件（流式）
type AgentEvent struct {
    Type      EventType `json:"type"`
    Timestamp time.Time `json:"timestamp"`
    Data      any       `json:"data"`
}

type EventType string

const (
    EventTypeContent    EventType = "content"     // 文本内容增量
    EventTypeToolCall   EventType = "tool_call"   // 工具调用
    EventTypeToolResult EventType = "tool_result" // 工具结果
    EventTypeComplete   EventType = "complete"    // 执行完成
    EventTypeError      EventType = "error"       // 错误
)
```

### 插件管理器

```go
// pkg/plugin/manager.go
package plugin

import (
    "context"
    "fmt"
    "plugin"
    "sync"
    
    "go.uber.org/zap"
)

type Manager struct {
    pluginsDir string
    agents     map[string]AgentPlugin
    tools      map[string]ToolPlugin
    logger     *zap.Logger
    mu         sync.RWMutex
}

func NewManager(pluginsDir string, logger *zap.Logger) *Manager {
    return &Manager{
        pluginsDir: pluginsDir,
        agents:     make(map[string]AgentPlugin),
        tools:      make(map[string]ToolPlugin),
        logger:     logger,
    }
}

// LoadAll 加载目录下所有插件
func (m *Manager) LoadAll(ctx context.Context) error {
    entries, err := os.ReadDir(m.pluginsDir)
    if err != nil {
        return fmt.Errorf("read plugins directory: %w", err)
    }
    
    for _, entry := range entries {
        if entry.IsDir() {
            continue
        }
        
        name := entry.Name()
        if !isPluginFile(name) {
            continue
        }
        
        path := filepath.Join(m.pluginsDir, name)
        if err := m.Load(ctx, path); err != nil {
            m.logger.Error("failed to load plugin", 
                zap.String("path", path),
                zap.Error(err))
            continue
        }
    }
    
    return nil
}

// GetAgent 获取 Agent 插件
func (m *Manager) GetAgent(name string) (AgentPlugin, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    agent, ok := m.agents[name]
    return agent, ok
}

// GetTool 获取工具插件
func (m *Manager) GetTool(name string) (ToolPlugin, bool) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    tool, ok := m.tools[name]
    return tool, ok
}
```

### 插件示例

```go
// plugins/custom-agent/main.go
package main

import (
    "context"
    "fmt"
    
    "mini-code/pkg/plugin"
)

// CustomAgent 自定义 Agent 实现
type CustomAgent struct {
    config map[string]any
}

// Agent 导出符号（必须）
var Agent plugin.AgentPlugin = &CustomAgent{}

func (a *CustomAgent) Info() plugin.PluginInfo {
    return plugin.PluginInfo{
        Name:        "custom-analyzer",
        Version:     "1.0.0",
        Description: "代码分析 Agent",
        Author:      "Your Name",
        Type:        plugin.PluginTypeAgent,
        Capabilities: []string{"code_analysis", "refactoring"},
    }
}

func (a *CustomAgent) Initialize(ctx context.Context, config map[string]any) error {
    a.config = config
    return nil
}

func (a *CustomAgent) Shutdown(ctx context.Context) error {
    return nil
}

func (a *CustomAgent) Run(ctx context.Context, req plugin.AgentRequest) (<-chan plugin.AgentEvent, error) {
    events := make(chan plugin.AgentEvent, 100)
    
    go func() {
        defer close(events)
        
        // 实现自定义 Agent 逻辑
        events <- plugin.AgentEvent{
            Type: plugin.EventTypeContent,
            Data: map[string]string{"text": "开始分析..."},
        }
        
        // ... 执行分析
        
        events <- plugin.AgentEvent{
            Type: plugin.EventTypeComplete,
            Data: map[string]string{"message": "分析完成"},
        }
    }()
    
    return events, nil
}

func main() {
    // 插件不需要 main 函数，但需要编译为 .so/.dll
    fmt.Println("This is a plugin, compile with: go build -buildmode=plugin")
}
```

### 插件编译与部署

```bash
# 编译插件（Linux/macOS）
go build -buildmode=plugin -o plugins/custom-agent.so plugins/custom-agent/main.go

# 编译插件（Windows）
go build -buildmode=plugin -o plugins/custom-agent.dll plugins/custom-agent/main.go
```

## Worker 核心组件

### Worker 架构

```
┌─────────────────────────────────────────────────────────────┐
│                         Worker                               │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                     gRPC Server                           ││
│  │            (WorkerService Implementation)                ││
│  └─────────────────────────────────────────────────────────┘│
│                            │                                 │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                    Agent Executor                         ││
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌──────────┐   ││
│  │  │ Coder   │  │Reviewer │  │Researcher│  │  Plugin  │   ││
│  │  │ Agent   │  │ Agent   │  │  Agent   │  │  Agents  │   ││
│  │  └─────────┘  └─────────┘  └─────────┘  └──────────┘   ││
│  └─────────────────────────────────────────────────────────┘│
│                            │                                 │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                     Tool Runner                           ││
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌──────────┐   ││
│  │  │  File   │  │  Shell  │  │   Git   │  │  Plugin  │   ││
│  │  │  Tools  │  │  Tools  │  │  Tools  │  │  Tools   │   ││
│  │  └─────────┘  └─────────┘  └─────────┘  └──────────┘   ││
│  └─────────────────────────────────────────────────────────┘│
│                            │                                 │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                    Plugin Manager                         ││
│  └─────────────────────────────────────────────────────────┘│
│                            │                                 │
│  ┌─────────────────────────────────────────────────────────┐│
│  │                      Provider                             ││
│  │           (LLM API Client - OpenAI Compatible)           ││
│  └─────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────┘
```

### Worker 核心代码

```go
// cmd/worker/main.go
package main

import (
    "context"
    "log"
    "net"
    "os"
    "os/signal"
    "syscall"
    
    "mini-code/pkg/worker"
    "mini-code/pkg/config"
    "go.uber.org/zap"
)

func main() {
    // 加载配置
    cfg, err := config.Load()
    if err != nil {
        log.Fatalf("load config: %v", err)
    }
    
    // 初始化日志
    logger, _ := zap.NewProduction()
    defer logger.Sync()
    
    // 创建 Worker
    w, err := worker.New(cfg, logger)
    if err != nil {
        log.Fatalf("create worker: %v", err)
    }
    
    // 启动 gRPC 服务器
    lis, err := net.Listen("tcp", cfg.Worker.Address)
    if err != nil {
        log.Fatalf("listen: %v", err)
    }
    
    go func() {
        if err := w.Serve(lis); err != nil {
            log.Fatalf("serve: %v", err)
        }
    }()
    
    logger.Info("worker started", 
        zap.String("address", cfg.Worker.Address))
    
    // 等待信号
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    <-sigCh
    
    // 优雅关闭
    w.GracefulStop()
    logger.Info("worker stopped")
}
```

```go
// pkg/worker/worker.go
package worker

import (
    "context"
    "sync"
    
    "github.com/sashabaranov/go-openai"
    "go.uber.org/zap"
    "google.golang.org/grpc"
    
    "mini-code/pkg/config"
    "mini-code/pkg/plugin"
    pb "mini-code/pkg/proto"
)

type Worker struct {
    pb.UnimplementedWorkerServiceServer
    
    cfg        *config.Config
    logger     *zap.Logger
    provider   *openai.Client
    pluginMgr  *plugin.Manager
    
    // 任务管理
    tasks      map[string]*Task
    tasksMu    sync.RWMutex
    
    grpcServer *grpc.Server
}

type Task struct {
    ID        string
    SessionID string
    Cancel    context.CancelFunc
    Events    chan *pb.AgentResponse
}

func New(cfg *config.Config, logger *zap.Logger) (*Worker, error) {
    // 初始化 Provider
    provider := openai.NewClient(cfg.APIKey)
    
    // 初始化插件管理器
    pluginMgr := plugin.NewManager(cfg.Plugins.Dir, logger)
    if err := pluginMgr.LoadAll(context.Background()); err != nil {
        logger.Warn("failed to load some plugins", zap.Error(err))
    }
    
    w := &Worker{
        cfg:       cfg,
        logger:    logger,
        provider:  provider,
        pluginMgr: pluginMgr,
        tasks:     make(map[string]*Task),
    }
    
    // 创建 gRPC 服务器
    w.grpcServer = grpc.NewServer()
    pb.RegisterWorkerServiceServer(w.grpcServer, w)
    
    return w, nil
}

func (w *Worker) Serve(lis net.Listener) error {
    return w.grpcServer.Serve(lis)
}

func (w *Worker) GracefulStop() {
    w.grpcServer.GracefulStop()
}
```

### Agent 执行器

```go
// pkg/worker/executor.go
package worker

import (
    "context"
    
    pb "mini-code/pkg/proto"
)

// ExecuteAgent 实现 gRPC 服务
func (w *Worker) ExecuteAgent(stream pb.WorkerService_ExecuteAgentServer) error {
    // 接收第一个请求
    req, err := stream.Recv()
    if err != nil {
        return err
    }
    
    // 创建任务上下文
    ctx, cancel := context.WithCancel(stream.Context())
    defer cancel()
    
    task := &Task{
        ID:        req.RequestId,
        SessionID: req.SessionId,
        Cancel:    cancel,
        Events:    make(chan *pb.AgentResponse, 100),
    }
    
    w.tasksMu.Lock()
    w.tasks[task.ID] = task
    w.tasksMu.Unlock()
    
    defer func() {
        w.tasksMu.Lock()
        delete(w.tasks, task.ID)
        w.tasksMu.Unlock()
    }()
    
    // 选择 Agent
    agent := w.selectAgent(req.AgentType)
    
    // 执行 Agent
    go w.runAgent(ctx, agent, req, task.Events)
    
    // 流式发送响应
    for {
        select {
        case event := <-task.Events:
            if err := stream.Send(event); err != nil {
                return err
            }
            if event.GetComplete() != nil || event.GetError() != nil {
                return nil
            }
        case <-ctx.Done():
            return ctx.Err()
        }
    }
}

func (w *Worker) selectAgent(agentType string) Agent {
    // 优先查找插件 Agent
    if pluginAgent, ok := w.pluginMgr.GetAgent(agentType); ok {
        return &PluginAgentWrapper{plugin: pluginAgent}
    }
    
    // 内置 Agent
    switch agentType {
    case "coder":
        return NewCoderAgent(w.provider, w.cfg.Model)
    case "reviewer":
        return NewReviewerAgent(w.provider, w.cfg.Model)
    case "researcher":
        return NewResearcherAgent(w.provider, w.cfg.Model)
    default:
        return NewBaseAgent(w.provider, w.cfg.Model, nil)
    }
}
```

## 项目结构

### 目录结构

```
mini-code/
├── cmd/                          # 命令行入口
│   ├── gateway/                  # Gateway 服务入口
│   │   └── main.go
│   ├── worker/                   # Worker 服务入口
│   │   └── main.go
│   └── cli/                      # CLI 客户端入口
│       └── main.go
│
├── pkg/                          # 公共包
│   ├── gateway/                  # Gateway 核心实现
│   │   ├── server.go             # WebSocket 服务器
│   │   ├── router.go             # 请求路由
│   │   ├── auth/                 # 认证模块
│   │   │   ├── token.go
│   │   │   ├── validator.go
│   │   │   └── device.go
│   │   ├── session/              # 会话管理
│   │   │   ├── session.go
│   │   │   ├── manager.go
│   │   │   └── store.go
│   │   ├── worker/               # Worker 管理
│   │   │   ├── registry.go
│   │   │   ├── loadbalancer.go
│   │   │   └── health.go
│   │   └── protocol/             # 协议定义
│   │       ├── frame.go
│   │       ├── methods.go
│   │       └── errors.go
│   │
│   ├── worker/                   # Worker 核心实现
│   │   ├── worker.go             # Worker 主结构
│   │   ├── executor.go           # Agent 执行器
│   │   ├── tools.go              # 工具执行器
│   │   └── config.go             # Worker 配置
│   │
│   ├── agent/                    # Agent 实现（复用现有）
│   │   ├── agent.go              # 接口定义
│   │   ├── base.go               # 基础 Agent
│   │   ├── coder.go              # Coder Agent
│   │   ├── reviewer.go           # Reviewer Agent
│   │   └── researcher.go         # Researcher Agent
│   │
│   ├── tools/                    # 工具实现（复用现有）
│   │   ├── registry.go
│   │   ├── file.go
│   │   ├── shell.go
│   │   ├── git.go
│   │   └── ...
│   │
│   ├── plugin/                   # 插件系统
│   │   ├── types.go              # 接口定义
│   │   ├── manager.go            # 插件管理器
│   │   └── loader.go             # 插件加载器
│   │
│   ├── provider/                 # LLM Provider（复用现有）
│   │   ├── interface.go
│   │   └── openai.go
│   │
│   ├── client/                   # 客户端库
│   │   ├── gateway.go            # Gateway 客户端
│   │   └── stream.go             # 流式处理
│   │
│   └── config/                   # 配置管理
│       ├── config.go
│       └── loader.go
│
├── proto/                        # Protocol Buffers
│   ├── worker.proto              # Worker 服务定义
│   └── worker.pb.go              # 生成的 Go 代码
│
├── plugins/                      # 插件目录（示例）
│   └── custom-agent/
│       └── main.go
│
├── migrations/                   # 数据库迁移
│   └── 001_init.sql
│
├── configs/                      # 配置文件示例
│   ├── gateway.yaml
│   └── worker.yaml
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

### 核心依赖

```go
// go.mod
module mini-code

go 1.23

require (
    github.com/sashabaranov/go-openai v1.32.0
    go.uber.org/zap v1.27.0
    google.golang.org/grpc v1.62.0
    google.golang.org/protobuf v1.33.0
    github.com/gorilla/websocket v1.5.1
    github.com/mattn/go-sqlite3 v1.14.22
    gopkg.in/yaml.v3 v3.0.1
)
```

### Makefile

```makefile
.PHONY: all build gateway worker cli test proto clean

all: build

# 构建所有组件
build: gateway worker cli

# 构建 Gateway
gateway:
	go build -o bin/gateway ./cmd/gateway

# 构建 Worker
worker:
	go build -o bin/worker ./cmd/worker

# 构建 CLI
cli:
	go build -o bin/mini-code ./cmd/cli

# 生成 Protocol Buffers
proto:
	protoc --go_out=. --go-grpc_out=. proto/worker.proto

# 运行测试
test:
	go test -v ./...

# 清理
clean:
	rm -rf bin/

# 构建插件（示例）
plugin:
	go build -buildmode=plugin -o plugins/custom-agent.so ./plugins/custom-agent

# 运行开发环境
dev:
	./bin/gateway &
	./bin/worker &
	./bin/mini-code
```

### 配置文件示例

```yaml
# configs/gateway.yaml
server:
  address: ":18789"
  bind: "loopback"  # loopback | lan | tailnet

auth:
  mode: "token"
  token: "${GATEWAY_TOKEN}"

session:
  store: "sqlite"
  database: "./data/sessions.db"
  expiration: "24h"

worker:
  registration:
    enabled: true
    token: "${WORKER_TOKEN}"
  health_check:
    interval: "30s"
    timeout: "10s"

logging:
  level: "info"
  format: "json"
```

```yaml
# configs/worker.yaml
worker:
  id: "worker-1"
  address: ":18790"
  max_concurrent: 10
  workspace: "./workspace"

gateway:
  address: "127.0.0.1:18789"
  token: "${WORKER_TOKEN}"

api:
  key: "${API_KEY}"
  base_url: "https://api.openai.com/v1"
  model: "gpt-4"

plugins:
  dir: "./plugins"
  enabled: true

logging:
  level: "info"
```

## 设计总结

| 模块 | 说明 |
|------|------|
| **整体架构** | Gateway + Worker 分离，支持水平扩展 |
| **通信协议** | Client-Gateway: WebSocket RPC, Gateway-Worker: gRPC |
| **认证机制** | Token 认证，支持 Gateway/Client/Worker 三种令牌 |
| **会话管理** | SQLite 持久化，支持状态转换和生命周期管理 |
| **插件系统** | Go plugin 机制，支持 Agent 和 Tool 扩展 |
| **Worker 组件** | Agent 执行器、工具执行器、插件管理器 |
| **项目结构** | 清晰的目录组织，复用现有代码 |