import { useCallback, useRef, useState } from 'react';

const RUNTIME_URL = 'http://localhost:18999/api/copilotkit';

/** 工具调用描述 */
export interface AgentToolCall {
  id: string;
  type: string;
  function: {
    name: string;
    arguments: string;
  };
}

/** Agent 可调用的动作定义 */
export interface AgentAction {
  /** 动作名称，对应 function calling 的 name */
  name: string;
  /** 动作描述，会传给 LLM 决定何时调用 */
  description: string;
  /** JSON Schema 参数定义 */
  parameters: Record<string, unknown>;
  /** 执行函数，返回字符串结果给 LLM */
  handler: (args: Record<string, unknown>) => Promise<string> | string;
}

/** Agent 消息，用于前端展示与传给 runtime */
export interface AgentMessage {
  id?: string;
  role: 'user' | 'assistant' | 'system' | 'tool';
  content: string;
  name?: string;
  toolCallId?: string;
  toolCalls?: AgentToolCall[];
}

/** 工具执行结果 */
interface ToolResult {
  toolCallId: string;
  name: string;
  result: string;
}

/** 运行时 SSE 事件 */
interface RuntimeEvent {
  type: string;
  timestamp?: number;
  // RUN_WAITING_FOR_TOOL_RESULTS
  threadId?: string;
  runId?: string;
  toolCalls?: AgentToolCall[];
  // TEXT_MESSAGE_*
  messageId?: string;
  delta?: string;
  // RUN_ERROR
  error?: { code: string; message: string };
}

/**
 * 连接本地 CopilotKit Runtime 的自定义 Agent Hook。
 *
 * 特点：
 * - 自己维护消息历史与 SSE 连接
 * - 支持 function calling：LLM 返回 tool_calls 后自动执行本地 action
 * - 通过 /run/{threadId}/tool-result 续跑，实现多轮 Agent 循环
 *
 * 注意：当前与标准 @copilotkit/react-core 的 useCopilotAction 不互通，
 * 因为标准组件期望 runtime 内部完成工具执行闭环；这里把执行逻辑放在前端。
 */
export function useCopilotAgent(initialActions: AgentAction[] = []) {
  const [messages, setMessages] = useState<AgentMessage[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const actionsRef = useRef<AgentAction[]>(initialActions);

  /** 注册/覆盖一个可执行动作 */
  const registerAction = useCallback((action: AgentAction) => {
    actionsRef.current = actionsRef.current.filter((a) => a.name !== action.name);
    actionsRef.current.push(action);
  }, []);

  /** 发送用户消息并启动/继续运行 */
  const sendMessage = useCallback(
    async (text: string) => {
      const nextMessages: AgentMessage[] = [...messages, { role: 'user', content: text }];
      setMessages(nextMessages);
      await startRun(nextMessages, actionsRef.current);
    },
    [messages]
  );

  /** 向 runtime 发起一次 run（SSE）并处理文本/工具事件 */
  const startRun = async (currentMessages: AgentMessage[], actions: AgentAction[]) => {
    setIsLoading(true);

    const threadId = `thread_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
    const runId = `run_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;

    const tools = actions.map((a) => ({
      name: a.name,
      description: a.description,
      parameters: a.parameters,
    }));

    try {
      const response = await fetch(`${RUNTIME_URL}/agent/default/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          threadId,
          runId,
          messages: currentMessages,
          tools,
        }),
      });

      if (!response.ok) {
        throw new Error(`Runtime returned ${response.status}`);
      }
      if (!response.body) {
        throw new Error('Response body is null');
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';
      let assistantContent = '';
      let pendingToolCalls: AgentToolCall[] = [];
      let runFinished = false;

      while (!runFinished) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (!line.startsWith('data: ')) continue;
          const payload = line.slice(6).trim();
          if (payload === '' || payload === '[DONE]') continue;

          let event: RuntimeEvent;
          try {
            event = JSON.parse(payload) as RuntimeEvent;
          } catch {
            continue;
          }

          switch (event.type) {
            case 'TEXT_MESSAGE_START':
              assistantContent = '';
              break;

            case 'TEXT_MESSAGE_CONTENT':
              if (event.delta) {
                assistantContent += event.delta;
                appendAssistantDelta(event.delta);
              }
              break;

            case 'TEXT_MESSAGE_END':
              // assistantContent 会在后续事件里用到
              break;

            case 'RUN_WAITING_FOR_TOOL_RESULTS':
              pendingToolCalls = event.toolCalls || [];
              // 先把带 tool_calls 的 assistant 消息写入历史
              setMessages((prev) => {
                const last = prev[prev.length - 1];
                if (last && last.role === 'assistant' && !last.toolCalls) {
                  return [
                    ...prev.slice(0, -1),
                    { ...last, content: assistantContent, toolCalls: pendingToolCalls },
                  ];
                }
                return [
                  ...prev,
                  { role: 'assistant', content: assistantContent, toolCalls: pendingToolCalls },
                ];
              });

              // 执行工具并回传结果；后端收到后会继续在同一条 SSE 流上输出
              await handleToolCalls(threadId, runId, pendingToolCalls, actions);
              break;

            case 'RUN_FINISHED':
              runFinished = true;
              break;

            case 'RUN_ERROR':
              throw new Error(event.error?.message || 'Agent run error');
          }
        }
      }
    } catch (err) {
      const errMsg = err instanceof Error ? err.message : String(err);
      setMessages((prev) => [...prev, { role: 'assistant', content: `错误：${errMsg}` }]);
    } finally {
      setIsLoading(false);
    }
  };

  /** 把文本增量追加到最后一条 assistant 消息 */
  const appendAssistantDelta = (delta: string) => {
    setMessages((prev) => {
      const last = prev[prev.length - 1];
      if (last && last.role === 'assistant' && !last.toolCalls) {
        return [...prev.slice(0, -1), { ...last, content: last.content + delta }];
      }
      return [...prev, { role: 'assistant', content: delta }];
    });
  };

  /** 执行工具调用并 POST 结果给 runtime */
  const handleToolCalls = async (
    threadId: string,
    runId: string,
    toolCalls: AgentToolCall[],
    actions: AgentAction[]
  ) => {
    const results: ToolResult[] = [];
    const toolResultMessages: AgentMessage[] = [];

    for (const tc of toolCalls) {
      const action = actions.find((a) => a.name === tc.function.name);
      let result = '';
      if (action) {
        try {
          const args = JSON.parse(tc.function.arguments || '{}') as Record<string, unknown>;
          const r = await action.handler(args);
          result = String(r);
        } catch (e) {
          result = `执行失败：${e instanceof Error ? e.message : String(e)}`;
        }
      } else {
        result = `未找到动作：${tc.function.name}`;
      }

      results.push({ toolCallId: tc.id, name: tc.function.name, result });
      toolResultMessages.push({
        role: 'tool',
        content: result,
        toolCallId: tc.id,
        name: tc.function.name,
      });
    }

    // 把 tool 结果也展示到消息列表
    setMessages((prev) => [...prev, ...toolResultMessages]);

    const res = await fetch(`${RUNTIME_URL}/agent/default/run/${threadId}/tool-result`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ threadId, runId, toolResults: results }),
    });

    if (!res.ok) {
      throw new Error(`提交工具结果失败：${res.status}`);
    }
  };

  return {
    messages,
    isLoading,
    sendMessage,
    registerAction,
  };
}
