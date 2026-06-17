package main

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/sashabaranov/go-openai"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ChatMessage 表示聊天消息，前后端共享结构
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

	clientConfig := openai.DefaultConfig(cfg.APIKey)
	clientConfig.BaseURL = cfg.BaseURL
	client := openai.NewClientWithConfig(clientConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 15000)
	defer cancel()

	_, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}},
		MaxTokens:   1,
		Temperature: 0,
	})
	if err != nil {
		return TestConnectionResult{
			Success: false,
			Message: fmt.Sprintf("Connection failed (%s/chat/completions): %v", cfg.BaseURL, err),
		}
	}

	return TestConnectionResult{Success: true, Message: fmt.Sprintf("Connected to %s", cfg.BaseURL)}
}

// ChatStream 启动流式聊天，通过 Wails 事件向前端推送数据
func (s *AIService) ChatStream(sessionID string, messages []ChatMessage) error {
	cfg := s.configStore.Get()
	if cfg.APIKey == "" {
		return fmt.Errorf("API Key not configured")
	}

	// 创建可取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
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

	clientConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = cfg.BaseURL
	}
	client := openai.NewClientWithConfig(clientConfig)

	// 转换消息格式
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(messages))
	for _, msg := range messages {
		openaiMessages = append(openaiMessages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	req := openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    openaiMessages,
		MaxTokens:   cfg.MaxTokens,
		Temperature: float32(cfg.Temperature),
		Stream:      true,
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		s.emitChunk(StreamChunk{
			SessionID: sessionID,
			Done:      true,
			Error:     fmt.Sprintf("create stream (%s/chat/completions): %v", cfg.BaseURL, err),
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
