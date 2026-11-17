package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"
)

// newFlakyServer 前 failures 次返回 status，之后正常响应（stream=true 时返回 SSE）。
func newFlakyServer(t *testing.T, failures int32, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= failures {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"message":"flaky %d","type":"server_error"}}`, n)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"stream":true`) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func newRetryingProvider(srv *httptest.Server, maxRetries int, onRetry func(int, time.Duration, error)) *RetryProvider {
	cfg := openai.DefaultConfig("k")
	cfg.BaseURL = srv.URL + "/v1"
	p := WithRetry(NewOpenAIProvider(cfg, "m"), RetryConfig{
		MaxRetries: maxRetries, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, OnRetry: onRetry,
	}).(*RetryProvider)
	return p
}

func chatReq(stream bool) openai.ChatCompletionRequest {
	return openai.ChatCompletionRequest{Model: "m", Stream: stream,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "x"}}}
}

func TestRetry_ChatRecoversFrom429(t *testing.T) {
	srv, calls := newFlakyServer(t, 2, http.StatusTooManyRequests)
	var retries []int
	p := newRetryingProvider(srv, 3, func(a int, _ time.Duration, _ error) { retries = append(retries, a) })

	resp, err := p.Chat(context.Background(), chatReq(false))
	if err != nil {
		t.Fatalf("应在重试后成功: %v", err)
	}
	if resp.Choices[0].Message.Content != "hi" {
		t.Errorf("unexpected resp %+v", resp)
	}
	if calls.Load() != 3 || len(retries) != 2 || retries[0] != 1 || retries[1] != 2 {
		t.Errorf("calls=%d retries=%v, 期望 3 次请求、回调 [1 2]", calls.Load(), retries)
	}
}

func TestRetry_StreamRecoversFrom503(t *testing.T) {
	srv, calls := newFlakyServer(t, 1, http.StatusServiceUnavailable)
	p := newRetryingProvider(srv, 3, nil)

	stream, err := p.ChatStream(context.Background(), chatReq(true))
	if err != nil {
		t.Fatalf("建流应在重试后成功: %v", err)
	}
	defer stream.Close()
	chunk, err := stream.Recv()
	if err != nil || chunk.Choices[0].Delta.Content != "hi" {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls=%d, 期望 2", calls.Load())
	}
}

func TestRetry_GivesUpAfterMaxRetries(t *testing.T) {
	srv, calls := newFlakyServer(t, 100, http.StatusBadGateway)
	p := newRetryingProvider(srv, 2, nil)

	_, err := p.Chat(context.Background(), chatReq(false))
	if err == nil || !strings.Contains(err.Error(), "重试 2 次后仍失败") {
		t.Fatalf("应报告重试耗尽, 得到 %v", err)
	}
	var apiErr *openai.APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatusCode != http.StatusBadGateway {
		t.Errorf("应保留原始错误供上层判断: %v", err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls=%d, 期望 1 + 2 次重试", calls.Load())
	}
}

func TestRetry_DoesNotRetryClientErrors(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusNotFound} {
		srv, calls := newFlakyServer(t, 100, status)
		p := newRetryingProvider(srv, 3, nil)
		if _, err := p.Chat(context.Background(), chatReq(false)); err == nil {
			t.Fatalf("status %d 应失败", status)
		}
		if calls.Load() != 1 {
			t.Errorf("status %d 不应重试, calls=%d", status, calls.Load())
		}
	}
}

// 退避等待期间取消任务必须立即返回，不能等满退避时间。
func TestRetry_CancelDuringBackoff(t *testing.T) {
	srv, calls := newFlakyServer(t, 100, http.StatusTooManyRequests)
	cfg := openai.DefaultConfig("k")
	cfg.BaseURL = srv.URL + "/v1"
	p := WithRetry(NewOpenAIProvider(cfg, "m"), RetryConfig{MaxRetries: 5, BaseDelay: time.Minute, MaxDelay: time.Minute})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := p.Chat(ctx, chatReq(false))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("应返回取消错误, 得到 %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("取消后未及时返回")
	}
	if calls.Load() != 1 {
		t.Errorf("calls=%d, 期望 1", calls.Load())
	}
}

