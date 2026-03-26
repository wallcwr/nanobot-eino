# Provider Parity Design Spec

**Date:** 2026-03-26
**Goal:** Add 5 new LLM providers to nanobot-eino using eino-ext, achieving parity with Python nanobot's native provider support.

## Background

nanobot-eino's model factory (`pkg/model/model.go`) currently supports 4 `EinoType` values: `openai`, `ollama`, `ark`, `gemini`. Many OpenAI-compatible providers (SiliconFlow, Moonshot, Zhipu, etc.) already route through `EinoType: "openai"` via the provider registry.

However, 4 providers supported by eino-ext have dedicated SDKs with features that the generic OpenAI-compatible path cannot leverage:

| Provider | eino-ext Package | Key Advantage |
|---|---|---|
| Anthropic Claude | `components/model/claude` | Prompt caching, extended thinking, native message format |
| DeepSeek | `components/model/deepseek` | Native SDK, reasoning_content extraction |
| OpenRouter | `components/model/openrouter` | Multi-model fallback (`Models[]`), metadata |
| Baidu Qianfan | `components/model/qianfan` | Native Qianfan SDK (ERNIE models) |

Additionally, the existing OpenAI provider in eino-ext supports Azure OpenAI via `ByAzure` flag, which is not wired in nanobot-eino.

## Design

### Overview

```
config.yaml
    ↓
MatchProvider() → ProviderSpec.EinoType
    ↓
model.NewChatModel() switch on EinoType
    ↓
┌─────────────────────────────────────────────┐
│ "openai"     → openai.NewChatModel()        │  (existing, + Azure auto-detect)
│ "ollama"     → ollama.NewChatModel()        │  (existing)
│ "ark"        → ark.NewChatModel()           │  (existing)
│ "gemini"     → gemini.NewChatModel()        │  (existing)
│ "claude"     → claude.NewChatModel()        │  NEW
│ "deepseek"   → deepseek.NewChatModel()      │  NEW
│ "openrouter" → openrouter.NewChatModel()    │  NEW
│ "qianfan"    → qianfan.NewChatModel()       │  NEW
└─────────────────────────────────────────────┘
```

### 1. Provider Registry Changes (`pkg/config/providers.go`)

**Modify 3 existing entries** — change `EinoType` from `"openai"` to their native type:

- `anthropic`: `EinoType: "openai"` → `"claude"`
- `deepseek`: `EinoType: "openai"` → `"deepseek"`
- `openrouter`: `EinoType: "openai"` → `"openrouter"`

**Add 2 new entries:**

```go
{
    Name:        "qianfan",
    DisplayName: "Baidu Qianfan (ERNIE)",
    Keywords:    []string{"qianfan", "ernie", "wenxin"},
    EinoType:    "qianfan",
}
{
    Name:         "azure_openai",
    DisplayName:  "Azure OpenAI",
    Keywords:     []string{"azure"},
    EinoType:     "openai",
    DetectByBase: ".openai.azure.com",
}
```

### 2. Model Config Extension (`pkg/model/model.go`)

Add 3 fields to `model.Config` for agent-level parameter passthrough:

```go
type Config struct {
    Type            string
    BaseURL         string
    APIKey          string
    Model           string
    MaxTokens       int      // from AgentConfig; required by Claude
    Temperature     float64  // from AgentConfig
    ReasoningEffort string   // from AgentConfig; "low"/"medium"/"high"
}
```

These are generic parameters that multiple providers accept. They are passed through from `AgentConfig` via `BuildModelConfig()`.

### 3. Factory Switch Cases (`pkg/model/model.go`)

#### 3.1 Claude (Anthropic)

```go
case "claude":
    maxTokens := cfg.MaxTokens
    if maxTokens <= 0 {
        maxTokens = 8192
    }
    return claude.NewChatModel(ctx, &claude.Config{
        APIKey:    cfg.APIKey,
        Model:     cfg.Model,
        MaxTokens: maxTokens,
        BaseURL:   toStringPtr(cfg.BaseURL),
    })
```

- `MaxTokens` is **required** by Claude SDK. Default to 8192 if not configured.
- `BaseURL` is `*string` in Claude SDK — use helper `toStringPtr()` that returns nil for empty string.

#### 3.2 DeepSeek

