package config

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration wraps time.Duration for JSON serialization as a human-readable string (e.g. "30m", "5s").
type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Duration.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		var n float64
		if numErr := json.Unmarshal(b, &n); numErr != nil {
			return fmt.Errorf("duration must be a string (e.g. \"30m\") or number of seconds: %w", err)
		}
		d.Duration = time.Duration(n * float64(time.Second))
		return nil
	}
	if s == "" {
		d.Duration = 0
		return nil
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = parsed
	return nil
}

// Config is the top-level configuration for nanobot-eino.
type Config struct {
	Agent     AgentConfig                `json:"agent"`
	Model     ModelConfig                `json:"model"`
	Providers map[string]ProviderConfig  `json:"providers,omitempty"`
	Channels  ChannelsConfig             `json:"channels"`
	Gateway   GatewayConfig              `json:"gateway"`
	Tools     ToolsConfig                `json:"tools"`
	Data      DataConfig                 `json:"data"`
	Trace     TracingConfig              `json:"trace"`
}

type AgentConfig struct {
	PromptDir           string  `json:"promptDir"`
	BuiltinSkillsDir    string  `json:"builtinSkillsDir"`
	ContextWindowTokens int     `json:"contextWindowTokens"`
	MaxStep             int     `json:"maxStep"`
	MaxTokens           int     `json:"maxTokens,omitempty"`
	Temperature         float64 `json:"temperature,omitempty"`
	ReasoningEffort     string  `json:"reasoningEffort,omitempty"`
	Provider            string  `json:"provider,omitempty"` // "auto" or explicit provider name
	Model               string  `json:"model,omitempty"`    // model name; overrides model.model
}

// ModelConfig is the legacy single-provider configuration.
// Kept for backward compatibility; new deployments should use Providers map.
type ModelConfig struct {
	Type    string `json:"type"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
}

// ProviderConfig holds credentials and endpoint for an LLM provider.
type ProviderConfig struct {
	APIKey       string            `json:"apiKey,omitempty"`
	APIBase      string            `json:"apiBase,omitempty"`
	ExtraHeaders map[string]string `json:"extraHeaders,omitempty"`
}

type ChannelsConfig struct {
	SendProgress  bool                `json:"sendProgress,omitempty"`
	SendToolHints bool                `json:"sendToolHints,omitempty"`
	Feishu        FeishuChannelConfig `json:"feishu"`
	Extra         map[string]any      `json:"extra,omitempty"`
}

type FeishuChannelConfig struct {
	AppID             string `json:"appId"`
	AppSecret         string `json:"appSecret"`
	VerificationToken string `json:"verificationToken"`
	EncryptKey        string `json:"encryptKey"`
	AllowFrom         []string `json:"allowFrom,omitempty"`
	GroupPolicy       string   `json:"groupPolicy,omitempty"`
}

type GatewayConfig struct {
	Heartbeat HeartbeatConfig   `json:"heartbeat"`
	Cron      CronGatewayConfig `json:"cron"`
}

type HeartbeatConfig struct {
	Enabled  *bool    `json:"enabled,omitempty"`
	Path     string   `json:"path"`
	Interval Duration `json:"interval"`
}

// IsEnabled returns whether heartbeat is enabled (defaults to true when not explicitly set).
func (h *HeartbeatConfig) IsEnabled() bool {
	if h.Enabled == nil {
		return true
	}
	return *h.Enabled
}

type CronGatewayConfig struct {
	StorePath string `json:"storePath"`
}

type ToolsConfig struct {
	Workspace           string      `json:"workspace"`
	RestrictToWorkspace bool        `json:"restrictToWorkspace"`
	ExtraReadDirs       []string    `json:"extraReadDirs,omitempty"`
	Web                 WebConfig   `json:"web"`
	Exec                ExecConfig  `json:"exec"`
	MCP                 []MCPConfig `json:"mcp,omitempty"`
}

type WebConfig struct {
	Proxy  string          `json:"proxy,omitempty"`
	Search WebSearchConfig `json:"search"`
}

type WebSearchConfig struct {
	Provider   string `json:"provider"`
	APIKey     string `json:"apiKey,omitempty"`
	BaseURL    string `json:"baseUrl,omitempty"`
	MaxResults int    `json:"maxResults,omitempty"`
}

type ExecConfig struct {
	Timeout       Duration `json:"timeout"`
	MaxOutput     int      `json:"maxOutput,omitempty"`
	DenyPatterns  []string `json:"denyPatterns,omitempty"`
	AllowPatterns []string `json:"allowPatterns,omitempty"`
	PathAppend    string   `json:"pathAppend,omitempty"`
}

type MCPConfig struct {
	Name         string            `json:"name"`
	Type         string            `json:"type,omitempty"`
	Command      string            `json:"command,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	URL          string            `json:"url,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	ToolTimeout  Duration          `json:"toolTimeout,omitempty"`
	EnabledTools []string          `json:"enabledTools,omitempty"`
}

