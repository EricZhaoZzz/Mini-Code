# 设计：将 web_search 后端替换为 Exa 免费搜索

- 日期：2026-06-02
- 状态：已确认，待实现
- 涉及包：`pkg/tools`、`cmd/agent`、`cmd/telegram`

## 背景与动机

项目已存在 `web_search` 工具（`pkg/tools/web_search.go`），但它只通过公司内部 **PIE 网关**（Serper 接口 `/v2/extend/web/serper/search`，HMAC-SHA256 签名）实现。其配置仅在 `cmd/agent/main.go` / `cmd/telegram/main.go` 检测到 `PIE_APP_ID / PIE_APP_SECRET / PIE_GATEWAY_PATH` 时通过 `SetWebSearchConfig` 注入。

结果：**走标准 OpenAI 兼容路径（`API_KEY / BASE_URL / MODEL`）的环境，`web_search` 永远未配置，调用即返回"未配置"错误，网络搜索实际不可用。**

决策：**移除 PIE 搜索后端，把 `web_search` 改为一个免费、零配置的 Exa 后端**。这样所有环境（含标准 OpenAI 路径）开箱即用，并去掉只在公司网关下才生效的"死路径"。

注意：PIE 网关在项目中还承担 **LLM provider** 身份（`pkg/provider` 的 `PieGatewayProvider`）。本次**仅移除 web search 的 PIE 后端**，PIE 作为大模型通道保持不变。

## 调研结论（Cherry Studio 的免费搜索）

Cherry Studio 的 web search provider 分三类（`src/main/services/webSearch/providers/`）：

- `api/`：需 key 的接口（Tavily、Exa `api.exa.ai/search`、Bocha、Searxng、Zhipu 等）。
- `mcp/ExaMcpProvider.ts`：**免费、无需 key** 的 Exa MCP 端点。
- 自托管 SearXNG（需自己用 Docker 部署）。

关键发现：`ExaMcpProvider` 虽名为 "MCP"，**并非传统 stdio 长连接，而是一次性 HTTP POST**，因此无需在 Go 侧引入 MCP 客户端子系统，与现有"工具直连 HTTP"的模式一致。

实测确认（`POST https://mcp.exa.ai/mcp`，无任何鉴权头）可正常返回结果。

| 项 | 内容 |
|---|---|
| 端点 | `POST https://mcp.exa.ai/mcp`（默认，可覆盖） |
| 鉴权 | 无 |
| 请求头 | `accept: application/json, text/event-stream`、`content-type: application/json` |
| 请求体 | JSON-RPC 2.0：`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"web_search_exa","arguments":{"query":"...","type":"auto","numResults":N,"livecrawl":"fallback"}}}` |
| 响应 | SSE 流：`data: {...}` 行 → `result.content[].text`；文本按 `\n\n` 分块，每块含 `Title:` / `URL:` / `Highlights:`(或 `Text:`) |

注意：实测真实正文字段为 `Highlights:`，而 Cherry Studio 原版解析器只认 `Text:` 会漏正文。本设计的解析器须更鲁棒——优先按 `Highlights:`/`Text:` 标记定位正文，捕获其后所有行；同时跳过 `Published:` / `Author:` 等元信息行。

## 架构

只有一个后端（Exa），**不引入后端接口抽象**（YAGNI）。`web_search.go` 直接实现：

```
WebSearch(args)
  → 解析 WebSearchArguments（query / max_results；language/country 保留但忽略）
  → 夹取 max_results（默认 3、上限 10）
  → 构造 JSON-RPC body（web_search_exa, type=auto, numResults, livecrawl=fallback）
  → POST exaEndpoint()（默认 https://mcp.exa.ai/mcp，可被 EXA_MCP_URL 覆盖）
  → parseExaResponse：解析 SSE → parseExaChunks → []SearchHit
  → 组装为现有 webSearchResult 返回
```

### 对外契约保持不变

`WebSearchArguments`（`query` / `max_results` / `language` / `country`）与返回结构 `webSearchResult`（`Title` / `Link` / `Snippet` / `Content`）均不变。因此 `registry.go` 注册、`pkg/ui/tools.go` 展示、`pkg/agent/researcher.go` 白名单**均无需改动**。

