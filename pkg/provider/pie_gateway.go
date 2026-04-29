package provider

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// PieGatewayConfig 是 Pie Gateway 的配置参数。
type PieGatewayConfig struct {
	AppID       string // PIE_APP_ID
	AppSecret   string // PIE_APP_SECRET
	GatewayPath string // PIE_GATEWAY_PATH，例如 https://pie-gateway.weapp.me
	Model       string // 模型名称
}

// PieGatewayProvider 通过 Pie Gateway 的 HMAC-SHA256 签名认证调用 OpenAI 兼容接口。
type PieGatewayProvider struct {
	client *openai.Client
	model  string
}

// NewPieGatewayProvider 创建一个使用 Pie Gateway 的 Provider。
func NewPieGatewayProvider(cfg PieGatewayConfig) *PieGatewayProvider {
	openaiCfg := openai.DefaultConfig("")
	openaiCfg.BaseURL = strings.TrimRight(cfg.GatewayPath, "/") + "/v2/extend"
	openaiCfg.HTTPClient = &hmacHTTPDoer{
		inner:     &http.Client{Timeout: 5 * time.Minute},
		appID:     cfg.AppID,
		appSecret: cfg.AppSecret,
	}

	return &PieGatewayProvider{
		client: openai.NewClientWithConfig(openaiCfg),
		model:  cfg.Model,
	}
}

func (p *PieGatewayProvider) Model() string { return p.model }

func (p *PieGatewayProvider) Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return p.client.CreateChatCompletion(ctx, req)
}

func (p *PieGatewayProvider) ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return p.client.CreateChatCompletionStream(ctx, req)
}

// hmacHTTPDoer 拦截 HTTP 请求，注入 HMAC-SHA256 签名认证头。
type hmacHTTPDoer struct {
	inner     *http.Client
	appID     string
	appSecret string
}

func (d *hmacHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	timestamp := time.Now().Unix()
	nonce, err := GenerateNonce()
	if err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}

	path := req.URL.Path
	signature := ComputeHMACSignature(req.Method, path, timestamp, nonce, d.appID, d.appSecret)

	req.Header.Set("X-App-Id", d.appID)
	req.Header.Set("X-Timestamp", fmt.Sprintf("%d", timestamp))
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("Authorization", "HMAC-SHA256 "+signature)

	return d.inner.Do(req)
}

// computeHMACSignature 计算 HMAC-SHA256 签名。
// 签名字符串格式: {METHOD}\n{PATH}\n{TIMESTAMP}\n{NONCE}\n{APP_ID}
func ComputeHMACSignature(method, path string, timestamp int64, nonce, appID, appSecret string) string {
	signatureString := fmt.Sprintf("%s\n%s\n%d\n%s\n%s", method, path, timestamp, nonce, appID)
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write([]byte(signatureString))
	return hex.EncodeToString(mac.Sum(nil))
}

// GenerateNonce 生成 16 字节随机十六进制字符串。
func GenerateNonce() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