// DataConfig is kept for backward compatibility with existing config files.
// New code should use config.GetSessionsDir() / config.GetMemoryDir() instead.
type DataConfig struct {
	Dir       string `json:"dir"`
	MemoryDir string `json:"memoryDir"`
}

// TracingConfig holds Langfuse tracing settings.
type TracingConfig struct {
	Enabled   bool   `json:"enabled"`
	Endpoint  string `json:"endpoint"`
	PublicKey string `json:"publicKey"`
	SecretKey string `json:"secretKey"`
}

// WorkspacePath returns the resolved workspace path under ~/.nanobot-eino/.
func (c *Config) WorkspacePath() string {
	return GetWorkspacePath(c.Tools.Workspace)
}

// ResolveSessionsDir returns the sessions directory.
// Uses Data.Dir from config if explicitly set, otherwise ~/.nanobot-eino/sessions/.
func (c *Config) ResolveSessionsDir() string {
	if c.Data.Dir != "" {
		return ensureDir(expandHome(c.Data.Dir))
	}
	return GetSessionsDir()
}

// ResolveMemoryDir returns the memory directory.
// Uses Data.MemoryDir from config if explicitly set, otherwise ~/.nanobot-eino/memory/.
func (c *Config) ResolveMemoryDir() string {
	if c.Data.MemoryDir != "" {
		return ensureDir(expandHome(c.Data.MemoryDir))
	}
	return GetMemoryDir()
}

// ResolveCronStorePath returns the cron job store file path.
func (c *Config) ResolveCronStorePath() string {
	if c.Gateway.Cron.StorePath != "" {
		return expandHome(c.Gateway.Cron.StorePath)
	}
	return GetCronStorePath()
}

// ResolvePromptDir returns the prompt directory.
func (c *Config) ResolvePromptDir() string {
	if c.Agent.PromptDir != "" {
		return ensureDir(expandHome(c.Agent.PromptDir))
	}
	return GetPromptsDir()
}

// ResolveSkillsDir returns the builtin skills directory.
func (c *Config) ResolveSkillsDir() string {
	if c.Agent.BuiltinSkillsDir != "" {
		return ensureDir(expandHome(c.Agent.BuiltinSkillsDir))
	}
	return GetSkillsDir()
}

// GetProvider returns the ProviderConfig for the given provider name.
// Falls back to constructing one from the legacy Model section.
func (c *Config) GetProvider(name string) (ProviderConfig, bool) {
	if len(c.Providers) > 0 {
		if p, ok := c.Providers[name]; ok {
			return p, true
		}
	}
	if c.Model.APIKey != "" && (name == "" || name == c.Model.Type) {
		return ProviderConfig{
			APIKey:  c.Model.APIKey,
			APIBase: c.Model.BaseURL,
		}, true
	}
	return ProviderConfig{}, false
}

// GetAPIKey returns the API key for the given provider (or the legacy key).
func (c *Config) GetAPIKey(providerName string) string {
	p, ok := c.GetProvider(providerName)
	if ok {
		return p.APIKey
	}
	return c.Model.APIKey
}

// GetAPIBase returns the API base URL for the given provider.
func (c *Config) GetAPIBase(providerName string) string {
	p, ok := c.GetProvider(providerName)
	if ok && p.APIBase != "" {
		return p.APIBase
	}
	return c.Model.BaseURL
}

// EffectiveModel returns the model name.
// Priority: Agent.Model > Model.Model > "gpt-4o".
func (c *Config) EffectiveModel() string {
	if c.Agent.Model != "" {
		return c.Agent.Model
	}
	if c.Model.Model != "" {
		return c.Model.Model
	}
	return "gpt-4o"
}

// EffectiveProviderName returns the provider name to use. Checks Agent.Provider
// first ("auto" means auto-detect), then falls back to Model.Type.
func (c *Config) EffectiveProviderName() string {
	if c.Agent.Provider != "" && c.Agent.Provider != "auto" {
		return c.Agent.Provider
	}
	if c.Model.Type != "" {
		return c.Model.Type
	}
	return "openai"
}
