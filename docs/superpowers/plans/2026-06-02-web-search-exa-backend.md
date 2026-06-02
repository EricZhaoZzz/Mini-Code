# Web Search Exa 后端 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `web_search` 工具的后端从公司内部 PIE 网关替换为免费、零配置的 Exa MCP HTTP 端点，使所有运行环境（含标准 OpenAI 路径）开箱即用。

**Architecture:** 只有一个后端（Exa），不引入接口抽象。`web_search.go` 直接构造 JSON-RPC 请求体 POST 到 `https://mcp.exa.ai/mcp`（可经 `EXA_MCP_URL` 覆盖），解析 SSE 响应中的 `Title:/URL:/Highlights:` 文本块为结果。对外的工具参数 schema 与返回结构保持不变，故 registry / ui / researcher 均不动。任务顺序保证每个提交都能整库编译。

**Tech Stack:** Go，标准库 `net/http` / `encoding/json` / `context`；测试用 `net/http/httptest` 与 `testdata` 夹具。

---

## 文件结构

- 修改：`cmd/agent/main.go` —— 删除 `tools.SetWebSearchConfig(...)` 调用（保留 PIE LLM provider 初始化）。
- 修改：`cmd/telegram/main.go` —— 删除 `tools.SetWebSearchConfig(...)` 调用（保留 PIE LLM provider 初始化）。
- 重写：`pkg/tools/web_search.go` —— 移除 PIE/Serper 逻辑与 `WebSearchConfig`/`SetWebSearchConfig`/`globalWebSearchConfig`，改为 Exa 实现。
- 新建：`pkg/tools/testdata/exa_mcp_response.txt` —— SSE 响应夹具。
- 重写：`pkg/tools/web_search_test.go` —— 移除 PIE 测试，改为 Exa 测试。
- 修改：`.env.example`、`README.md`、`CLAUDE.md` —— 文档更新。

不变：`pkg/tools/registry.go`、`pkg/ui/tools.go`、`pkg/agent/researcher.go`、`pkg/provider/*`（`GenerateNonce`/`ComputeHMACSignature` 仍由 PIE LLM provider 使用）。

**任务顺序理由：** 先删入口对 `SetWebSearchConfig` 的调用（此时该函数在 `web_search.go` 中仍定义，整库可编译、旧测试仍绿）；再重写 `web_search.go` 删除这些符号（此时已无任何引用），从而每个提交都能编译。

---

## Task 1: 移除入口处的 PIE 搜索接线

**Files:**
- Modify: `cmd/agent/main.go`（删除 `tools.SetWebSearchConfig(...)` 调用，约 320-324 行）
- Modify: `cmd/telegram/main.go`（删除 `tools.SetWebSearchConfig(...)` 调用，约 45 行）

- [ ] **Step 1: 删除 `cmd/agent/main.go` 中的 SetWebSearchConfig 调用**

定位并删除以下代码块（位于 `if pieAppID != "" && pieAppSecret != "" && pieGatewayPath != ""` 分支内、`p = provider.NewPieGatewayProvider(...)` 之后）：

```go
		tools.SetWebSearchConfig(&tools.WebSearchConfig{
			AppID:       pieAppID,
			AppSecret:   pieAppSecret,
			GatewayPath: pieGatewayPath,
		})
```

保留同分支内的 `p = provider.NewPieGatewayProvider(...)`（PIE 仍是 LLM provider）。

- [ ] **Step 2: 删除 `cmd/telegram/main.go` 中的 SetWebSearchConfig 调用**

定位并删除以下代码块（约第 45 行起）：

```go
		tools.SetWebSearchConfig(&tools.WebSearchConfig{
			AppID:       pieAppID,
			AppSecret:   pieAppSecret,
			GatewayPath: pieGatewayPath,
		})
```

保留该分支内的 PIE LLM provider 初始化。

- [ ] **Step 3: 检查 `tools` 包导入是否仍被使用**

