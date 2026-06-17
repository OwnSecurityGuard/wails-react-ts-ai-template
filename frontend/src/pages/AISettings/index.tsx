import React, { useState, useEffect } from 'react';
import {
  Form,
  Input,
  Select,
  Slider,
  Button,
  Card,
  Typography,
  Space,
  InputNumber,
  message as antMessage,
  Spin,
} from 'antd';
import { SaveOutlined, ArrowLeftOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { useAIChat } from '../../hooks/useAIChat';
import type { AIProvider } from '../../types/chat';

const { Title } = Typography;
const { Option } = Select;

/**
 * AI 设置页面
 * 配置 API Key、模型、温度等参数
 */
const AISettings: React.FC = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const { getConfig, saveConfig } = useAIChat();

  const [form] = Form.useForm();
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [, setProvider] = useState<AIProvider>('openai');

  useEffect(() => {
    getConfig()
      .then(config => {
        form.setFieldsValue({
          provider: config.provider,
          api_key: config.api_key,
          base_url: config.base_url,
          model: config.model,
          max_tokens: config.max_tokens,
          temperature: config.temperature,
        });
        setProvider(config.provider);
        setLoading(false);
      })
      .catch(() => {
        setLoading(false);
      });
  }, [getConfig, form]);

  const handleProviderChange = (value: AIProvider) => {
    setProvider(value);
    // 根据提供商设置推荐的默认值
    const defaults: Record<AIProvider, { base_url: string; model: string }> = {
      openai: { base_url: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
      deepseek: { base_url: 'https://api.deepseek.com/v1', model: 'deepseek-chat' },
      gemini: { base_url: 'https://generativelanguage.googleapis.com/v1beta/openai', model: 'gemini-1.5-flash' },
      custom: { base_url: '', model: '' },
    };

    const def = defaults[value];
    if (def) {
      form.setFieldsValue({
        base_url: def.base_url,
        model: def.model,
      });
    }
  };

  const handleSave = async (values: {
    provider: AIProvider;
    api_key: string;
    base_url: string;
    model: string;
    max_tokens: number;
    temperature: number;
    top_p?: number;
  }) => {
    setSaving(true);
    try {
      await saveConfig({ ...values, top_p: values.top_p ?? 1.0 });
      antMessage.success(t('ai.configSaved'));
    } catch (err) {
      antMessage.error(t('ai.configError'));
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        <Spin size="large" />
      </div>
    );
  }

  return (
    <div style={{ padding: '24px', maxWidth: 720, margin: '0 auto' }}>
      <Space style={{ marginBottom: 24 }}>
        <Button icon={<ArrowLeftOutlined />} onClick={() => navigate('/ai-chat')}>
          {t('common.back')}
        </Button>
        <Title level={4} style={{ margin: 0 }}>
          {t('ai.settings')}
        </Title>
      </Space>

      <Card>
        <Form
          form={form}
          layout="vertical"
          onFinish={handleSave}
          initialValues={{
            provider: 'openai',
            max_tokens: 4096,
            temperature: 0.7,
          }}
        >
          <Form.Item
            name="provider"
            label={t('ai.provider')}
            rules={[{ required: true }]}
          >
            <Select onChange={handleProviderChange}>
              <Option value="openai">OpenAI</Option>
              <Option value="deepseek">DeepSeek</Option>
              <Option value="gemini">Gemini</Option>
              <Option value="custom">Custom</Option>
            </Select>
          </Form.Item>

          <Form.Item
            name="api_key"
            label={t('ai.apiKey')}
            rules={[{ required: true, message: t('ai.noApiKey') }]}
          >
            <Input.Password placeholder="sk-..." />
          </Form.Item>

          <Form.Item
            name="base_url"
            label={t('ai.baseURL')}
            rules={[{ required: true }]}
          >
            <Input placeholder="https://api.example.com/v1" />
          </Form.Item>

          <Form.Item
            name="model"
            label={t('ai.model')}
            rules={[{ required: true }]}
          >
            <Input placeholder="gpt-4o-mini" />
          </Form.Item>

          <Form.Item
            name="max_tokens"
            label={t('ai.maxTokens')}
            rules={[
              { required: true },
              { type: 'number', min: 1, message: t('ai.maxTokensMin') },
            ]}
          >
            <InputNumber min={1} max={32768} style={{ width: '100%' }} />
          </Form.Item>

          <Form.Item
            name="temperature"
            label={
              <span>
                {t('ai.temperature')}
                <span style={{ marginLeft: 8, color: '#999' }}>
                  (0 - 2)
                </span>
              </span>
            }
            rules={[{ required: true }]}
          >
            <Slider
              min={0}
              max={2}
              step={0.1}
              marks={{
                0: '0',
                0.7: '0.7',
                1: '1',
                2: '2',
              }}
            />
          </Form.Item>

          <Form.Item style={{ marginBottom: 0, textAlign: 'right' }}>
            <Button type="primary" htmlType="submit" icon={<SaveOutlined />} loading={saving}>
              {t('common.save')}
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
};

export default AISettings;
