/**
 * AI 聊天相关类型定义
 * 与 Go 后端结构保持同步
 */

/** 聊天消息角色 */
export type ChatRole = 'system' | 'user' | 'assistant';

/** 单条聊天消息 */
export interface ChatMessage {
  role: ChatRole;
  content: string;
}

/** 流式响应数据块 */
export interface StreamChunk {
  session_id: string;
  content: string;
  done: boolean;
  error?: string;
}

/** AI 服务提供商 */
export type AIProvider = 'openai' | 'deepseek' | 'gemini' | 'custom';

/** 提供商预设 */
export interface ProviderPreset {
  name: string;
  base_url: string;
  default_model: string;
}

/** AI 配置 */
export interface AIConfig {
  provider: AIProvider;
  api_key: string;
  base_url: string;
  model: string;
  max_tokens: number;
  temperature: number;
  top_p: number;
  thinking?: boolean;
  custom_headers?: Record<string, string>;
}

/** 测试连接结果 */
export interface TestConnectionResult {
  success: boolean;
  message: string;
}

/** 聊天会话 */
export interface ChatSession {
  id: string;
  title: string;
  messages: ChatMessage[];
  createdAt: number;
  updatedAt: number;
}
