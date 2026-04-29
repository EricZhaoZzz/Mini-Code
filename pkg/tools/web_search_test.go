package tools

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSearch_MissingConfig(t *testing.T) {
	old := globalWebSearchConfig
	globalWebSearchConfig = nil
	defer func() { globalWebSearchConfig = old }()

	_, err := WebSearch(`{"query":"test"}`)
	if err == nil {
		t.Fatal("expected error when config is nil")
	}
}

func TestWebSearch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v2/extend/web/serper/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-App-Id") != "app_test" {
			t.Fatalf("missing or wrong X-App-Id: %s", r.Header.Get("X-App-Id"))
		}
		if r.Header.Get("Authorization") == "" {
			t.Fatal("missing Authorization header")
		}

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]interface{}
		json.Unmarshal(body, &reqBody)
		if reqBody["q"] != "golang testing" {
			t.Fatalf("unexpected query: %v", reqBody["q"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"organic": []map[string]interface{}{
				{
					"title":   "Go Testing",
					"link":    "https://example.com/go-testing",
					"snippet": "Learn Go testing...",
					"content": map[string]string{
						"text":     "Full content here",
						"markdown": "# Go Testing\nFull content here",
					},
				},
				{
					"title":   "Second Result",
					"link":    "https://example.com/second",
					"snippet": "Another result",
				},
			},
		})
	}))
	defer server.Close()

	old := globalWebSearchConfig
	globalWebSearchConfig = &WebSearchConfig{
		AppID:       "app_test",
		AppSecret:   "sk_test_secret",
		GatewayPath: server.URL,
	}
	defer func() { globalWebSearchConfig = old }()

	result, err := WebSearch(`{"query":"golang testing","max_results":2}`)
	if err != nil {
		t.Fatalf("WebSearch failed: %v", err)
	}

	data, _ := json.Marshal(result)
	var parsed struct {
		Query   string `json:"query"`
		Results []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
			Content string `json:"content"`
		} `json:"results"`
	}
	json.Unmarshal(data, &parsed)

	if parsed.Query != "golang testing" {
		t.Fatalf("expected query=golang testing, got %s", parsed.Query)
	}
	if len(parsed.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(parsed.Results))
	}
	if parsed.Results[0].Title != "Go Testing" {
		t.Fatalf("unexpected first result title: %s", parsed.Results[0].Title)
	}
	if parsed.Results[0].Content == "" {
		t.Fatal("expected content in first result")
	}
	if parsed.Results[1].Content != "" {
		t.Fatal("expected no content in second result (no content field in response)")
	}
}

func TestWebSearch_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	old := globalWebSearchConfig
	globalWebSearchConfig = &WebSearchConfig{
		AppID:       "app_test",
		AppSecret:   "sk_test_secret",
		GatewayPath: server.URL,
	}
	defer func() { globalWebSearchConfig = old }()

	_, err := WebSearch(`{"query":"test"}`)
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
}

func TestWebSearch_DefaultParams(t *testing.T) {
	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"organic": []interface{}{}})
	}))
	defer server.Close()

	old := globalWebSearchConfig
	globalWebSearchConfig = &WebSearchConfig{
		AppID:       "app_test",
		AppSecret:   "sk_test_secret",
		GatewayPath: server.URL,
	}
	defer func() { globalWebSearchConfig = old }()

	WebSearch(`{"query":"test"}`)

	if capturedBody["max_results"] != float64(3) {
		t.Fatalf("expected default max_results=3, got %v", capturedBody["max_results"])
	}
	if capturedBody["hl"] != "en" {
		t.Fatalf("expected default hl=en, got %v", capturedBody["hl"])
	}
	if capturedBody["gl"] != "us" {
		t.Fatalf("expected default gl=us, got %v", capturedBody["gl"])
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
