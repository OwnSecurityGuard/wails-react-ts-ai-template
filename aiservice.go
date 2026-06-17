package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatMessage 表示聊天消息，前后端共享结构
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// normalizeBaseURL 去除 BaseURL 末尾斜杠，避免 SDK 拼接出 /v1//chat/completions 导致 404。
func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(baseURL, "/")
}

// effectiveMaxTokens 返回最终请求使用的 max_tokens，0 会被兜底为默认值，避免多数 API 直接拒绝。
func effectiveMaxTokens(maxTokens int) int {
	if maxTokens <= 0 {
		return 4096
	}
	return maxTokens
}

// customHeadersDoer 包装 HTTPDoer 并在每次请求时注入用户自定义 Header（跳过保留头）。
type customHeadersDoer struct {
	headers map[string]string
	base    openai.HTTPDoer
}

func (d *customHeadersDoer) Do(req *http.Request) (*http.Response, error) {
	if len(d.headers) == 0 {
		return d.base.Do(req)
	}
	cloned := req.Clone(req.Context())
	for k, v := range d.headers {
		name := strings.TrimSpace(k)
		if name == "" {
			continue
		}
		lower := strings.ToLower(name)
		// 保留头由 SDK 或鉴权逻辑控制，避免被覆盖
		if lower == "authorization" || lower == "api-key" || lower == "x-api-key" ||
			lower == "content-type" || lower == "content-length" {
			continue
		}
		cloned.Header.Set(name, v)
	}
	return d.base.Do(cloned)
}

// wrapHTTPDoerWithHeaders 返回注入自定义 Header 的 HTTPDoer。
func wrapHTTPDoerWithHeaders(doer openai.HTTPDoer, headers map[string]string) openai.HTTPDoer {
	if len(headers) == 0 || doer == nil {
		return doer
	}
	return &customHeadersDoer{headers: headers, base: doer}
}

// newOpenAIClient 创建已注入自定义 Header 的 go-openai 客户端。
func newOpenAIClient(cfg AIConfig) *openai.Client {
	clientConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = normalizeBaseURL(cfg.BaseURL)
	}
	clientConfig.HTTPClient = wrapHTTPDoerWithHeaders(clientConfig.HTTPClient, cfg.CustomHeaders)
	return openai.NewClientWithConfig(clientConfig)
}

// StreamChunk 表示流式响应的一块数据
type StreamChunk struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	Done      bool   `json:"done"`
	Error     string `json:"error,omitempty"`
}

// AIService 提供 AI 聊天功能
type AIService struct {
	configStore *ConfigStore
	cancelMap   map[string]context.CancelFunc
	mu          sync.Mutex
}

// NewAIService 创建 AI 服务实例
func NewAIService(store *ConfigStore) *AIService {
	return &AIService{
		configStore: store,
		cancelMap:   make(map[string]context.CancelFunc),
	}
}

// GetConfig 获取当前 AI 配置
func (s *AIService) GetConfig() AIConfig {
	return s.configStore.Get()
}

// SaveConfig 保存 AI 配置
func (s *AIService) SaveConfig(cfg AIConfig) error {
	return s.configStore.Save(cfg)
}

// GetProviderPresets 获取所有提供商预设
func (s *AIService) GetProviderPresets() map[AIProvider]ProviderPreset {
	return ProviderPresets()
}

