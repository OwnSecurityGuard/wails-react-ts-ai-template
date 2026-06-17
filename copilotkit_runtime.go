package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// CopilotKitRuntime 提供 CopilotKit 的 HTTP Runtime 端点
// 实现 AG-UI 协议，让前端 CopilotKit 组件能够直接连接
type CopilotKitRuntime struct {
	aiService   *AIService
	server      *http.Server
	port        int
	cancelMap   map[string]context.CancelFunc
	mu          sync.Mutex
}

// NewCopilotKitRuntime 创建 Runtime 实例
func NewCopilotKitRuntime(aiService *AIService) *CopilotKitRuntime {
	return &CopilotKitRuntime{
		aiService: aiService,
		port:      18999,
		cancelMap: make(map[string]context.CancelFunc),
	}
}

// Start 启动 HTTP server
func (r *CopilotKitRuntime) Start() error {
	mux := http.NewServeMux()
	basePath := "/api/copilotkit"

	// CORS middleware
	handler := withCORS(mux)

	// Runtime info endpoint
	mux.HandleFunc(basePath+"/info", r.handleInfo)

	// Agent run endpoint (SSE)
	mux.HandleFunc(basePath+"/agent/", r.handleAgent)

	// 处理 OPTIONS 预检请求
	mux.HandleFunc(basePath+"/", func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, req)
	})

	r.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", r.port),
		Handler: handler,
	}

	go func() {
		log.Printf("[CopilotKit Runtime] starting on http://localhost:%d%s", r.port, basePath)
		if err := r.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[CopilotKit Runtime] server error: %v", err)
		}
	}()

	return nil
}

// Stop 停止 HTTP server
func (r *CopilotKitRuntime) Stop() error {
	if r.server != nil {
		return r.server.Shutdown(context.Background())
	}
	return nil
}

// RuntimeURL 返回前端配置的 runtime URL
func (r *CopilotKitRuntime) RuntimeURL() string {
	return fmt.Sprintf("http://localhost:%d/api/copilotkit", r.port)
}

// withCORS 添加跨域支持
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		origin := req.Header.Get("Origin")
		if origin == "" {
			// Wails webview 可能不发 Origin，允许所有来源
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if req.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, req)
	})
}

// handleInfo 返回 Runtime 信息
func (r *CopilotKitRuntime) handleInfo(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := map[string]interface{}{
		"agents": []map[string]interface{}{
			{
				"id":          "default",
				"name":        "default",
				"description": "Default AI assistant",
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

// handleAgent 处理 agent 相关请求
func (r *CopilotKitRuntime) handleAgent(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, "/api/copilotkit/agent/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	agentID := parts[0]
	if agentID != "default" {
		http.Error(w, fmt.Sprintf("Agent '%s' not found", agentID), http.StatusNotFound)
		return
	}

	// 解析剩余路径: run, connect, stop/:threadId
	if len(parts) >= 2 {
		switch parts[1] {
		case "run":
			r.handleRun(w, req)
			return
		case "connect":
			r.handleConnect(w, req)
			return
		case "stop":
			if len(parts) >= 3 {
				r.handleStop(w, req, parts[2])
				return
			}
		}
	}

	http.Error(w, "Not found", http.StatusNotFound)
}

// RunAgentInput AG-UI 运行输入
type RunAgentInput struct {
	ThreadID       string                 `json:"threadId"`
	RunID          string                 `json:"runId"`
	ParentRunID    string                 `json:"parentRunId,omitempty"`
	State          interface{}            `json:"state"`
	Messages       []AGUIMessage          `json:"messages"`
	Tools          []AGUITool             `json:"tools"`
	Context        []AGUIContext          `json:"context"`
	ForwardedProps map[string]interface{} `json:"forwardedProps"`
}

// AGUIMessage AG-UI 消息格式
type AGUIMessage struct {
	ID        string      `json:"id"`
	Role      string      `json:"role"`
	Content   interface{} `json:"content,omitempty"` // string or []InputContent
	Name      string      `json:"name,omitempty"`
	ToolCalls []AGUIToolCall `json:"toolCalls,omitempty"`
}

// AGUITool AG-UI 工具定义
type AGUITool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// AGUIContext AG-UI 上下文
type AGUIContext struct {
	Name    string      `json:"name"`
	Value   interface{} `json:"value"`
}

// AGUIToolCall 工具调用
type AGUIToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function AGUIToolFunction `json:"function"`
}

// AGUIToolFunction 工具函数信息
type AGUIToolFunction struct {
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
}

// BaseEvent AG-UI 基础事件
type BaseEvent struct {
	Type      string      `json:"type"`
	Timestamp int64       `json:"timestamp,omitempty"`
	RawEvent  interface{} `json:"rawEvent,omitempty"`
}

// RunStartedEvent 运行开始事件
type RunStartedEvent struct {
	BaseEvent
	ThreadID string `json:"threadId"`
	RunID    string `json:"runId"`
}

// RunFinishedEvent 运行结束事件
type RunFinishedEvent struct {
	BaseEvent
	ThreadID string `json:"threadId"`
	RunID    string `json:"runId"`
}

// RunErrorEvent 运行错误事件
type RunErrorEvent struct {
	BaseEvent
	ThreadID string    `json:"threadId"`
	RunID    string    `json:"runId"`
	Error    AGUIError `json:"error"`
}

// AGUIError AG-UI 错误
type AGUIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// TextMessageStartEvent 文本消息开始
type TextMessageStartEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Role      string `json:"role"`
}

// TextMessageContentEvent 文本消息内容增量
type TextMessageContentEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
	Delta     string `json:"delta"`
}

// TextMessageEndEvent 文本消息结束
type TextMessageEndEvent struct {
	BaseEvent
	MessageID string `json:"messageId"`
}

// MessagesSnapshotEvent 消息快照
type MessagesSnapshotEvent struct {
	BaseEvent
	Messages []AGUIMessage `json:"messages"`
}

// ToolCallStartEvent 工具调用开始
type ToolCallStartEvent struct {
	BaseEvent
	ToolCallID   string `json:"toolCallId"`
	ToolCallName string `json:"toolCallName"`
}

// ToolCallArgsEvent 工具调用参数
type ToolCallArgsEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
	Delta      string `json:"delta"`
}

