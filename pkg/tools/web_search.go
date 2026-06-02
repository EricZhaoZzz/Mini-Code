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
	Language   string `json:"language" jsonschema_description:"语言代码（保留以向后兼容，当前 Exa 后端忽略此参数）"`
	Country    string `json:"country" jsonschema_description:"国家代码（保留以向后兼容，当前 Exa 后端忽略此参数）"`
}

type webSearchResult struct {
	Query   string             `json:"query"`
	Results []webSearchOrganic `json:"results"`
	Error   string             `json:"error,omitempty"`
}

type webSearchOrganic struct {
	Title   string `json:"title"`
	Link    string `json:"link"`
	Snippet string `json:"snippet,omitempty"`
	Content string `json:"content,omitempty"`
}

// searchHit is a backend-agnostic single search result.
type searchHit struct {
	Title   string
	Link    string
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
			Content: h.Content,
		})
	}
	return result, nil
}

func exaSearch(ctx context.Context, endpoint, query string, maxResults int) ([]searchHit, error) {
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
func parseExaResponse(raw string, maxResults int) ([]searchHit, error) {
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

// parseExaChunks splits the text by \n\n and parses each block into a searchHit.
func parseExaChunks(text string) []searchHit {
	var hits []searchHit
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
		hits = append(hits, searchHit{
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