确认删除后这两个文件里 `mini-code/pkg/tools` 仍有其它引用；若某文件已不再引用 `tools`，删除其 `import` 中的 `"mini-code/pkg/tools"` 行以避免 “imported and not used”。

Run: `go build ./...`
Expected: PASS（`SetWebSearchConfig` 此时仍定义于 `web_search.go`，只是变为未被调用——Go 允许未使用的导出函数；旧 PIE 实现仍在，整库可编译）。

- [ ] **Step 4: 运行测试**

Run: `go test ./...`
Expected: PASS（现有 `web_search_test.go` 的 PIE 测试仍通过，因为 `web_search.go` 尚未改动）。

- [ ] **Step 5: Commit**

```bash
git add cmd/agent/main.go cmd/telegram/main.go
git commit -m "refactor: remove PIE web_search wiring from CLI and telegram entrypoints"
```

---

## Task 2: 用 Exa 实现重写 web_search.go 及测试

**Files:**
- Create: `pkg/tools/testdata/exa_mcp_response.txt`
- Modify (重写): `pkg/tools/web_search.go`
- Modify (重写): `pkg/tools/web_search_test.go`

- [ ] **Step 1: 创建 SSE 响应夹具**

创建 `pkg/tools/testdata/exa_mcp_response.txt`，内容如下（注意：只有一行 `data:`，其 JSON 的 `text` 字段内用 `\n` 转义，两条结果之间是 `\n\n` 空行；正文用 `Highlights:`，并故意夹带 `Published:`/`Author:` 元信息行以便测试跳过）：

```
event: message
data: {"result":{"content":[{"type":"text","text":"Title: Go Concurrency Patterns: Context\nURL: https://go.dev/blog/context\nPublished: 2014-07-29\nAuthor: Sameer Ajmani\nHighlights:\nIn Go servers, each incoming request is handled in its own goroutine.\nWhen a request is canceled, all goroutines should exit quickly.\n\nTitle: Context package - Go\nURL: https://pkg.go.dev/context\nHighlights:\nPackage context defines the Context type."}]}}

```

- [ ] **Step 2: 写失败测试（重写 `pkg/tools/web_search_test.go`）**

整个替换文件内容为：