`language` / `country` 对 Exa 无意义，字段保留以维持工具 schema 与历史兼容，但实现中忽略。

## 移除 PIE 搜索

- 删除 `web_search.go` 内的 PIE/Serper 逻辑、`WebSearchConfig`、`SetWebSearchConfig`、`globalWebSearchConfig`，以及对 `provider.GenerateNonce` / `provider.ComputeHMACSignature` 的调用。
- 删除 `cmd/agent/main.go`（约 320-324 行）与 `cmd/telegram/main.go`（约 45 行）中的 `tools.SetWebSearchConfig(...)` 调用；**保留**这两处的 PIE LLM provider（`PieGatewayProvider`）初始化。
- `pkg/provider` 中的 `GenerateNonce` / `ComputeHMACSignature` 保留（仍由 PIE LLM provider 使用）。

## 配置

- `EXA_MCP_URL`（可选）：覆盖默认端点，默认 `https://mcp.exa.ai/mcp`。在 tools 包内通过 `os.Getenv` 直接读取；两个入口启动时均已 `loadDotEnv(".env")`，故无需在 `main.go` 接线。
- 无需 API key，无需其他环境变量。

## Exa 数据流与解析要点

```
query
  → JSON-RPC body (name=web_search_exa, type=auto, numResults=max_results, livecrawl=fallback)
  → POST exaEndpoint() (accept: text/event-stream)
  → 读取全部响应体；逐行取 data: 行 → JSON result.content[].text
  → join 后按 \n\n 分块；每块解析 Title: / URL:
  → 正文：定位 Highlights:/Text: 标记行，取其后所有行；无标记时取 URL 行之后内容
  → 跳过 Published:/Author: 等元信息（通过标记定位天然跳过）
  → SearchHit{Title, Link, Content}；content 截断 2000 字（复用 truncate）
```

- `numResults` 取夹取后的 `max_results`（默认 3、上限 10）。
- 超时 30s（`context.WithTimeout`）。
- 空响应或全部解析失败 → 返回明确中文错误。
- HTTP 非 200 → 返回含状态码与截断 body 的中文错误。

## 测试（TDD 先行）

- 夹具 `pkg/tools/testdata/exa_mcp_response.txt`：一份真实形态的 SSE 响应（两条结果，正文用 `Highlights:`，含 `Published:`/`Author:` 元信息行以验证被正确跳过）。
- 替换现有 `web_search_test.go`（原 PIE 测试全部移除）为 Exa 测试：
  - `parseExaResponse` 解析夹具：2 条结果、Title/URL/Content 正确、`\n\n` 分块、`Highlights:` 多行正文被捕获、`Published:`/`Author:` 不混入正文。
  - `numResults` 截断（maxResults=1 → 1 条）。
  - 空响应 → 错误。
  - `WebSearch` 端到端（`httptest.Server` + `EXA_MCP_URL` 指向它）：请求体 `name=web_search_exa`、`numResults` 正确；返回 `webSearchResult` 结构正确。
  - HTTP 500 → 错误。
  - `TestWebSearch_ToolRegistered` 保留（验证工具仍注册、schema 含 query/max_results）。

## 收尾

- `.env.example`：移除 PIE 作为搜索配置的说明（如有），新增 `EXA_MCP_URL` 注释；PIE 作为 LLM provider 的变量说明保留。
- `README.md` 与 `CLAUDE.md`：工具/环境变量段落更新为"Exa 免费网络搜索（零配置）"。

## 不做（YAGNI）

- 不引入 `SearchBackend` 接口或多后端选择（只剩 Exa 一个后端）。
- 不引入通用 MCP 客户端子系统（Exa 端点只需单次 HTTP POST）。
- 不新增 Tavily / Brave / SearXNG 等其他后端。
- 不改动 `WebSearchArguments` 与返回结构、不改 `registry.go` / `ui/tools.go` / `researcher.go`。
- 不移除 PIE 的 LLM provider 身份。
