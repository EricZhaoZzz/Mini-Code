# 设计：为 web_search 增加 Exa 免费搜索后端

- 日期：2026-06-02
- 状态：已确认，待实现
- 涉及包：`pkg/tools`、`cmd/agent`、`cmd/telegram`（配置接线）

## 背景与动机

项目已存在 `web_search` 工具（`pkg/tools/web_search.go`），但它只通过公司内部 **PIE 网关**（Serper 接口 `/v2/extend/web/serper/search`，HMAC-SHA256 签名）实现。其配置仅在 `cmd/agent/main.go` 检测到 `PIE_APP_ID / PIE_APP_SECRET / PIE_GATEWAY_PATH` 时通过 `SetWebSearchConfig` 注入。

结果：**走标准 OpenAI 兼容路径（`API_KEY / BASE_URL / MODEL`）的环境，`web_search` 永远未配置，调用即返回"未配置"错误，网络搜索实际不可用。**

目标：新增一个 **免费、零配置** 的搜索后端补上这个缺口，与现有 PIE 后端共存。

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

注意：实测真实正文字段为 `Highlights:`，而 Cherry Studio 原版解析器只认 `Text:` 会漏正文。本设计的解析器须更鲁棒——将 `URL:` 行之后的内容整体作为正文。

## 架构

引入 `SearchBackend` 接口，解耦"如何搜"与"工具如何被调用"：

```go
type SearchBackend interface {
    Search(ctx context.Context, q SearchQuery) ([]SearchHit, error)
    Name() string
}
```

- `pieBackend`：搬入现有 PIE/Serper 逻辑，行为不变。
- `exaBackend`（新增）：POST Exa MCP 端点并解析 SSE，零配置、免费。
- `WebSearch` 工具函数职责：解析参数 → 选 backend → 调 `Search` → 组装为现有 `webSearchResult` 返回。

### 对外契约保持不变

`WebSearchArguments`（`query` / `max_results` / `language` / `country`）与返回结构 `webSearchResult`（`Title` / `Link` / `Snippet` / `Content`）均不变。因此 `registry.go` 注册、`pkg/ui/tools.go` 展示、`pkg/agent/researcher.go` 白名单**均无需改动**。

## 后端选择逻辑

由环境变量 `WEB_SEARCH_BACKEND` 决定，带智能默认：

| `WEB_SEARCH_BACKEND` | 行为 |
|---|---|
| 未设置（默认） | 使用 Exa 免费后端（零配置可用，补上标准 OpenAI 路径缺口） |
| `exa` | 强制 Exa 免费后端 |
| `pie` | 使用 PIE 后端（需 `PIE_APP_ID/SECRET/GATEWAY_PATH`，缺失则返回中文错误） |

- `EXA_MCP_URL`（可选）：覆盖默认端点，默认 `https://mcp.exa.ai/mcp`。
- 兼容性：公司环境设 `WEB_SEARCH_BACKEND=pie` 即维持原行为；其他环境零配置即可用 Exa。
- 现有 `SetWebSearchConfig`（PIE 配置注入）保留；新增对 Exa 端点/选择的读取在 worker 启动处接线（`cmd/agent`、`cmd/telegram`）。

## Exa 后端数据流

```
query
  → JSON-RPC body (name=web_search_exa, type=auto, numResults=max_results, livecrawl=fallback)
  → POST mcp.exa.ai/mcp (accept: text/event-stream)
  → 逐行解析 SSE，取 data: 行 → result.content[].text
  → 按 \n\n 分块；每块解析 Title: / URL:，URL 之后内容整体作为正文
  → []SearchHit → webSearchResult
```

实现要点：

- 正文解析鲁棒：不仅认 `Text:`，将 `URL:` 行之后的全部内容作为 content。
- content 截断 2000 字，复用现有 `truncate`。
- `language` / `country` 对 Exa 无意义 → 忽略（仅 PIE 使用）。
- `max_results` 映射到 `numResults`，默认 3、上限 10，复用现有夹取逻辑。
- 超时 30s，复用现有 `context.WithTimeout` 写法。
- 空结果或全部解析失败时返回明确中文错误，便于排查。

## 测试（TDD 先行）

- 夹具 `pkg/tools/testdata/exa_mcp_response.txt`：录一份真实 SSE 响应（仿 Cherry Studio fixtures 做法）。
- `exa_backend_test.go`：用 `httptest.Server` 回放夹具，断言：
  - Title / URL / Content 正确解析；
  - `\n\n` 分块正确；
  - `Highlights:` 正文被捕获（不漏）；
  - `numResults` / `max_results` 截断（默认 3、上限 10）。
- backend 选择逻辑单测：`WEB_SEARCH_BACKEND` 各取值 → 选对 backend；`pie` 缺配置 → 中文错误。
- 现有 `web_search_test.go` 保持通过（PIE 行为不回归）。

## 收尾

- `.env.example`：新增 `WEB_SEARCH_BACKEND`、`EXA_MCP_URL` 注释说明。
- `README.md` 与 `CLAUDE.md`：工具/环境变量段落补充 Exa 免费搜索说明。

## 不做（YAGNI）

- 不引入通用 MCP 客户端子系统（Exa 端点只需单次 HTTP POST）。
- 不新增 Tavily / Brave / SearXNG 等其他后端（本次仅 Exa）。
- 不改动 `WebSearchArguments` 与返回结构。
- 不移除 PIE（web search 与 LLM provider 两处 PIE 均保留）。