```go
package tools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func readFixture(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/exa_mcp_response.txt")
	if err != nil {
		t.Fatalf("读取夹具失败: %v", err)
	}
	return string(raw)
}

func TestParseExaResponse_Fixture(t *testing.T) {
	hits, err := parseExaResponse(readFixture(t), 10)
	if err != nil {
		t.Fatalf("parseExaResponse failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Title != "Go Concurrency Patterns: Context" {
		t.Fatalf("unexpected title[0]: %q", hits[0].Title)
	}
	if hits[0].Link != "https://go.dev/blog/context" {
		t.Fatalf("unexpected link[0]: %q", hits[0].Link)
	}
	if !strings.Contains(hits[0].Content, "In Go servers") || !strings.Contains(hits[0].Content, "When a request") {
		t.Fatalf("content[0] missing body lines: %q", hits[0].Content)
	}
	if strings.Contains(hits[0].Content, "Published") || strings.Contains(hits[0].Content, "Author") {
		t.Fatalf("content[0] should not include metadata lines: %q", hits[0].Content)
	}
	if hits[1].Title != "Context package - Go" {
		t.Fatalf("unexpected title[1]: %q", hits[1].Title)
	}
	if hits[1].Content != "Package context defines the Context type." {
		t.Fatalf("unexpected content[1]: %q", hits[1].Content)
	}
}

func TestParseExaResponse_Truncate(t *testing.T) {
	hits, err := parseExaResponse(readFixture(t), 1)
	if err != nil {
		t.Fatalf("parseExaResponse failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
}

func TestParseExaResponse_Empty(t *testing.T) {
	if _, err := parseExaResponse("", 3); err == nil {
		t.Fatal("expected error on empty response")
	}
}

func TestExaSearch_RequestAndParse(t *testing.T) {
	var captured map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(readFixture(t)))
	}))
	defer server.Close()

	hits, err := exaSearch(context.Background(), server.URL, "go context", 5)
	if err != nil {
		t.Fatalf("exaSearch failed: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	params, _ := captured["params"].(map[string]interface{})
	if params["name"] != "web_search_exa" {
		t.Fatalf("unexpected tool name: %v", params["name"])
	}
	argsm, _ := params["arguments"].(map[string]interface{})
	if argsm["query"] != "go context" {
		t.Fatalf("unexpected query: %v", argsm["query"])
	}
	if argsm["numResults"] != float64(5) {
		t.Fatalf("expected numResults=5, got %v", argsm["numResults"])
	}
}

func TestWebSearch_ExaEndToEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(readFixture(t)))
	}))
	defer server.Close()
	t.Setenv("EXA_MCP_URL", server.URL)

	result, err := WebSearch(`{"query":"go context","max_results":5}`)
	if err != nil {
		t.Fatalf("WebSearch failed: %v", err)
	}
	data, _ := json.Marshal(result)
	var parsed struct {
		Query   string `json:"query"`
		Results []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Content string `json:"content"`
		} `json:"results"`
	}
	json.Unmarshal(data, &parsed)
	if parsed.Query != "go context" {
		t.Fatalf("unexpected query: %s", parsed.Query)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(parsed.Results))
	}
	if parsed.Results[0].Title != "Go Concurrency Patterns: Context" {
		t.Fatalf("unexpected first title: %s", parsed.Results[0].Title)
	}
}

func TestWebSearch_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()
	t.Setenv("EXA_MCP_URL", server.URL)

	if _, err := WebSearch(`{"query":"test"}`); err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestWebSearch_ToolRegistered(t *testing.T) {
	found := false
	for _, def := range Definitions {
		if def.Function != nil && def.Function.Name == "web_search" {
			found = true
			if def.Function.Description == "" {
				t.Fatal("web_search should have a description")
			}
			params, ok := def.Function.Parameters.(map[string]interface{})
			if !ok {
				t.Fatal("expected Parameters to be a map")
			}
			props, ok := params["properties"].(map[string]interface{})
			if !ok {
				t.Fatal("expected properties in schema")
			}
			if _, ok := props["query"]; !ok {
				t.Fatal("expected 'query' in schema properties")
			}
			if _, ok := props["max_results"]; !ok {
				t.Fatal("expected 'max_results' in schema properties")
			}
			required, _ := params["required"].([]interface{})
			hasQuery := false
			for _, r := range required {
				if r == "query" {
					hasQuery = true
				}
			}
			if !hasQuery {
				t.Fatal("'query' should be in required fields")
			}
			break
		}
	}
	if !found {
		t.Fatal("web_search not found in Definitions")
	}
	if _, ok := Executors["web_search"]; !ok {
		t.Fatal("web_search not found in Executors")
	}
}
```

- [ ] **Step 3: 运行测试，确认失败**

Run: `go test ./pkg/tools -run 'Exa|WebSearch' -v`
Expected: 编译失败或 FAIL —— 因为 `web_search.go` 仍是旧 PIE 实现，`parseExaResponse` / `exaSearch` / `EXA_MCP_URL` 行为尚不存在。

- [ ] **Step 4: 重写 `pkg/tools/web_search.go` 为 Exa 实现**

完整替换文件内容为：

```go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultExaEndpoint = "https://mcp.exa.ai/mcp"

type WebSearchArguments struct {
	Query      string `json:"query" validate:"required" jsonschema:"required" jsonschema_description:"搜索关键词"`
	MaxResults int    `json:"max_results" jsonschema_description:"读取内容的结果数量，默认 3，最大 10"`
	Language   string `json:"language" jsonschema_description:"语言代码（当前后端忽略此参数）"`
	Country    string `json:"country" jsonschema_description:"国家代码（当前后端忽略此参数）"`
}

type webSearchResult struct {
	Query   string             `json:"query"`
	Results []webSearchOrganic `json:"results"`
	Error   string             `json:"error,omitempty"`
}

type webSearchOrganic struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"`
}

