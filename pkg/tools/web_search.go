package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mini-code/pkg/provider"
	"net/http"
	"strings"
	"time"
)

type WebSearchConfig struct {
	AppID       string
	AppSecret   string
	GatewayPath string
}

var globalWebSearchConfig *WebSearchConfig

func SetWebSearchConfig(cfg *WebSearchConfig) {
	globalWebSearchConfig = cfg
}

type WebSearchArguments struct {
	Query      string `json:"query" validate:"required" jsonschema:"required" jsonschema_description:"搜索关键词"`
	MaxResults int    `json:"max_results" jsonschema_description:"读取内容的结果数量，默认 3，最大 10"`
	Language   string `json:"language" jsonschema_description:"语言代码，如 en, zh-cn，默认 en"`
	Country    string `json:"country" jsonschema_description:"国家代码，如 us, cn，默认 us"`
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

func WebSearch(args interface{}) (interface{}, error) {
	var params WebSearchArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	if globalWebSearchConfig == nil {
		return nil, fmt.Errorf("web_search 未配置：需要设置 PIE_APP_ID / PIE_APP_SECRET / PIE_GATEWAY_PATH")
	}

	cfg := globalWebSearchConfig
	maxResults := params.MaxResults
	if maxResults <= 0 {
		maxResults = 3
	}
	if maxResults > 10 {
		maxResults = 10
	}

	lang := params.Language
	if lang == "" {
		lang = "en"
	}
	country := params.Country
	if country == "" {
		country = "us"
	}

	reqBody := map[string]interface{}{
		"q":           params.Query,
		"max_results": maxResults,
		"hl":          lang,
		"gl":          country,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	apiPath := "/v2/extend/web/serper/search"
	fullURL := strings.TrimRight(cfg.GatewayPath, "/") + apiPath

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	timestamp := time.Now().Unix()
	nonce, err := provider.GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	signature := provider.ComputeHMACSignature("POST", apiPath, timestamp, nonce, cfg.AppID, cfg.AppSecret)

	req.Header.Set("X-App-Id", cfg.AppID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("Authorization", "HMAC-SHA256 "+signature)

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

	var serperResp struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
			Content *struct {
				Text     string `json:"text"`
				Markdown string `json:"markdown"`
			} `json:"content"`
		} `json:"organic"`
	}
	if err := json.Unmarshal(respBody, &serperResp); err != nil {
		return nil, fmt.Errorf("解析搜索结果失败: %w", err)
	}

	result := webSearchResult{Query: params.Query}
	for _, item := range serperResp.Organic {
		organic := webSearchOrganic{
			Title:   item.Title,
			Link:    item.Link,
			Snippet: item.Snippet,
		}
		if item.Content != nil {
			if item.Content.Markdown != "" {
				organic.Content = truncate(item.Content.Markdown, 2000)
			} else if item.Content.Text != "" {
				organic.Content = truncate(item.Content.Text, 2000)
			}
		}
		result.Results = append(result.Results, organic)
	}

	return result, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
