import { useState, useCallback, useRef, useEffect } from 'react';
import { Call, Events } from '@wailsio/runtime';
import type {
  ChatMessage,
  ChatSession,
  StreamChunk,
  AIConfig,
  AIProvider,
  ProviderPreset,
  TestConnectionResult,
} from '../types/chat';

interface UseAIChatReturn {
  /** 当前会话 */
  session: ChatSession;
  /** 是否正在流式输出中 */
  isStreaming: boolean;
  /** 发送消息 */
  sendMessage: (content: string) => Promise<void>;
  /** 停止生成 */
  stopGeneration: () => void;
  /** 清空会话 */
  clearSession: () => void;
  /** 获取 AI 配置 */
  getConfig: () => Promise<AIConfig>;
  /** 保存 AI 配置 */
  saveConfig: (config: AIConfig) => Promise<void>;
  /** 获取提供商预设 */
  getProviderPresets: () => Promise<Record<AIProvider, ProviderPreset>>;
  /** 测试连接 */
  testConnection: (config: AIConfig) => Promise<TestConnectionResult>;
}

function generateSessionId(): string {
  return `session_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`;
}

function createNewSession(): ChatSession {
  const now = Date.now();
  return {
    id: generateSessionId(),
    title: '新对话',
    messages: [],
    createdAt: now,
    updatedAt: now,
  };
}

/**
 * AI 聊天 Hook
 * 管理聊天状态，通过 Wails Events 接收流式响应
 */
export function useAIChat(): UseAIChatReturn {
  const [session, setSession] = useState<ChatSession>(createNewSession);
  const [isStreaming, setIsStreaming] = useState(false);
  const sessionRef = useRef(session);
  const unsubscribeRef = useRef<(() => void) | null>(null);

  // 保持 ref 同步
  useEffect(() => {
    sessionRef.current = session;
  }, [session]);

  // 组件卸载时清理事件监听
  useEffect(() => {
    return () => {
      if (unsubscribeRef.current) {
        unsubscribeRef.current();
      }
    };
  }, []);

  const stopGeneration = useCallback(() => {
    const currentSession = sessionRef.current;
    Call.ByName('main.AIService.StopStream', currentSession.id).catch(() => {
      // 忽略停止错误
    });
    setIsStreaming(false);
  }, []);

  const sendMessage = useCallback(async (content: string) => {
    if (!content.trim() || isStreaming) return;

    const currentSession = sessionRef.current;

    // 添加用户消息
    const userMessage: ChatMessage = { role: 'user', content: content.trim() };
    const updatedMessages = [...currentSession.messages, userMessage];

    setSession(prev => ({
      ...prev,
      messages: updatedMessages,
      updatedAt: Date.now(),
    }));

    // 添加助手占位消息
    const assistantMessage: ChatMessage = { role: 'assistant', content: '' };
    const messagesWithAssistant = [...updatedMessages, assistantMessage];

    setSession(prev => ({
      ...prev,
      messages: messagesWithAssistant,
    }));

    setIsStreaming(true);

    // 清理旧的事件监听
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }

    // 注册新的事件监听
    const unsubscribe = Events.On('ai:stream:chunk', (event) => {
      const chunk = event.data as StreamChunk;
      if (chunk.session_id !== currentSession.id) return;

      if (chunk.error) {
        setSession(prev => {
          const msgs = [...prev.messages];
          const lastIndex = msgs.length - 1;
          if (lastIndex >= 0 && msgs[lastIndex].role === 'assistant') {
            msgs[lastIndex] = { ...msgs[lastIndex], content: `Error: ${chunk.error}` };
          }
          return { ...prev, messages: msgs };
        });
        setIsStreaming(false);
        return;
      }

      if (chunk.done) {
        setIsStreaming(false);
        return;
      }

      setSession(prev => {
        const msgs = [...prev.messages];
        const lastIndex = msgs.length - 1;
        if (lastIndex >= 0 && msgs[lastIndex].role === 'assistant') {
          msgs[lastIndex] = {
            ...msgs[lastIndex],
            content: msgs[lastIndex].content + chunk.content,
          };
        }
        return { ...prev, messages: msgs, updatedAt: Date.now() };
      });
    });

    unsubscribeRef.current = unsubscribe;

    try {
      // 调用 Go 后端启动流式聊天
      await Call.ByName('main.AIService.ChatStream', currentSession.id, updatedMessages);
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : String(err);
      setSession(prev => {
        const msgs = [...prev.messages];
        const lastIndex = msgs.length - 1;
        if (lastIndex >= 0 && msgs[lastIndex].role === 'assistant') {
          msgs[lastIndex] = { ...msgs[lastIndex], content: `Error: ${errorMsg}` };
        }
        return { ...prev, messages: msgs };
      });
      setIsStreaming(false);
    }
  }, [isStreaming]);

  const clearSession = useCallback(() => {
    stopGeneration();
    setSession(createNewSession());
  }, [stopGeneration]);

  const getConfig = useCallback(async (): Promise<AIConfig> => {
    return Call.ByName('main.AIService.GetConfig') as Promise<AIConfig>;
  }, []);

  const saveConfig = useCallback(async (config: AIConfig): Promise<void> => {
    await Call.ByName('main.AIService.SaveConfig', config);
  }, []);

  const getProviderPresets = useCallback(async (): Promise<Record<AIProvider, ProviderPreset>> => {
    return Call.ByName('main.AIService.GetProviderPresets') as Promise<Record<AIProvider, ProviderPreset>>;
  }, []);

  const testConnection = useCallback(async (config: AIConfig): Promise<TestConnectionResult> => {
    return Call.ByName('main.AIService.TestConnection', config) as Promise<TestConnectionResult>;
  }, []);

  return {
    session,
    isStreaming,
    sendMessage,
    stopGeneration,
    clearSession,
    getConfig,
    saveConfig,
    getProviderPresets,
    testConnection,
  };
}
