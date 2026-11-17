package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"

	"github.com/sashabaranov/go-openai"
)

// DefaultMaxRetries 默认最大重试次数（不含首次请求）。
const DefaultMaxRetries = 3

// RetryConfig 重试策略。
type RetryConfig struct {
	// MaxRetries 最大重试次数（不含首次请求），0 表示不重试。
	MaxRetries int
	// BaseDelay 首次重试前的基准等待，之后按 2 倍递增。
	BaseDelay time.Duration
	// MaxDelay 单次等待上限。
	MaxDelay time.Duration
	// OnRetry 每次重试前回调，用于提示用户（可为 nil）。
	OnRetry func(attempt int, wait time.Duration, err error)
}

// DefaultRetryConfig 返回默认策略：最多重试 3 次，等待约 1s → 2s → 4s（带抖动），单次不超过 20s。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: DefaultMaxRetries,
		BaseDelay:  time.Second,
		MaxDelay:   20 * time.Second,
	}
}

// MaxRetriesFromEnv 读取 LM_MAX_RETRIES，非法值回退默认值。
func MaxRetriesFromEnv() int {
	if v := os.Getenv("LM_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return DefaultMaxRetries
}

// RetryProvider 为 Provider 增加瞬时错误重试。
//
// 只重试「请求根本没被处理」类错误：限流（429）、网关/服务端 5xx、请求超时（408）
// 与连接级错误。4xx 参数错误重试也不会成功；任务取消必须立即返回。
//
// 流式请求只在建立流时重试：go-openai 在建流阶段就检查状态码，429/5xx
// 都在这一步返回。流开始输出后出错不重试，否则已流到终端的内容会重复。
type RetryProvider struct {
	inner Provider
	cfg   RetryConfig
	sleep func(ctx context.Context, d time.Duration) error // 可替换，便于测试
}

// WithRetry 包装 Provider；MaxRetries <= 0 时原样返回。
func WithRetry(p Provider, cfg RetryConfig) Provider {
	if cfg.MaxRetries <= 0 {
		return p
	}
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = time.Second
	}
	if cfg.MaxDelay < cfg.BaseDelay {
		cfg.MaxDelay = cfg.BaseDelay
	}
	return &RetryProvider{inner: p, cfg: cfg, sleep: sleepContext}
}

func (r *RetryProvider) Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return retry(ctx, r, func() (openai.ChatCompletionResponse, error) {
		return r.inner.Chat(ctx, req)
	})
}

func (r *RetryProvider) ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return retry(ctx, r, func() (*openai.ChatCompletionStream, error) {
		return r.inner.ChatStream(ctx, req)
	})
}

func retry[T any](ctx context.Context, r *RetryProvider, call func() (T, error)) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		result, err := call()
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil || !IsRetryable(err) {
			return zero, err
		}
		if attempt >= r.cfg.MaxRetries {
			return zero, fmt.Errorf("重试 %d 次后仍失败: %w", r.cfg.MaxRetries, err)
		}

		wait := r.backoff(attempt)
		if r.cfg.OnRetry != nil {
			r.cfg.OnRetry(attempt+1, wait, err)
		}
		if err := r.sleep(ctx, wait); err != nil {
			return zero, err
		}
	}
}

// backoff 指数退避 + 抖动：在 [d/2, d] 内取值，避免多个客户端同时重试再次撞上限流。
func (r *RetryProvider) backoff(attempt int) time.Duration {
	d := r.cfg.BaseDelay << attempt
	if d <= 0 || d > r.cfg.MaxDelay { // <= 0 防移位溢出
		d = r.cfg.MaxDelay
	}
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsRetryable 判断错误是否为值得重试的瞬时错误。
func IsRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatusCode > 0 {
		return isRetryableStatus(apiErr.HTTPStatusCode)
	}
	var reqErr *openai.RequestError
	if errors.As(err, &reqErr) && reqErr.HTTPStatusCode > 0 {
		return isRetryableStatus(reqErr.HTTPStatusCode)
	}

	// 超时不重试：HTTP 客户端超时为 5 分钟，再重试几次会让用户等上十几分钟
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	// 域名不存在多为 BASE_URL 配置错误，重试只会推迟报错
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return false
	}
	// 连接级错误：请求大概率未被服务端处理
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		529: // 部分服务商用于表示过载
		return true
	}
	return false
}
