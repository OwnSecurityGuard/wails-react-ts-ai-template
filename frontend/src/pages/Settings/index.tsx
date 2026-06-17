import React, { useState, useEffect } from 'react';
import {
  Form,
  Switch,
  Divider,
  message,
  Input,
  Slider,
  Button,
  Card,
  Space,
  InputNumber,
  Collapse,
  Tag,
} from 'antd';
import { useTranslation } from 'react-i18next';
import {
  BellOutlined,
  PoweroffOutlined,
  RobotOutlined,
  ApiOutlined,
  ExperimentOutlined,
  LinkOutlined,
  KeyOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
} from '@ant-design/icons';
import styled from 'styled-components';
import { useAIChat } from '../../hooks/useAIChat';
import type { AIConfig, AIProvider, ProviderPreset } from '../../types/chat';

const { Panel } = Collapse;

// 样式组件
const SettingsContainer = styled.div`
  max-width: 800px;
  margin: 0 auto;
  padding: 24px;

  @media (max-width: 768px) {
    max-width: 100%;
    padding: 16px;
  }
`;

const SettingsSection = styled.div`
  padding: 16px 0;

  h2 {
    font-size: 18px;
    margin-bottom: 24px;
    color: var(--ant-primary-7);
    display: flex;
    align-items: center;
    gap: 8px;

    .anticon {
      font-size: 20px;
    }
  }
`;

const StyledFormItem = styled(Form.Item)`
  margin-bottom: 16px;
  max-width: 400px;

  .ant-form-item-label > label {
    font-weight: 500;
    color: var(--ant-primary-6);
  }

  @media (max-width: 768px) {
    max-width: 100%;
  }
`;

const StyledDivider = styled(Divider)`
  margin: 8px 0;
`;

const ProviderCard = styled.div<{ $active: boolean }>`
  padding: 12px 16px;
  border-radius: 8px;
  border: 2px solid ${props => (props.$active ? 'var(--ant-primary-color)' : 'rgba(128, 128, 128, 0.15)')};
  cursor: pointer;
  transition: all 0.2s;
  display: flex;
  align-items: center;
  gap: 8px;

  &:hover {
    border-color: var(--ant-primary-color);
  }
`;

const TestResultTag = styled(Tag)<{ $success?: boolean }>`
  margin-left: 8px;
`;

/**
 * 设置页面
 * 包含通知、系统、AI 配置
 */
