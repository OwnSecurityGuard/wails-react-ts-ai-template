import React, { useMemo, useState } from 'react';
import { Card, Row, Col, Statistic, Input, Button, List, Space } from 'antd';
import { useTranslation } from 'react-i18next';
import { useCopilotAgent, AgentAction } from '../../hooks/useCopilotAgent';

const { TextArea } = Input;

interface HomeStats {
  total: number;
  active: number;
  completed: number;
}

const Home: React.FC = () => {
  const { t } = useTranslation();
  const [stats, setStats] = useState<HomeStats>({ total: 11, active: 3, completed: 8 });
  const [input, setInput] = useState('');

  /** 定义 AI 可调用的动作，用于修改页面状态 */
  const actions = useMemo<AgentAction[]>(
    () => [
      {
        name: 'updateHomeStats',
        description: '修改首页统计卡片的数值',
        parameters: {
          type: 'object',
          properties: {
            total: { type: 'number', description: '总任务数' },
            active: { type: 'number', description: '进行中任务数' },
            completed: { type: 'number', description: '已完成任务数' },
          },
          required: ['total', 'active', 'completed'],
        },
        handler: (args) => {
          const next: HomeStats = {
            total: Number(args.total) ?? stats.total,
            active: Number(args.active) ?? stats.active,
            completed: Number(args.completed) ?? stats.completed,
          };
          setStats(next);
          return `已更新首页统计为：总任务 ${next.total}，进行中 ${next.active}，已完成 ${next.completed}`;
        },
      },
      {
        name: 'getCurrentStats',
        description: '获取当前首页统计数值',
        parameters: {
          type: 'object',
          properties: {},
        },
        handler: () => {
          return `当前统计：总任务 ${stats.total}，进行中 ${stats.active}，已完成 ${stats.completed}`;
        },
      },
    ],
    [stats]
  );

  const { messages, isLoading, sendMessage } = useCopilotAgent(actions);

  const handleSend = async () => {
    if (!input.trim() || isLoading) return;
    const text = input.trim();
    setInput('');
    await sendMessage(text);
  };

  return (
    <div className="home-container" style={{ padding: 24, height: '100%', overflow: 'auto' }}>
      <h1>{t('pages.home.title')}</h1>
      <Row gutter={[16, 16]}>
        <Col span={8}>
          <Card>
            <Statistic
              title={t('pages.home.totalTasks')}
              value={stats.total}
              valueStyle={{ color: '#3f8600' }}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic
              title={t('pages.home.activeTasks')}
              value={stats.active}
              valueStyle={{ color: '#cf1322' }}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic
              title={t('pages.home.completedTasks')}
              value={stats.completed}
              valueStyle={{ color: '#1890ff' }}
            />
          </Card>
        </Col>
      </Row>

      <Card title="CopilotKit Agent" style={{ marginTop: 24 }}>
        <Space direction="vertical" style={{ width: '100%' }}>
          <List
            bordered
            style={{ maxHeight: 300, overflow: 'auto', background: 'var(--bg-color)' }}
            dataSource={messages}
            renderItem={(msg) => (
              <List.Item>
                <div style={{ whiteSpace: 'pre-wrap' }}>
                  <strong>{msg.role}: </strong>
                  {msg.content}
                  {msg.toolCalls && (
                    <div style={{ color: '#888', fontSize: 12, marginTop: 4 }}>
                      调用工具：{msg.toolCalls.map((tc) => tc.function.name).join(', ')}
                    </div>
                  )}
                </div>
              </List.Item>
            )}
          />
          <TextArea
            rows={2}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="例如：把总任务改成 20，进行中改成 5"
            onPressEnter={(e) => {
              if (!e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
          />
          <Button type="primary" onClick={handleSend} loading={isLoading} disabled={!input.trim()}>
            发送
          </Button>
        </Space>
      </Card>
    </div>
  );
};

export default Home;