```go
case "deepseek":
    dsCfg := &deepseek.ChatModelConfig{
        APIKey: cfg.APIKey,
        Model:  cfg.Model,
    }
    if cfg.BaseURL != "" {
        dsCfg.BaseURL = cfg.BaseURL
    }
    return deepseek.NewChatModel(ctx, dsCfg)
```

- `BaseURL` defaults to `https://api.deepseek.com/` in SDK if empty.

#### 3.3 OpenRouter

```go
case "openrouter":
    return openrouter.NewChatModel(ctx, &openrouter.Config{
        APIKey: cfg.APIKey,
        Model:  cfg.Model,
    })
```

- `BaseURL` defaults to `https://openrouter.ai/api/v1` in SDK.

#### 3.4 Qianfan (Baidu)

```go
case "qianfan":
    qfCfg := qianfan.GetQianfanSingletonConfig()
    qfCfg.AccessKey = cfg.APIKey
    return qianfan.NewChatModel(ctx, &qianfan.ChatModelConfig{
        Model: cfg.Model,
    })
```

- Qianfan uses a singleton config pattern. Credentials must be set on the global config before constructing the model.
- `cfg.APIKey` maps to Qianfan's `AccessKey`.

#### 3.5 Azure OpenAI (existing openai case enhancement)

```go
case "openai", "siliconflow", "silicon-flow":
    oaiCfg := &openai.ChatModelConfig{
        BaseURL: cfg.BaseURL,
        APIKey:  cfg.APIKey,
        Model:   cfg.Model,
    }
    if strings.Contains(cfg.BaseURL, ".openai.azure.com") {
        oaiCfg.ByAzure = true
        oaiCfg.APIVersion = "2024-08-01-preview"
    }
    return openai.NewChatModel(ctx, oaiCfg)
```

- Azure detection: if `BaseURL` contains `.openai.azure.com`, auto-enable Azure mode.
- `APIVersion` defaults to `"2024-08-01-preview"`.
- No changes to `ProviderConfig` schema — Azure users configure `apiBase` to their Azure endpoint.

### 4. BuildModelConfig Passthrough (`pkg/app/model.go`)

```go
return model.Config{
    Type:            spec.EinoType,
    BaseURL:         apiBase,
    APIKey:          provCfg.APIKey,
    Model:           cfg.EffectiveModel(),
    MaxTokens:       cfg.Agent.MaxTokens,
    Temperature:     cfg.Agent.Temperature,
    ReasoningEffort: cfg.Agent.ReasoningEffort,
}
```

### 5. Dependencies (`go.mod`)

New direct dependencies:

```
github.com/cloudwego/eino-ext/components/model/claude
github.com/cloudwego/eino-ext/components/model/deepseek
github.com/cloudwego/eino-ext/components/model/openrouter
github.com/cloudwego/eino-ext/components/model/qianfan
```

## Config Examples

### Claude

```yaml
agent:
  model: claude-sonnet-4-20250514
  maxTokens: 8192
providers:
  anthropic:
    apiKey: sk-ant-xxx
```

### DeepSeek

```yaml
agent:
  model: deepseek-chat
providers:
  deepseek:
    apiKey: sk-xxx
```

### OpenRouter

```yaml
agent:
  model: anthropic/claude-opus-4-5
  provider: openrouter
providers:
  openrouter:
    apiKey: sk-or-v1-xxx
```

### Qianfan (Baidu ERNIE)

```yaml
agent:
  model: ernie-4.0-8k
providers:
  qianfan:
    apiKey: your-access-key
```

### Azure OpenAI

```yaml
agent:
  model: gpt-4o
  provider: azure_openai
providers:
  azure_openai:
    apiKey: your-azure-key
    apiBase: https://myresource.openai.azure.com
```

## Files Changed

| File | Change | Lines |
|---|---|---|
| `pkg/config/providers.go` | Modify 3 EinoType + add 2 entries | ~20 |
| `pkg/model/model.go` | Extend Config + add 4 switch cases + enhance openai case | ~60 |
| `pkg/app/model.go` | Passthrough 3 new fields | ~3 |
| `go.mod` / `go.sum` | Add 4 eino-ext dependencies | `go get` |

**No changes to:** `pkg/config/schema.go`, `pkg/agent/`, `pkg/app/runloop.go`, or any other files.

## Testing

- Unit test: verify `MatchProvider()` returns correct EinoType for each new provider
- Unit test: verify `NewChatModel()` constructs without error for each new type (mock or skip network calls)
- Integration test: manual verification with real API keys for each provider