// SearchHit is a backend-agnostic single search result.
type SearchHit struct {
	Title   string
	Link    string
	Snippet string
	Content string
}

func exaEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("EXA_MCP_URL")); v != "" {
		return v
	}
	return defaultExaEndpoint
}

func WebSearch(args interface{}) (interface{}, error) {
	var params WebSearchArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 3
	}
	if maxResults > 10 {
		maxResults = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hits, err := exaSearch(ctx, exaEndpoint(), params.Query, maxResults)
	if err != nil {
		return nil, err
	}

	result := webSearchResult{Query: params.Query}
	for _, h := range hits {
		result.Results = append(result.Results, webSearchOrganic{
			Title:   h.Title,
			Link:    h.Link,
			Snippet: h.Snippet,
			Content: h.Content,
		})
	}
	return result, nil
}

func exaSearch(ctx context.Context, endpoint, query string, maxResults int) ([]SearchHit, error) {
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "web_search_exa",
			"arguments": map[string]interface{}{
				"query":      query,
				"type":       "auto",
				"numResults": maxResults,
				"livecrawl":  "fallback",
			},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("搜索失败: HTTP %d, body: %s", resp.StatusCode, truncate(string(respBody), 500))
	}

	return parseExaResponse(string(respBody), maxResults)
}

// parseExaResponse parses the Exa MCP SSE response text and returns up to maxResults hits.
func parseExaResponse(raw string, maxResults int) ([]SearchHit, error) {
	var texts []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var msg struct {
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(payload), &msg); err != nil {
			continue
		}
		for _, c := range msg.Result.Content {
			if strings.TrimSpace(c.Text) != "" {
				texts = append(texts, c.Text)
			}
		}
	}

	// Fallback: response is not SSE but already the raw text-block format.
	if len(texts) == 0 && strings.Contains(raw, "Title:") {
		texts = append(texts, raw)
	}
	if len(texts) == 0 {
		return nil, fmt.Errorf("Exa 搜索无可解析内容（响应可能为空或格式异常）")
	}

	hits := parseExaChunks(strings.Join(texts, "\n\n"))
	if len(hits) == 0 {
		return nil, fmt.Errorf("Exa 搜索结果解析为空")
	}
	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	return hits, nil
}

// parseExaChunks splits the text by \n\n and parses each block into a SearchHit.
func parseExaChunks(text string) []SearchHit {
	var hits []SearchHit
	for _, chunk := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		lines := strings.Split(chunk, "\n")
		var title, link string
		urlIdx, bodyIdx := -1, -1
		for i, ln := range lines {
			switch {
			case strings.HasPrefix(ln, "Title:"):
				title = strings.TrimSpace(strings.TrimPrefix(ln, "Title:"))
			case strings.HasPrefix(ln, "URL:") && urlIdx == -1:
				link = strings.TrimSpace(strings.TrimPrefix(ln, "URL:"))
				urlIdx = i
			case (strings.HasPrefix(ln, "Highlights:") || strings.HasPrefix(ln, "Text:")) && bodyIdx == -1:
				bodyIdx = i
			}
		}

		var content string
		if bodyIdx != -1 {
			first := lines[bodyIdx]
			first = strings.TrimPrefix(first, "Highlights:")
			first = strings.TrimPrefix(first, "Text:")
			parts := append([]string{strings.TrimSpace(first)}, lines[bodyIdx+1:]...)
			content = strings.TrimSpace(strings.Join(parts, "\n"))
		} else if urlIdx != -1 && urlIdx+1 < len(lines) {
			content = strings.TrimSpace(strings.Join(lines[urlIdx+1:], "\n"))
		}

		if title == "" && link == "" && content == "" {
			continue
		}
		hits = append(hits, SearchHit{
			Title:   title,
			Link:    link,
			Content: truncate(content, 2000),
		})
	}
	return hits
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `go test ./pkg/tools -run 'Exa|WebSearch' -v`
Expected: 全部 PASS（`TestParseExaResponse_Fixture`、`_Truncate`、`_Empty`、`TestExaSearch_RequestAndParse`、`TestWebSearch_ExaEndToEnd`、`TestWebSearch_HTTPError`、`TestWebSearch_ToolRegistered`）。

