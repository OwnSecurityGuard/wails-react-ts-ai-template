package main

import (
	"context"
	"encoding/json"
	"errors"
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
// 实现 AG-UI 协议，让前端 CopilotKit 组件能够直接连接。
// 当前实现支持文本流式输出与工具调用（function calling）闭环：
//   1. 前端 POST /agent/default/run 启动一次运行（SSE 流）
//   2. 后端流式调用 LLM，遇到 tool_calls 时推送 TOOL_CALL_* 事件
//   3. 后端推送 RUN_WAITING_FOR_TOOL_RESULTS 事件并阻塞等待
//   4. 前端执行工具后 POST /agent/default/run/{threadId}/tool-result
//   5. 后端追加 tool 消息，再次调用 LLM，继续流式输出
// 状态全部保存在内存中，应用重启后丢失。
type CopilotKitRuntime struct {
	aiService *AIService
	server    *http.Server
	port      int

	// runs 保存进行中的运行，key 为 threadID
	runs map[string]*agentRun
	mu   sync.RWMutex
}

// NewCopilotKitRuntime 创建 Runtime 实例
func NewCopilotKitRuntime(aiService *AIService) *CopilotKitRuntime {
	return &CopilotKitRuntime{
		aiService: aiService,
		port:      18999,
		runs:      make(map[string]*agentRun),
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

	// Agent run endpoint (SSE) 与 tool-result 续跑端点
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
				"description": "Default AI assistant with tool calling support",
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

	// 解析剩余路径: run, run/:threadId/tool-result, connect, stop/:threadId
	if len(parts) >= 2 {
		switch parts[1] {
		case "run":
			if len(parts) >= 4 && parts[2] != "" && parts[3] == "tool-result" {
				r.handleToolResult(w, req, parts[2])
				return
			}
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
	ID        string         `json:"id"`
	Role      string         `json:"role"`
	Content   interface{}    `json:"content,omitempty"` // string or []InputContent
	Name      string         `json:"name,omitempty"`
	ToolCalls []AGUIToolCall `json:"toolCalls,omitempty"`
	// ToolCallID 用于 role="tool" 的消息，对应 assistant 之前发起的 tool_call
	ToolCallID string `json:"toolCallId,omitempty"`
}

// AGUITool AG-UI 工具定义
type AGUITool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// AGUIContext AG-UI 上下文
type AGUIContext struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// AGUIToolCall 工具调用
type AGUIToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function AGUIToolFunction `json:"function"`
}

// AGUIToolFunction 工具函数信息
type AGUIToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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

// RunWaitingForToolResultsEvent 通知前端执行工具并回传结果
type RunWaitingForToolResultsEvent struct {
	BaseEvent
	ThreadID  string         `json:"threadId"`
	RunID     string         `json:"runId"`
	ToolCalls []AGUIToolCall `json:"toolCalls"`
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
	EventTypeRunStarted              = "RUN_STARTED"
	EventTypeRunFinished             = "RUN_FINISHED"
	EventTypeRunError                = "RUN_ERROR"
	EventTypeRunWaitingForToolResults = "RUN_WAITING_FOR_TOOL_RESULTS"
	EventTypeTextMessageStart        = "TEXT_MESSAGE_START"
	EventTypeTextMessageContent      = "TEXT_MESSAGE_CONTENT"
	EventTypeTextMessageEnd          = "TEXT_MESSAGE_END"
	EventTypeMessagesSnapshot        = "MESSAGES_SNAPSHOT"
	EventTypeToolCallStart           = "TOOL_CALL_START"
	EventTypeToolCallArgs            = "TOOL_CALL_ARGS"
	EventTypeToolCallEnd             = "TOOL_CALL_END"
	EventTypeStateSnapshot           = "STATE_SNAPSHOT"
)

// pendingToolCall 累积流式响应中的单个工具调用
type pendingToolCall struct {
	ID        string
	Type      string
	Name      string
	Arguments string
}

// toolResult 前端执行完工具后回传的结果
type toolResult struct {
	ToolCallID string `json:"toolCallId"`
	Name       string `json:"name"`
	Result     string `json:"result"`
}

// SubmitToolResultsInput 前端提交工具结果的请求体
type SubmitToolResultsInput struct {
	ThreadID    string       `json:"threadId"`
	RunID       string       `json:"runId"`
	ToolResults []toolResult `json:"toolResults"`
}

// agentRun 单次运行的状态机
type agentRun struct {
	threadID string
	runID    string
	messages []openai.ChatCompletionMessage
	tools    []openai.Tool

	// eventCh 向 SSE 客户端推送事件，带缓冲避免 goroutine 阻塞
	eventCh chan interface{}
	// toolResultCh 接收前端执行完工具后的结果
	toolResultCh chan []toolResult
	// cancel 用于客户端断开或主动停止时中断运行
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
}

func (run *agentRun) appendMessages(msgs ...openai.ChatCompletionMessage) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.messages = append(run.messages, msgs...)
}

func (run *agentRun) getMessages() []openai.ChatCompletionMessage {
	run.mu.Lock()
	defer run.mu.Unlock()
	out := make([]openai.ChatCompletionMessage, len(run.messages))
	copy(out, run.messages)
	return out
}

func (run *agentRun) emit(ev interface{}) {
	run.mu.Lock()
	closed := run.closed
	run.mu.Unlock()
	if closed {
		return
	}
	select {
	case run.eventCh <- ev:
	case <-time.After(5 * time.Second):
		log.Printf("[CopilotKit Runtime] event channel blocked, dropping event %T", ev)
	}
}

func (run *agentRun) close() {
	run.mu.Lock()
	defer run.mu.Unlock()
	if run.closed {
		return
	}
	run.closed = true
	close(run.eventCh)
}

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

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(req.Context())

	// 转换消息与工具
	openaiMessages := convertAGUIMessages(input.Messages)
	openaiTools := convertAGUITools(input.Tools)

	run := &agentRun{
		threadID:     input.ThreadID,
		runID:        input.RunID,
		messages:     openaiMessages,
		tools:        openaiTools,
		eventCh:      make(chan interface{}, 64),
		toolResultCh: make(chan []toolResult, 1),
		cancel:       cancel,
	}

	// 保存运行状态
	r.mu.Lock()
	r.runs[input.ThreadID] = run
	r.mu.Unlock()

	// 清理函数
	cleanup := func() {
		cancel()
		r.mu.Lock()
		delete(r.runs, input.ThreadID)
		r.mu.Unlock()
	}
	defer cleanup()

	// 启动 agent 循环
	go r.agentLoop(ctx, run)

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

	// 从 eventCh 读取事件并推送到 SSE
	for ev := range run.eventCh {
		sendEvent(w, flusher, ev)
		if _, finished := ev.(RunFinishedEvent); finished {
			break
		}
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
	run, ok := r.runs[threadID]
	r.mu.Unlock()

	if ok && run != nil && run.cancel != nil {
		run.cancel()
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleToolResult 接收前端执行完工具后的结果，唤醒 agentLoop 继续运行
func (r *CopilotKitRuntime) handleToolResult(w http.ResponseWriter, req *http.Request, threadID string) {
	if req.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input SubmitToolResultsInput
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	r.mu.RLock()
	run, ok := r.runs[threadID]
	r.mu.RUnlock()

	if !ok || run == nil {
		http.Error(w, "Run not found", http.StatusNotFound)
		return
	}

	if run.runID != input.RunID {
		http.Error(w, "Run ID mismatch", http.StatusBadRequest)
		return
	}

	select {
	case run.toolResultCh <- input.ToolResults:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	case <-time.After(5 * time.Second):
		http.Error(w, "Run not waiting for tool results", http.StatusConflict)
	}
}

// agentLoop 运行 Agent 主循环，处理多轮工具调用
func (r *CopilotKitRuntime) agentLoop(ctx context.Context, run *agentRun) {
	defer run.close()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		hasToolCalls, err := r.runOneTurn(ctx, run)
		if err != nil {
			return
		}

		if !hasToolCalls {
			// 没有工具调用，本次运行自然结束
			run.emit(RunFinishedEvent{
				BaseEvent: BaseEvent{Type: EventTypeRunFinished, Timestamp: nowMillis()},
				ThreadID:  run.threadID,
				RunID:     run.runID,
			})
			return
		}

		// 有工具调用，等待前端执行结果
		select {
		case <-ctx.Done():
			return
		case results := <-run.toolResultCh:
			toolMsgs := make([]openai.ChatCompletionMessage, 0, len(results))
			for _, tr := range results {
				toolMsgs = append(toolMsgs, openai.ChatCompletionMessage{
					Role:       "tool",
					Content:    tr.Result,
					ToolCallID: tr.ToolCallID,
					Name:       tr.Name,
				})
			}
			run.appendMessages(toolMsgs...)
		}
	}
}

// runOneTurn 执行一轮 LLM 流式调用。返回值表示本轮是否产生了需要前端执行的工具调用。
func (r *CopilotKitRuntime) runOneTurn(ctx context.Context, run *agentRun) (bool, error) {
	cfg := r.aiService.configStore.Get()
	if cfg.APIKey == "" {
		run.emit(RunErrorEvent{
			BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
			ThreadID:  run.threadID,
			RunID:     run.runID,
			Error:     AGUIError{Code: "CONFIG_ERROR", Message: "API Key not configured"},
		})
		return false, errors.New("api key not configured")
	}

	client := newOpenAIClient(cfg)

	req := openai.ChatCompletionRequest{
		Model:       cfg.Model,
		Messages:    run.getMessages(),
		MaxTokens:   effectiveMaxTokens(cfg.MaxTokens),
		Temperature: float32(cfg.Temperature),
		TopP:        float32(cfg.TopP),
		Stream:      true,
		Tools:       run.tools,
	}

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		run.emit(RunErrorEvent{
			BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
			ThreadID:  run.threadID,
			RunID:     run.runID,
			Error:     AGUIError{Code: "MODEL_ERROR", Message: fmt.Sprintf("create stream: %v", err)},
		})
		return false, err
	}
	defer stream.Close()

	messageID := fmt.Sprintf("msg_%d", nowMillis())
	run.emit(TextMessageStartEvent{
		BaseEvent: BaseEvent{Type: EventTypeTextMessageStart, Timestamp: nowMillis()},
		MessageID: messageID,
		Role:      "assistant",
	})

	var contentBuf strings.Builder
	pendingCalls := make(map[string]*pendingToolCall)

streamLoop:
	for {
		select {
		case <-ctx.Done():
			run.emit(TextMessageEndEvent{
				BaseEvent: BaseEvent{Type: EventTypeTextMessageEnd, Timestamp: nowMillis()},
				MessageID: messageID,
			})
			return false, nil
		default:
		}

		response, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break streamLoop
			}
			run.emit(RunErrorEvent{
				BaseEvent: BaseEvent{Type: EventTypeRunError, Timestamp: nowMillis()},
				ThreadID:  run.threadID,
				RunID:     run.runID,
				Error:     AGUIError{Code: "STREAM_ERROR", Message: fmt.Sprintf("stream error: %v", err)},
			})
			return false, err
		}

		if len(response.Choices) == 0 {
			continue
		}
		delta := response.Choices[0].Delta

		if delta.Content != "" {
			contentBuf.WriteString(delta.Content)
			run.emit(TextMessageContentEvent{
				BaseEvent: BaseEvent{Type: EventTypeTextMessageContent, Timestamp: nowMillis()},
				MessageID: messageID,
				Delta:     delta.Content,
			})
		}

		if len(delta.ToolCalls) > 0 {
			for _, tc := range delta.ToolCalls {
				id := tc.ID
				// OpenAI 流式工具调用偶尔会在后续 delta 里把 ID 置空，沿用已有 ID
				if id == "" && len(pendingCalls) == 1 {
					for k := range pendingCalls {
						id = k
						break
					}
				}
				if id == "" {
					continue
				}

				p, ok := pendingCalls[id]
				if !ok {
					p = &pendingToolCall{ID: id, Type: string(tc.Type)}
					pendingCalls[id] = p
					run.emit(ToolCallStartEvent{
						BaseEvent:    BaseEvent{Type: EventTypeToolCallStart, Timestamp: nowMillis()},
						ToolCallID:   id,
						ToolCallName: tc.Function.Name,
					})
				}
				if tc.Function.Name != "" {
					p.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					p.Arguments += tc.Function.Arguments
					run.emit(ToolCallArgsEvent{
						BaseEvent:  BaseEvent{Type: EventTypeToolCallArgs, Timestamp: nowMillis()},
						ToolCallID: id,
						Delta:      tc.Function.Arguments,
					})
				}
			}
		}
	}

	run.emit(TextMessageEndEvent{
		BaseEvent: BaseEvent{Type: EventTypeTextMessageEnd, Timestamp: nowMillis()},
		MessageID: messageID,
	})

	if len(pendingCalls) == 0 {
		// 普通 assistant 回复
		if contentBuf.Len() > 0 {
			run.appendMessages(openai.ChatCompletionMessage{
				Role:    "assistant",
				Content: contentBuf.String(),
			})
		}
		return false, nil
	}

	// 本轮产生了工具调用，构建 assistant 的 tool_calls 消息并追加
	toolCalls := make([]openai.ToolCall, 0, len(pendingCalls))
	aguiToolCalls := make([]AGUIToolCall, 0, len(pendingCalls))
	for _, p := range pendingCalls {
		toolCalls = append(toolCalls, openai.ToolCall{
			ID:   p.ID,
			Type: openai.ToolType(p.Type),
			Function: openai.FunctionCall{
				Name:      p.Name,
				Arguments: p.Arguments,
			},
		})
		aguiToolCalls = append(aguiToolCalls, AGUIToolCall{
			ID:   p.ID,
			Type: p.Type,
			Function: AGUIToolFunction{
				Name:      p.Name,
				Arguments: p.Arguments,
			},
		})
		run.emit(ToolCallEndEvent{
			BaseEvent:  BaseEvent{Type: EventTypeToolCallEnd, Timestamp: nowMillis()},
			ToolCallID: p.ID,
		})
	}

	run.appendMessages(openai.ChatCompletionMessage{
		Role:      "assistant",
		Content:   contentBuf.String(),
		ToolCalls: toolCalls,
	})

	run.emit(RunWaitingForToolResultsEvent{
		BaseEvent: BaseEvent{Type: EventTypeRunWaitingForToolResults, Timestamp: nowMillis()},
		ThreadID:  run.threadID,
		RunID:     run.runID,
		ToolCalls: aguiToolCalls,
	})

	return true, nil
}

// convertAGUIMessages 把 AG-UI 消息转为 OpenAI 消息格式
func convertAGUIMessages(msgs []AGUIMessage) []openai.ChatCompletionMessage {
	out := make([]openai.ChatCompletionMessage, 0, len(msgs))
	for _, msg := range msgs {
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

		om := openai.ChatCompletionMessage{
			Role:       msg.Role,
			Content:    content,
			Name:       msg.Name,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			tcs := make([]openai.ToolCall, 0, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				tcs = append(tcs, openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				})
			}
			om.ToolCalls = tcs
		}
		out = append(out, om)
	}
	return out
}

// convertAGUITools 把 AG-UI 工具定义转为 OpenAI 工具格式
func convertAGUITools(tools []AGUITool) []openai.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.Tool, 0, len(tools))
	for _, tool := range tools {
		paramsJSON, _ := json.Marshal(tool.Parameters)
		out = append(out, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  paramsJSON,
			},
		})
	}
	return out
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