// ToolCallEndEvent 工具调用结束
type ToolCallEndEvent struct {
	BaseEvent
	ToolCallID string `json:"toolCallId"`
}

// StateSnapshotEvent 状态快照
type StateSnapshotEvent struct {
	BaseEvent
	Snapshot interface{} `json:"snapshot"`
}

const (
	EventTypeRunStarted        = "RUN_STARTED"
	EventTypeRunFinished       = "RUN_FINISHED"
	EventTypeRunError          = "RUN_ERROR"
	EventTypeTextMessageStart  = "TEXT_MESSAGE_START"
	EventTypeTextMessageContent= "TEXT_MESSAGE_CONTENT"
	EventTypeTextMessageEnd    = "TEXT_MESSAGE_END"
	EventTypeMessagesSnapshot  = "MESSAGES_SNAPSHOT"
	EventTypeToolCallStart     = "TOOL_CALL_START"
	EventTypeToolCallArgs      = "TOOL_CALL_ARGS"
	EventTypeToolCallEnd       = "TOOL_CALL_END"
	EventTypeStateSnapshot     = "STATE_SNAPSHOT"
)

// handleRun 处理 agent run 请求（SSE）
func (r *CopilotKitRuntime) handleRun(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input RunAgentInput
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// 设置 SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	// 获取 flusher
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()

	// 存储 cancel 函数以便 stop 使用
	r.mu.Lock()
	r.cancelMap[input.ThreadID] = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.cancelMap, input.ThreadID)
		r.mu.Unlock()
	}()

	// 发送 RUN_STARTED
	sendEvent(w, flusher, RunStartedEvent{
		BaseEvent: BaseEvent{Type: EventTypeRunStarted, Timestamp: nowMillis()},
		ThreadID:  input.ThreadID,
		RunID:     input.RunID,
	})

	// 发送消息快照
	sendEvent(w, flusher, MessagesSnapshotEvent{
		BaseEvent: BaseEvent{Type: EventTypeMessagesSnapshot, Timestamp: nowMillis()},
		Messages:  input.Messages,
	})

	// 执行流式聊天
	r.runStream(ctx, w, flusher, input)

	// 发送 RUN_FINISHED（如果没有错误且没有被取消）
	if ctx.Err() == nil {
		sendEvent(w, flusher, RunFinishedEvent{
			BaseEvent: BaseEvent{Type: EventTypeRunFinished, Timestamp: nowMillis()},
			ThreadID:  input.ThreadID,
			RunID:     input.RunID,
		})
	}
}

// handleConnect 处理 connect 请求
func (r *CopilotKitRuntime) handleConnect(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 对于新的 thread，直接返回空 SSE 流然后关闭
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if ok {
		flusher.Flush()
	}
}

