package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
)

// AIProvider 表示 AI 服务提供商类型
type AIProvider string

const (
	ProviderOpenAI   AIProvider = "openai"
	ProviderDeepSeek AIProvider = "deepseek"
	ProviderGemini   AIProvider = "gemini"
	ProviderCustom   AIProvider = "custom"
)

// ProviderPreset 定义各提供商的预设参数
type ProviderPreset struct {
	Name        string `json:"name"`
	BaseURL     string `json:"base_url"`
	DefaultModel string `json:"default_model"`
}

// ProviderPresets 返回所有支持的提供商预设
func ProviderPresets() map[AIProvider]ProviderPreset {
	return map[AIProvider]ProviderPreset{
		ProviderOpenAI: {
			Name:         "OpenAI",
			BaseURL:      "https://api.openai.com/v1",
			DefaultModel: "gpt-4o-mini",
		},
		ProviderDeepSeek: {
			Name:         "DeepSeek",
			BaseURL:      "https://api.deepseek.com/v1",
			DefaultModel: "deepseek-chat",
		},
		ProviderGemini: {
			Name:         "Gemini",
			BaseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
			DefaultModel: "gemini-1.5-flash",
		},
		ProviderCustom: {
			Name:         "Custom",
			BaseURL:      "",
			DefaultModel: "",
		},
	}
}

// AIConfig 存储 AI 服务配置
type AIConfig struct {
	Provider    AIProvider        `json:"provider"`
	APIKey      string            `json:"api_key"`
	BaseURL     string            `json:"base_url"`
	Model       string            `json:"model"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature"`
	TopP        float64           `json:"top_p"`
	Thinking    *bool             `json:"thinking,omitempty"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

// DefaultAIConfig 返回默认配置
func DefaultAIConfig() AIConfig {
	return AIConfig{
		Provider:    ProviderOpenAI,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o-mini",
		MaxTokens:   4096,
		Temperature: 0.7,
		TopP:        1.0,
	}
}

// ConfigStore 管理 AI 配置的持久化存储
type ConfigStore struct {
	mu     sync.RWMutex
	config AIConfig
	path   string
}

// NewConfigStore 创建配置存储实例
func NewConfigStore() *ConfigStore {
	configDir := filepath.Join(xdg.ConfigHome, "wails-react-ts-ai-template")
	_ = os.MkdirAll(configDir, 0755)
	path := filepath.Join(configDir, "ai_config.json")

	store := &ConfigStore{
		path: path,
	}
	store.load()
	return store
}

// load 从文件加载配置
func (s *ConfigStore) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		s.config = DefaultAIConfig()
		return
	}

	var cfg AIConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		s.config = DefaultAIConfig()
		return
	}
	s.config = cfg
}

// Save 保存配置到文件
func (s *ConfigStore) Save(cfg AIConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	s.config = cfg
	return nil
}

// Get 获取当前配置副本
func (s *ConfigStore) Get() AIConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}