如有失败，按 superpowers:systematic-debugging 排查解析逻辑或夹具格式。

- [ ] **Step 6: 整库构建与全量测试**

Run: `go build ./...`
Expected: PASS（`SetWebSearchConfig`/`WebSearchConfig`/`globalWebSearchConfig` 已删除，且 Task 1 已移除其调用方，无未定义符号）。

Run: `go test ./...`
Expected: 全部 PASS。

- [ ] **Step 7: gofmt 与 vet**

Run: `gofmt -w ./cmd ./pkg`
Run: `go vet ./...`
Expected: 无输出。

- [ ] **Step 8: Commit**

```bash
git add pkg/tools/web_search.go pkg/tools/web_search_test.go pkg/tools/testdata/exa_mcp_response.txt
git commit -m "feat(tools): replace PIE web_search backend with free Exa MCP endpoint"
```

---

## Task 3: 文档更新

**Files:**
- Modify: `.env.example`
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: 更新 `.env.example`**

先查看当前内容：

Run: `cat .env.example`

操作：
- 若其中把 `PIE_APP_ID/PIE_APP_SECRET/PIE_GATEWAY_PATH` 描述为"网络搜索"所需，改为说明它们用于 **PIE LLM provider**（可选）。
- 新增一行可选配置说明：

```env
# 可选：覆盖 Exa 免费搜索端点（默认 https://mcp.exa.ai/mcp，无需 API key）
EXA_MCP_URL=
```

- [ ] **Step 2: 更新 `README.md`**

Run: `grep -n "web_search\|网页搜索\|PIE\|Serper" README.md`

把描述 `web_search` 的段落由"通过 PIE 网关 / Serper 搜索"改为"通过 Exa 免费 MCP 端点搜索，无需 API key、零配置；可用 `EXA_MCP_URL` 覆盖端点"。

- [ ] **Step 3: 更新 `CLAUDE.md`**

Run: `grep -n "web_search\|PIE\|搜索" CLAUDE.md`

在 `## 环境配置` 段落：将 `web_search` 相关说明更新为 Exa 免费搜索（零配置，`EXA_MCP_URL` 可选覆盖）。若文中把 PIE 变量描述为搜索用途，澄清 PIE 仅用于 LLM provider。

- [ ] **Step 4: 校对**

Run: `grep -rn "PIE\|Serper" README.md CLAUDE.md .env.example`
Expected: 残留的 PIE 引用都仅指向 “LLM provider” 语义，不再把 PIE/Serper 描述为网络搜索后端。

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md .env.example
git commit -m "docs: document free Exa web_search and clarify PIE is LLM-only"
```

---

## 完成标准

- `go test ./...` 全绿。
- `go vet ./...` 无输出。
- 在仅配置 `API_KEY/BASE_URL/MODEL`（无 PIE、无 `EXA_MCP_URL`）的环境下，`web_search` 工具能直接通过 `https://mcp.exa.ai/mcp` 返回结果。
- 代码中不再存在 `WebSearchConfig` / `SetWebSearchConfig` / `globalWebSearchConfig` 及 PIE/Serper 搜索路径。
- PIE 作为 LLM provider（`PieGatewayProvider`）的初始化保持可用。
- 每个提交均可独立 `go build ./...`。