// handleStop 处理停止请求
func (r *CopilotKitRuntime) handleStop(w http.ResponseWriter, req *http.Request, threadID string) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.mu.Lock()
	cancel, ok := r.cancelMap[threadID]
	r.mu.Unlock()

	if ok && cancel != nil {
		cancel()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// runStream 执行流式 LLM 调用并发送 AG-UI 事件
func (r *CopilotKitRuntime) runStream(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	input RunAgentInput,
) {
	cfg := r.aiService.configStore.Get()
	if cfg.APIKey == "" {
		sendEvent(w, flusher, RunErrorEvent{
			BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
			ThreadID:  input.ThreadID,
			RunID:     input.RunID,
			Error:     AGUIError{Code: "CONFIG_ERROR", Message: "API Key not configured"},
		})
		return
	}

	// 转换 AG-UI 消息为 OpenAI 消息格式
	openaiMessages := make([]openai.ChatCompletionMessage, 0, len(input.Messages))
	for _, msg := range input.Messages {
		content := ""
		switch v := msg.Content.(type) {
		case string:
			content = v
		case []interface{}:
			// 多模态内容，简化处理取第一个 text
			for _, item := range v {
				if m, ok := item.(map[string]interface{}); ok {
					if m["type"] == "text" {
						if text, ok := m["text"].(string); ok {
							content = text
							break
						}
					}
				}
			}
		}

		openaiMessages = append(openaiMessages, openai.ChatCompletionMessage{
			Role:    msg.Role,
			Content: content,
		})
	}

	clientConfig := openai.DefaultConfig(cfg.APIKey)
	if cfg.BaseURL != "" {
		clientConfig.BaseURL = cfg.BaseURL
	}
	client := openai.NewClientWithConfig(clientConfig)

	req := openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    openaiMessages,
		MaxTokens:   cfg.MaxTokens,
		Temperature: float32(cfg.Temperature),
		Stream:      true,
	}

	// 如果有 tools，添加到请求中
	if len(input.Tools) > 0 {
		openaiTools := make([]openai.Tool, 0, len(input.Tools))
		for _, tool := range input.Tools {
			paramsJSON, _ := json.Marshal(tool.Parameters)
			openaiTools = append(openaiTools, openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  paramsJSON,
				},
			})
		}
		req.Tools = openaiTools
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		sendEvent(w, flusher, RunErrorEvent{
			BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
			ThreadID:  input.ThreadID,
			RunID:     input.RunID,
			Error:     AGUIError{Code: "MODEL_ERROR", Message: fmt.Sprintf("create stream: %v", err)},
		})
		return
	}
	defer stream.Close()

	messageID := fmt.Sprintf("msg_%d", nowMillis())

	// 发送 TEXT_MESSAGE_START
	sendEvent(w, flusher, TextMessageStartEvent{
		BaseEvent: BaseEvent{Type: EventTypeTextMessageStart, Timestamp: nowMillis()},
		MessageID: messageID,
		Role:      "assistant",
	})

	for {
		select {
		case <-ctx.Done():
			// 被取消，发送结束事件
			sendEvent(w, flusher, TextMessageEndEvent{
				BaseEvent: BaseEvent{Type: EventTypeTextMessageEnd, Timestamp: nowMillis()},
				MessageID: messageID,
			})
			return
		default:
		}

		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				sendEvent(w, flusher, TextMessageEndEvent{
					BaseEvent: BaseEvent{Type: EventTypeTextMessageEnd, Timestamp: nowMillis()},
					MessageID: messageID,
				})
				return
			}
			sendEvent(w, flusher, RunErrorEvent{
				BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
				ThreadID:  input.ThreadID,
				RunID:     input.RunID,
				Error:     AGUIError{Code: "STREAM_ERROR", Message: fmt.Sprintf("stream error: %v", err)},
			})
			return
		}

		if len(response.Choices) > 0 {
			delta := response.Choices[0].Delta

			// 处理文本内容
			if delta.Content != "" {
				sendEvent(w, flusher, TextMessageContentEvent{
					BaseEvent: BaseEvent{Type: EventTypeTextMessageContent, Timestamp: nowMillis()},
					MessageID: messageID,
					Delta:     delta.Content,
				})
			}

			// 处理工具调用（简化版，实际需累积参数）
			if len(delta.ToolCalls) > 0 {
				for _, tc := range delta.ToolCalls {
					if tc.ID != "" {
						sendEvent(w, flusher, ToolCallStartEvent{
							BaseEvent:    BaseEvent{Type: EventTypeToolCallStart, Timestamp: nowMillis()},
							ToolCallID:   tc.ID,
							ToolCallName: tc.Function.Name,
						})
					}
					if tc.Function.Arguments != "" {
						sendEvent(w, flusher, ToolCallArgsEvent{
							BaseEvent:  BaseEvent{Type: EventTypeToolCallArgs, Timestamp: nowMillis()},
							ToolCallID: tc.ID,
							Delta:      tc.Function.Arguments,
						})
					}
					// 注意：工具调用结束需要更复杂的逻辑来判断
				}
			}
		}
	}
}

// sendEvent 发送 SSE 事件
func sendEvent(w http.ResponseWriter, flusher http.Flusher, event interface{}) {
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("[CopilotKit Runtime] failed to marshal event: %v", err)
		return
	}

	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// nowMillis 返回当前毫秒时间戳
func nowMillis() int64 {
	return time.Now().UnixMilli()
}
