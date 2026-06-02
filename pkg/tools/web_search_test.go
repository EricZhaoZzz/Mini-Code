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

func TestParseExaResponse_PlainTextFallback(t *testing.T) {
	raw := "Title: Plain Result\nURL: https://example.com\nHighlights:\nSome body text."
	hits, err := parseExaResponse(raw, 5)
	if err != nil {
		t.Fatalf("parseExaResponse fallback failed: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].Title != "Plain Result" {
		t.Fatalf("unexpected title: %q", hits[0].Title)
	}
	if hits[0].Link != "https://example.com" {
		t.Fatalf("unexpected link: %q", hits[0].Link)
	}
	if hits[0].Content != "Some body text." {
		t.Fatalf("unexpected content: %q", hits[0].Content)
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