func TestWithRetry_ZeroDisables(t *testing.T) {
	inner := NewOpenAIProvider(openai.DefaultConfig("k"), "m")
	if got := WithRetry(inner, RetryConfig{MaxRetries: 0}); got != Provider(inner) {
		t.Error("MaxRetries=0 应原样返回内层 Provider")
	}
}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"429", &openai.APIError{HTTPStatusCode: 429}, true},
		{"500", &openai.RequestError{HTTPStatusCode: 500}, true},
		{"529", &openai.APIError{HTTPStatusCode: 529}, true},
		{"400", &openai.APIError{HTTPStatusCode: 400}, false},
		{"wrapped 503", fmt.Errorf("stream error: %w", &openai.APIError{HTTPStatusCode: 503}), true},
		{"canceled", context.Canceled, false},
		{"deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), false},
		{"unexpected eof", io.ErrUnexpectedEOF, true},
		{"dial", &net.OpError{Op: "dial", Err: errors.New("refused")}, true},
		{"dns not found", &net.OpError{Op: "dial", Err: &net.DNSError{IsNotFound: true}}, false},
		{"timeout", &net.OpError{Op: "read", Err: timeoutErr{}}, false},
		{"plain", errors.New("boom"), false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("%s: IsRetryable = %v, want %v", c.name, got, c.want)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestBackoffBounds(t *testing.T) {
	r := &RetryProvider{cfg: RetryConfig{BaseDelay: time.Second, MaxDelay: 4 * time.Second}}
	for attempt, wantMax := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second, 4 * time.Second} {
		for i := 0; i < 50; i++ {
			d := r.backoff(attempt)
			if d < wantMax/2 || d > wantMax {
				t.Fatalf("attempt %d: backoff %v 不在 [%v, %v]", attempt, d, wantMax/2, wantMax)
			}
		}
	}
	// 移位溢出不应产生负值
	if d := r.backoff(70); d <= 0 || d > 4*time.Second {
		t.Errorf("大 attempt 时 backoff = %v", d)
	}
}

func TestMaxRetriesFromEnv(t *testing.T) {
	t.Setenv("LM_MAX_RETRIES", "")
	if MaxRetriesFromEnv() != DefaultMaxRetries {
		t.Error("默认值错误")
	}
	t.Setenv("LM_MAX_RETRIES", "0")
	if MaxRetriesFromEnv() != 0 {
		t.Error("0 应关闭重试")
	}
	for _, v := range []string{"-1", "abc"} {
		t.Setenv("LM_MAX_RETRIES", v)
		if MaxRetriesFromEnv() != DefaultMaxRetries {
			t.Errorf("非法值 %q 应回退默认", v)
		}
	}
}

// Pie Gateway 的签名含一次性 nonce：重试必须重新签名，否则会被网关当作重放请求拒绝。
func TestRetry_PieGatewayResignsEachAttempt(t *testing.T) {
	var nonces []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nonces = append(nonces, r.Header.Get("X-Nonce"))
		w.Header().Set("Content-Type", "application/json")
		if len(nonces) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"slow down"}}`)
			return
		}
		fmt.Fprint(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer srv.Close()

	p := WithRetry(NewPieGatewayProvider(PieGatewayConfig{
		AppID: "app", AppSecret: "secret", GatewayPath: srv.URL, Model: "m",
	}), RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond})

	if _, err := p.Chat(context.Background(), chatReq(false)); err != nil {
		t.Fatalf("应在重试后成功: %v", err)
	}
	if len(nonces) != 3 {
		t.Fatalf("请求次数 = %d, 期望 3", len(nonces))
	}
	seen := map[string]bool{}
	for _, n := range nonces {
		if n == "" || seen[n] {
			t.Fatalf("每次重试必须使用新的 nonce: %v", nonces)
		}
		seen[n] = true
	}
}
