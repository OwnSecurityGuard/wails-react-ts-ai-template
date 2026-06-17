import React, { useState, useRef, useEffect, useCallback } from 'react';
import {
  Input,
  Button,
  Typography,
  Tooltip,
} from 'antd';
import {
  SendOutlined,
  StopOutlined,
  ClearOutlined,
  RobotOutlined,
  UserOutlined,
  SettingOutlined,
} from '@ant-design/icons';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAIChat } from '../../hooks/useAIChat';
import type { ChatMessage } from '../../types/chat';
import styled from 'styled-components';

const { Text } = Typography;

const ChatContainer = styled.div`
  display: flex;
  flex-direction: column;
  height: 100%;
  background: transparent;
`;

const MessagesArea = styled.div`
  flex: 1;
  overflow-y: auto;
  padding: 16px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
`;

const MessageRow = styled.div<{ $isUser: boolean }>`
  display: flex;
  justify-content: ${props => (props.$isUser ? 'flex-end' : 'flex-start')};
  gap: 12px;
  align-items: flex-start;
`;

const Avatar = styled.div<{ $isUser: boolean }>`
  width: 36px;
  height: 36px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  background: ${props =>
    props.$isUser
      ? 'var(--ant-primary-color, #1890ff)'
      : 'rgba(128, 128, 128, 0.15)'};
  color: ${props => (props.$isUser ? '#fff' : 'inherit')};
`;

const MessageBubble = styled.div<{ $isUser: boolean }>`
  max-width: 70%;
  padding: 12px 16px;
  border-radius: 12px;
  background: ${props =>
    props.$isUser
      ? 'var(--ant-primary-color, #1890ff)'
      : 'rgba(128, 128, 128, 0.08)'};
  color: ${props => (props.$isUser ? '#fff' : 'inherit')};
  word-break: break-word;
  line-height: 1.6;

  pre {
    background: rgba(0, 0, 0, 0.06);
    padding: 12px;
    border-radius: 6px;
    overflow-x: auto;
  }

  code {
    font-family: 'Fira Code', monospace;
    font-size: 0.9em;
  }

  p {
    margin: 0 0 8px;
    &:last-child {
      margin-bottom: 0;
    }
  }

  ul,
  ol {
    margin: 0 0 8px;
    padding-left: 20px;
  }

  blockquote {
    margin: 0 0 8px;
    padding-left: 12px;
    border-left: 3px solid rgba(128, 128, 128, 0.3);
    color: rgba(128, 128, 128, 0.8);
  }

  table {
    border-collapse: collapse;
    width: 100%;
    margin-bottom: 8px;
  }

  th,
  td {
    border: 1px solid rgba(128, 128, 128, 0.2);
    padding: 6px 10px;
  }

  th {
    background: rgba(128, 128, 128, 0.08);
  }
`;

const InputArea = styled.div`
  padding: 12px 24px 24px;
  border-top: 1px solid rgba(128, 128, 128, 0.1);
  display: flex;
  gap: 8px;
  align-items: flex-end;
`;

const StyledInput = styled(Input.TextArea)`
  border-radius: 8px;
  resize: none;
`;

const ActionBar = styled.div`
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 24px;
  border-bottom: 1px solid rgba(128, 128, 128, 0.1);
`;

const EmptyState = styled.div`
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: rgba(128, 128, 128, 0.6);
`;

const TypingIndicator = styled.div`
  display: flex;
  gap: 4px;
  padding: 8px 16px;

  span {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--ant-primary-color, #1890ff);
    opacity: 0.4;
    animation: bounce 1.4s infinite ease-in-out both;
  }

  span:nth-child(1) {
    animation-delay: -0.32s;
  }
  span:nth-child(2) {
    animation-delay: -0.16s;
  }

  @keyframes bounce {
    0%, 80%, 100% {
      transform: scale(0);
    }
    40% {
      transform: scale(1);
    }
  }
`;

/**
 * 渲染单条消息内容
 */
const MessageContent: React.FC<{ msg: ChatMessage }> = ({ msg }) => {
  if (msg.role === 'user') {
    return <Text style={{ color: '#fff' }}>{msg.content}</Text>;
  }

  if (!msg.content) {
    return (
      <TypingIndicator>
        <span />
        <span />
        <span />
      </TypingIndicator>
    );
  }

  return (
    <ReactMarkdown remarkPlugins={[remarkGfm]}>
      {msg.content}
    </ReactMarkdown>
  );
};

/**
 * AI 聊天页面（基于 Wails 方法调用）
 */
const AIChat: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const {
    session,
    isStreaming,
    sendMessage,
    stopGeneration,
    clearSession,
  } = useAIChat();

  const [inputValue, setInputValue] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<any>(null);

  // 自动滚动到底部
  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => {
    scrollToBottom();
  }, [session.messages, scrollToBottom]);

  const handleSend = async () => {
    if (!inputValue.trim() || isStreaming) return;
    const content = inputValue.trim();
    setInputValue('');
    await sendMessage(content);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleClear = () => {
    clearSession();
    setInputValue('');
    inputRef.current?.focus();
  };

  const hasMessages = session.messages.length > 0;

  return (
    <ChatContainer>
      <ActionBar>
        <Text strong style={{ fontSize: 16 }}>
          {t('ai.chat')}
        </Text>
        <div style={{ display: 'flex', gap: 8 }}>
          <Tooltip title={t('ai.settings')}>
            <Button
              icon={<SettingOutlined />}
              onClick={() => navigate('/settings')}
            />
          </Tooltip>
          {hasMessages && (
            <Tooltip title={t('ai.clear')}>
              <Button
                icon={<ClearOutlined />}
                onClick={handleClear}
                disabled={isStreaming}
              />
            </Tooltip>
          )}
        </div>
      </ActionBar>

      {hasMessages ? (
        <MessagesArea>
          {session.messages.map((msg, index) => (
            <MessageRow key={index} $isUser={msg.role === 'user'}>
              {msg.role !== 'user' && (
                <Avatar $isUser={false}>
                  <RobotOutlined />
                </Avatar>
              )}
              <MessageBubble $isUser={msg.role === 'user'}>
                <MessageContent msg={msg} />
              </MessageBubble>
              {msg.role === 'user' && (
                <Avatar $isUser={true}>
                  <UserOutlined />
                </Avatar>
              )}
            </MessageRow>
          ))}
          <div ref={messagesEndRef} />
        </MessagesArea>
      ) : (
        <EmptyState>
          <RobotOutlined style={{ fontSize: 48, opacity: 0.3 }} />
          <Text type="secondary">{t('ai.emptyState')}</Text>
        </EmptyState>
      )}

      <InputArea>
        <StyledInput
          ref={inputRef}
          value={inputValue}
          onChange={e => setInputValue(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={t('ai.inputPlaceholder')}
          autoSize={{ minRows: 1, maxRows: 6 }}
          disabled={isStreaming}
          style={{ flex: 1 }}
        />
        {isStreaming ? (
          <Button
            type="primary"
            danger
            icon={<StopOutlined />}
            onClick={stopGeneration}
          >
            {t('ai.stop')}
          </Button>
        ) : (
          <Button
            type="primary"
            icon={<SendOutlined />}
            onClick={handleSend}
            disabled={!inputValue.trim()}
          >
            {t('ai.send')}
          </Button>
        )}
      </InputArea>
    </ChatContainer>
  );
};

export default AIChat;