const Settings: React.FC = () => {
  const { t } = useTranslation();
  const [form] = Form.useForm();
  const { getConfig, saveConfig, getProviderPresets, testConnection } = useAIChat();

  const [aiForm] = Form.useForm();
  const [aiLoading, setAiLoading] = useState(true);
  const [aiSaving, setAiSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<{ success: boolean; message: string } | null>(null);
  const [providerPresets, setProviderPresets] = useState<Record<AIProvider, ProviderPreset>>({} as Record<AIProvider, ProviderPreset>);
  const [selectedProvider, setSelectedProvider] = useState<AIProvider>('openai');

  // 加载 AI 配置和预设
  useEffect(() => {
    Promise.all([getConfig(), getProviderPresets()])
      .then(([config, presets]) => {
        aiForm.setFieldsValue({
          provider: config.provider,
          api_key: config.api_key,
          base_url: config.base_url,
          model: config.model,
          max_tokens: config.max_tokens,
          temperature: config.temperature,
          top_p: config.top_p ?? 1.0,
          thinking: config.thinking ?? false,
          custom_headers: config.custom_headers
            ? Object.entries(config.custom_headers).map(([key, value]) => ({ key, value }))
            : [],
        });
        setSelectedProvider(config.provider);
        setProviderPresets(presets);
        setAiLoading(false);
      })
      .catch(() => {
        setAiLoading(false);
      });
  }, [getConfig, getProviderPresets, aiForm]);

  const handleSettingChange = (setting: string, value: boolean) => {
    message.success(t('settings.saved'));
  };

  const handleProviderChange = (value: AIProvider) => {
    setSelectedProvider(value);
    const preset = providerPresets[value];
    if (preset) {
      aiForm.setFieldsValue({
        base_url: preset.base_url,
        model: preset.default_model,
      });
    }
  };

  const handleTestConnection = async () => {
    const values = aiForm.getFieldsValue();
    if (!values.api_key) {
      message.warning(t('ai.noApiKey'));
      return;
    }
    setTesting(true);
    setTestResult(null);
    try {
      const result = await testConnection(values as AIConfig);
      setTestResult(result);
      if (result.success) {
        message.success(result.message);
      } else {
        message.error(result.message);
      }
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      setTestResult({ success: false, message: msg });
      message.error(msg);
    } finally {
      setTesting(false);
    }
  };

  const handleSaveAI = async (values: {
    provider: AIProvider;
    api_key: string;
    base_url: string;
    model: string;
    max_tokens: number;
    temperature: number;
    top_p: number;
    thinking?: boolean;
    custom_headers?: { key: string; value: string }[];
  }) => {
    setAiSaving(true);
    try {
      const config: AIConfig = {
        provider: values.provider,
        api_key: values.api_key,
        base_url: values.base_url,
        model: values.model,
        max_tokens: values.max_tokens,
        temperature: values.temperature,
        top_p: values.top_p,
        thinking: values.thinking,
        custom_headers: values.custom_headers
          ? Object.fromEntries(
              values.custom_headers
                .filter((h) => h.key && h.value)
                .map((h) => [h.key, h.value])
            )
          : undefined,
      };
      await saveConfig(config);
      message.success(t('ai.configSaved'));
    } catch (err) {
      message.error(t('ai.configError'));
    } finally {
      setAiSaving(false);
    }
  };

  return (
    <SettingsContainer>
      {/* AI 配置 Section */}
      <SettingsSection>
        <h2>
          <RobotOutlined /> {t('ai.settings')}
        </h2>
        {aiLoading ? (
          <Card loading />
        ) : (
          <Form
            form={aiForm}
            layout="vertical"
            onFinish={handleSaveAI}
            initialValues={{
              provider: 'openai',
              max_tokens: 4096,
              temperature: 0.7,
              top_p: 1.0,
              thinking: false,
            }}
          >
            {/* Provider 选择卡片 */}
            <StyledFormItem name="provider" label={t('ai.provider')} rules={[{ required: true }]}>
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(140px, 1fr))', gap: 12 }}>
                {(Object.entries(providerPresets) as [AIProvider, ProviderPreset][]).map(([key, preset]) => (
                  <ProviderCard
                    key={key}
                    $active={selectedProvider === key}
                    onClick={() => {
                      aiForm.setFieldsValue({ provider: key });
                      handleProviderChange(key);
                    }}
                  >
                    <ApiOutlined />
                    <span>{preset.name}</span>
                  </ProviderCard>
                ))}
              </div>
            </StyledFormItem>

            <StyledFormItem
              name="api_key"
              label={
                <Space>
                  <KeyOutlined />
                  {t('ai.apiKey')}
                </Space>
              }
              rules={[{ required: true, message: t('ai.noApiKey') }]}
            >
              <Input.Password placeholder="sk-..." />
            </StyledFormItem>

            <StyledFormItem
              name="base_url"
              label={
                <Space>
                  <LinkOutlined />
                  {t('ai.baseURL')}
                </Space>
              }
              rules={[{ required: true }]}
            >
              <Input placeholder="https://api.example.com/v1" />
            </StyledFormItem>

            <StyledFormItem name="model" label={t('ai.model')} rules={[{ required: true }]}>
              <Input placeholder="gpt-4o-mini" />
            </StyledFormItem>

            <Collapse ghost style={{ marginBottom: 16 }}>
              <Panel header={<Space><ExperimentOutlined />{t('common.more')}</Space>} key="1">
                <StyledFormItem name="max_tokens" label={t('ai.maxTokens')} rules={[{ required: true }]}>
                  <InputNumber min={1} max={32768} style={{ width: '100%' }} />
                </StyledFormItem>

                <StyledFormItem
                  name="temperature"
                  label={
                    <span>
                      {t('ai.temperature')}
                      <span style={{ marginLeft: 8, color: '#999' }}>(0 - 2)</span>
                    </span>
                  }
                  rules={[{ required: true }]}
                >
                  <Slider min={0} max={2} step={0.1} marks={{ 0: '0', 0.7: '0.7', 1: '1', 2: '2' }} />
                </StyledFormItem>

                <StyledFormItem
                  name="top_p"
                  label={
                    <span>
                      Top P
                      <span style={{ marginLeft: 8, color: '#999' }}>(0 - 1)</span>
                    </span>
                  }
                  rules={[{ required: true }]}
                >
                  <Slider min={0} max={1} step={0.05} marks={{ 0: '0', 0.5: '0.5', 1: '1' }} />
                </StyledFormItem>

                <StyledFormItem name="thinking" valuePropName="checked" label="Thinking Mode">
                  <Switch />
                </StyledFormItem>

                <Form.List name="custom_headers">
                  {(fields, { add, remove }) => (
                    <div>
                      {fields.map((field) => (
                        <Space key={field.key} style={{ display: 'flex', marginBottom: 8 }} align="baseline">
                          <Form.Item {...field} name={[field.name, 'key']} rules={[{ required: true }]} noStyle>
                            <Input placeholder="Header Key" />
                          </Form.Item>
                          <Form.Item {...field} name={[field.name, 'value']} rules={[{ required: true }]} noStyle>
                            <Input placeholder="Header Value" />
                          </Form.Item>
                          <Button type="link" danger onClick={() => remove(field.name)}>
                            {t('common.delete')}
                          </Button>
                        </Space>
                      ))}
                      <Button type="dashed" onClick={() => add()} block>
                        + Add Custom Header
                      </Button>
                    </div>
                  )}
                </Form.List>
              </Panel>
            </Collapse>

            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <Space>
                <Button onClick={handleTestConnection} loading={testing} icon={<ApiOutlined />}>
                  Test Connection
                </Button>
                {testResult && (
                  <TestResultTag
                    icon={testResult.success ? <CheckCircleOutlined /> : <CloseCircleOutlined />}
                    color={testResult.success ? 'success' : 'error'}
                    $success={testResult.success}
                  >
                    {testResult.message}
                  </TestResultTag>
                )}
              </Space>
              <Button type="primary" htmlType="submit" loading={aiSaving}>
                {t('common.save')}
              </Button>
            </div>
          </Form>
        )}
      </SettingsSection>

      <StyledDivider />

      {/* 通知设置 Section */}
      <SettingsSection>
        <h2>
          <BellOutlined /> {t('settings.notifications')}
        </h2>
        <Form form={form} layout="vertical">
          <StyledFormItem label={t('settings.enableNotifications')} name="notifications">
            <Switch defaultChecked onChange={(checked) => handleSettingChange('notifications', checked)} />
          </StyledFormItem>
        </Form>
      </SettingsSection>

      <StyledDivider />

      {/* 系统设置 Section */}
      <SettingsSection>
        <h2>
          <PoweroffOutlined /> {t('settings.system')}
        </h2>
        <Form form={form} layout="vertical">
          <StyledFormItem label={t('settings.autoStart')} name="autoStart">
            <Switch onChange={(checked) => handleSettingChange('autoStart', checked)} />
          </StyledFormItem>
        </Form>
      </SettingsSection>
    </SettingsContainer>
  );
};

export default Settings;