// TestConnectionResult 测试连接结果
type TestConnectionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// TestConnection 测试 AI 连接
func (s *AIService) TestConnection(cfg AIConfig) TestConnectionResult {
	if cfg.APIKey == "" {
		return TestConnectionResult{Success: false, Message: "API Key is required"}
	}
	if cfg.BaseURL == "" {
		return TestConnectionResult{Success: false, Message: "Base URL is required"}
	}
	if cfg.Model == "" {
		return TestConnectionResult{Success: false, Message: "Model is required"}
	}

	client := newOpenAIClient(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req := openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}},
		MaxTokens:   1,
		Temperature: 0,
		TopP:        float32(cfg.TopP),
	}
	log.Printf("[AI TestConnection] base_url=%s model=%s top_p=%.2f", normalizeBaseURL(cfg.BaseURL), cfg.Model, cfg.TopP)
	_, err := client.CreateChatCompletion(ctx, req)
	if err != nil {
		return TestConnectionResult{
			Success: false,
			Message: fmt.Sprintf("Connection failed (%s/chat/completions): %v", normalizeBaseURL(cfg.BaseURL), err),
		}
	}

	return TestConnectionResult{Success: true, Message: fmt.Sprintf("Connected to %s", normalizeBaseURL(cfg.BaseURL))}
}

// ChatStream 启动流式聊天，通过 Wails 事件向前端推送数据
func (s *AIService) ChatStream(sessionID string, messages []ChatMessage) error {
	cfg := s.configStore.Get()
	if cfg.APIKey == "" {
		return fmt.Errorf("API Key not configured")
	}
	if cfg.BaseURL == "" {
		return fmt.Errorf("Base URL not configured (provider=%s, model=%s)", cfg.Provider, cfg.Model)
	}
	if cfg.Model == "" {
		return fmt.Errorf("Model not configured")
	}

	// 创建可取消的上下文，并附加流式超时兜底
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	s.mu.Lock()
	s.cancelMap[sessionID] = cancel
	s.mu.Unlock()

	go s.runStream(ctx, sessionID, messages, cfg)
	return nil
}

// StopStream 停止指定会话的流式输出
func (s *AIService) StopStream(sessionID string) {
	s.mu.Lock()
	cancel, ok := s.cancelMap[sessionID]
	if ok {
		cancel()
		delete(s.cancelMap, sessionID)
	}
	s.mu.Unlock()
}

// emitChunk 发送流式数据块到前端
func (s *AIService) emitChunk(chunk StreamChunk) {
	app := application.Get()
	if app != nil {
		app.Event.Emit("ai:stream:chunk", chunk)
	}
}

// runStream 在 goroutine 中执行流式请求
func (s *AIService) runStream(
	ctx context.Context,
	sessionID string,
	messages []ChatMessage,
	cfg AIConfig,
) {
	defer func() {
		s.mu.Lock()
		delete(s.cancelMap, sessionID)
		s.mu.Unlock()
	}()

	client := newOpenAIClient(cfg)

	// 转换消息格式
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		openaiMessages = append(openaiMessages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	maxTokens := effectiveMaxTokens(cfg.MaxTokens)
	req := openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    openaiMessages,
		MaxTokens:   maxTokens,
		Temperature: float32(cfg.Temperature),
		TopP:        float32(cfg.TopP),
		Stream:      true,
	}
	baseURL := normalizeBaseURL(cfg.BaseURL)
	log.Printf("[AI ChatStream] session=%s base_url=%s model=%s max_tokens=%d temperature=%.2f top_p=%.2f messages=%d",
		sessionID, baseURL, cfg.Model, maxTokens, cfg.Temperature, cfg.TopP, len(messages))

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		s.emitChunk(StreamChunk{
			SessionID: sessionID,
			Done:      true,
			Error:     fmt.Sprintf("create stream (%s/chat/completions): %v", baseURL, err),
		})
		return
	}
	defer stream.Close()

	for {
		select {
		case <-ctx.Done():
			s.emitChunk(StreamChunk{
				SessionID: sessionID,
				Done:      true,
			})
			return
		default:
		}

		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				s.emitChunk(StreamChunk{
					SessionID: sessionID,
					Done:      true,
				})
				return
			}
			s.emitChunk(StreamChunk{
				SessionID: sessionID,
				Done:      true,
				Error:     fmt.Sprintf("stream error: %v", err),
			})
			return
		}

		if len(response.Choices) > 0 {
			content := response.Choices[0].Delta.Content
			if content != "" {
				s.emitChunk(StreamChunk{
					SessionID: sessionID,
					Content:   content,
				})
			}
		}
	}
}
