# Nanobot-Eino

基于 Golang 和 [Cloudwego Eino](https://github.com/cloudwego/eino) 框架的 AI Agent 个人助手。

---

## 功能概览

- **ReAct Agent Loop** — 基于 Eino React Agent 实现多步推理 + 工具调用循环
- **持久化记忆** — Token 感知的自动记忆整理，MEMORY.md + HISTORY.md 双层存储
- **多渠道接入** — 飞书 WebSocket 实时通信，可扩展其他渠道
- **丰富工具集** — 文件系统、Shell、Web 搜索/抓取、MCP 协议、定时任务、消息发送
- **子任务系统** — 后台 Subagent 独立执行长耗时任务，完成后自动通知
- **定时任务** — Cron / 一次性定时任务，自然语言创建和管理
- **心跳唤醒** — 定期读取 HEARTBEAT.md，LLM 自主决定是否行动
- **技能扩展** — 8 个内建技能，支持从Clawhub安装，支持 always-on 和按需加载
- **MCP 延迟连接** — MCP 服务器首次收到消息时才建立连接，避免启动阻塞
- **Prompt Cache 友好** — Append-only 消息列表 + 静态 System Prompt，最大化 LLM 缓存命中

---

## 架构

```
┌──────────────────────────────────────────────────────────────────┐
│                     Channels (飞书 WebSocket)                     │
│                  onMessage → PublishInbound                       │
└──────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│                        MessageBus                                │
│            Inbound → Agent.ChatStream                            │
│            Outbound → Channel.Send                               │
└──────────────────────────────────────────────────────────────────┘
                                │
                                ▼
┌──────────────────────────────────────────────────────────────────┐
│                    Agent (Eino React)                             │
│  Session → Memory Consolidation → Prompt Building → LLM Stream   │
└──────────────────────────────────────────────────────────────────┘
        │              │              │              │
        ▼              ▼              ▼              ▼
   ┌─────────┐  ┌───────────┐  ┌──────────┐  ┌────────────┐
   │ Session  │  │  Memory   │  │   Cron   │  │ Heartbeat  │
   │ (JSONL)  │  │ (MD文件)  │  │ (调度器)  │  │  (定期)    │
   └─────────┘  └───────────┘  └──────────┘  └────────────┘
```

---

## 快速开始

### 1) 环境要求

- Go `1.25+`（`go.mod` 当前为 `go 1.25.6`）
- 至少一个可用模型 Provider 的 API Key（本地 Ollama 可无 Key），模型支持OpenAI / 通义千问 / 硅基流动 / 火山方舟 / Google Gemini / Ollama / Deepseek / Claude / Openrouter / Qianfan

### 2) 拉取与安装

```bash
git clone https://github.com/wall/nanobot-eino.git
cd nanobot-eino
go mod download
```

### 3) 初始化

```bash
go run ./cmd/nanobot onboard
```

会创建 `~/.nanobot-eino/` 下的配置与运行目录（config、sessions、memory、cron、prompts、skills 等）。

### 4) 配置模型

编辑 `~/.nanobot-eino/config.yaml`，至少配置一个 provider 的 `apiKey` 和你要使用的模型名。

### 5) 运行

```bash
# 交互式 CLI
go run ./cmd/nanobot agent

# 单轮消息，直接在终端与 Agent 对话，支持历史记录、Markdown 渲染
go run ./cmd/nanobot agent -m "hello"

# 启动网关（飞书 + agent + heartbeat + cron）
go run ./cmd/nanobot gateway
```
![feishu](/case/channel.gif)

## CLI 命令

| 命令 | 说明 |
|---|---|
| `go run ./cmd/nanobot onboard` | 初始化配置与运行目录 |
| `go run ./cmd/nanobot agent` | 交互式聊天 |
| `go run ./cmd/nanobot agent -m "..."` | 单轮聊天 |
| `go run ./cmd/nanobot agent --raw` | 不做 Markdown 渲染，直接输出原文 |
| `go run ./cmd/nanobot gateway` | 启动网关（飞书 + 心跳 + 定时任务） |
| `go run ./cmd/nanobot status` | 输出当前生效配置与路径信息 |
| `go run ./cmd/nanobot version` | 查看版本 |

Agent 对话内置命令：

- `/new`：新会话并归档旧会话记忆
- `/stop`：停止当前会话任务（含该会话子任务）
- `/restart`：重启当前进程
- `/help`：显示命令帮助

## 配置说明

默认配置文件：`~/.nanobot-eino/config.yaml`（也支持 JSON / TOML）。

### 配置示例（YAML）

```yaml
agent:
  provider: "openai"    # auto / openai / azure_openai / anthropic / deepseek / openrouter / qianfan / ark / gemini / ollama ...
  model: "gpt-4o"
  contextWindowTokens: 65536
  maxStep: 20
  maxTokens: 8192
  temperature: 0.1
  reasoningEffort: "medium" # low / medium / high（按模型支持情况生效）
  
providers:
  openai:
    apiKey: "sk-..."
    apiBase: "https://api.openai.com/v1"
  deepseek:
    apiKey: "sk-..."
    apiBase: "https://api.deepseek.com/v1"
  anthropic:
    apiKey: "sk-ant-..."
  openrouter:
    apiKey: "sk-or-v1-..."
  qianfan:
    apiKey: "your-access-key"
    apiSecret: "your-secret-key"
  azure_openai:
    apiKey: "your-azure-key"
    apiBase: "https://<resource>.openai.azure.com"

channels:
  feishu:
    appId: "cli_xxx"
    appSecret: "xxx"
    allowFrom: ["ou_xxx"] # “*” 为全部允许
    groupPolicy: "mention" # mention / open

tools:
  workspace: "~/.nanobot-eino/workspace"
  restrictToWorkspace: false
  web:
    search:
      provider: tavily # brave / tavily / searxng / jina / duckduckgo
      apiKey: "tvly-..."
      maxResults: 5
  exec:
    timeout: "60s"
  mcp:
    - name: filesystem
      command: npx
      args: ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]

gateway:
  heartbeat:
    enabled: true
    path: "HEARTBEAT.md" # 相对路径时，相对于进程当前工作目录
    interval: "30m"
  cron:
    storePath: "~/.nanobot-eino/cron/jobs.json"
```

Provider 路由说明：

- `agent.provider` 为显式值时优先使用显式 provider。
- `agent.provider=auto` 时，按模型前缀/关键词、`apiBase` 特征、已配置 Key 自动匹配。
- `azure_openai` 通过 `apiBase` 包含 `.openai.azure.com` 自动走 Azure 模式。
- `qianfan` 必须同时提供 `apiKey` 与 `apiSecret`。

Google Gemini 配置示例（`config.yaml`）：

```yaml
agent:
  provider: gemini
  model: gemini-2.5-flash
providers:
  gemini:
    apiKey: "your-google-api-key"
```

硅基流动（SiliconFlow）配置示例（`config.yaml`）：

```yaml
agent:
  provider: siliconflow
  model: "Qwen/Qwen3-8B"
providers:
  siliconflow:
  # 需要在配置中显式填写 SiliconFlow OpenAI 兼容地址
  apiBase: "https://api.siliconflow.cn/v1"
  apiKey: "sk-xxxx"
```

Qianfan（文心一言）配置示例（`config.yaml`）：

```yaml
agent:
  provider: qianfan
  model: ernie-4.0-8k
providers:
  qianfan:
    apiKey: "your-access-key"
    apiSecret: "your-secret-key"
```

Azure OpenAI 配置示例（`config.yaml`）：

```yaml
agent:
  provider: azure_openai
  model: gpt-4o
providers:
  azure_openai:
    apiKey: "your-azure-key"
    apiBase: "https://<resource>.openai.azure.com"
```

环境变量覆盖（`NANOBOT_` 前缀）始终可用。

## 工具系统

## 工具

| 工具 | 功能 |
|------|------|
| `read_file` / `write_file` / `edit_file` / `list_dir` | 文件读写和目录浏览 |
| `shell` | 执行 Shell 命令（可配置超时和黑白名单） |
| `web_search` | Web 搜索（brave/tavily/searxng/jina/duckduckgo） |
| `web_fetch` | 抓取网页并转为 Markdown |
| `message` | 通过 MessageBus 向渠道发送消息 |
| `cron` | 创建 / 删除 / 列出定时任务 |
| `spawn` | 启动后台 Subagent 子任务 |
| MCP | 通过 Model Context Protocol 接入外部工具 |

---

## 技能系统

Skill 系统支持 8 个内建技能：

| 技能 | 说明 | 依赖 |
|------|------|------|
| memory | 双层记忆系统（MEMORY.md + HISTORY.md） | — (always-on) |
| weather | 天气查询（wttr.in + Open-Meteo fallback） | curl |
| summarize | URL/文件/视频摘要 | summarize CLI |
| skill-creator | 创建/更新 Agent 技能 | — |
| github | 通过 gh CLI 操作 GitHub | gh |
| cron | 定时提醒和周期任务 | — |
| tmux | 远程控制 tmux 会话 | tmux |
| clawhub | 从 ClawHub 搜索安装技能 | — |

技能加载优先级：workspace (`{workspace}/skills/`) > builtin (`configs/skills/`)。

技能以 SKILL.md 文件定义，包含 YAML frontmatter（name、description、metadata）和 Markdown 正文。`always: true` 的技能全文注入 system prompt；其他技能以 XML 摘要注入，agent 按需用 `read_file` 加载。

---

## 记忆与会话

- **MEMORY.md** — 长期记忆，由 LLM 使用 `save_memory` 工具整理，每次覆写
- **HISTORY.md** — 对话历史摘要日志，只追加不修改
- 超出阈值后自动触发 consolidation，保留关键信息并压缩历史上下文

整理策略：
- Token 估算超过上下文窗口 50% 时自动触发
- 在 user 消息边界处切割，不破坏对话轮次
- 多轮整理（最多 5 轮），目标压缩到窗口一半
- 连续 3 次 LLM 整理失败自动降级为原始归档

---

## 项目结构

```
nanobot-eino/
├── cmd/
│   ├── nanobot/             # Cobra CLI 入口
│   │   ├── main.go          #   根命令
│   │   ├── gateway.go       #   gateway 子命令
│   │   ├── agent.go         #   agent 子命令
│   │   ├── onboard.go       #   onboard 子命令
│   │   └── status.go        #   status 子命令
│   ├── server/main.go       # 独立服务入口（legacy）
│   └── cli/main.go          # 简易 REPL（legacy）
├── pkg/
│   ├── agent/               # Agent Loop（核心）
│   ├── bus/                  # MessageBus（入站/出站路由）
│   ├── channels/            # 渠道适配（飞书）
│   ├── config/              # 配置加载（Viper）
│   ├── cron/                # 定时任务（robfig/cron）
│   ├── heartbeat/           # 心跳服务
│   ├── memory/              # 记忆存储 + 整理器
│   ├── model/               # LLM 模型工厂
│   ├── prompt/              # Prompt 模板加载
│   ├── session/             # 会话管理（JSONL）
│   ├── skill/               # 技能管理器
│   ├── subagent/            # 子任务管理器
│   ├── tools/               # 工具实现 + 封装
│   └── workspace/           # Workspace 模板同步
├── configs/
│   ├── prompts/             # 默认 Prompt（SOUL / USER / TOOLS / AGENTS / HEARTBEAT）
│   └── skills/              # 内建技能（memory / weather / summarize / skill-creator / github / cron / tmux / clawhub）
├── scripts/
│   ├── start-langfuse.sh    # Langfuse 一键启动（Linux/macOS）
│   ├── start-langfuse.ps1   # Langfuse 一键启动（Windows）
│   ├── stop-langfuse.sh     # Langfuse 停止（Linux/macOS）
│   └── stop-langfuse.ps1    # Langfuse 停止（Windows）
├── data/                    # 运行时数据（sessions / memory / jobs）
├── go.mod
├── Makefile                 # make langfuse-up / langfuse-down / langfuse-logs
├── Dockerfile
└── note.md                  # 设计笔记
```

---

## 核心依赖

| 依赖 | 用途 |
|------|------|
| [cloudwego/eino](https://github.com/cloudwego/eino) | AI 应用框架（React Agent / Schema / Compose） |
| eino-ext/model/* | LLM 模型扩展（OpenAI / Claude / DeepSeek / OpenRouter / Qianfan / Ollama / Ark / Gemini） |
| eino-ext/tool/mcp | MCP 工具集成 |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI 框架 |
| [spf13/viper](https://github.com/spf13/viper) | 配置管理 |
| [charmbracelet/glamour](https://github.com/charmbracelet/glamour) | 终端 Markdown 渲染 |
| [peterh/liner](https://github.com/peterh/liner) | 终端行编辑 / 历史记录 |
| [robfig/cron](https://github.com/robfig/cron) | Cron 调度器 |
| [larksuite/oapi-sdk-go](https://github.com/larksuite/oapi-sdk-go) | 飞书 SDK |
| [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) | MCP 协议 SDK |

---

## Docker

```bash
docker build -t nanobot-eino .

# 初始化（首次）
docker run --rm -it \
  -v ~/.nanobot-eino:/root/.nanobot-eino \
  nanobot-eino onboard

# 交互式 agent
docker run --rm -it \
  -v ~/.nanobot-eino:/root/.nanobot-eino \
  nanobot-eino agent

# gateway / status
docker run --rm -it \
  -v ~/.nanobot-eino:/root/.nanobot-eino \
  nanobot-eino gateway
docker run --rm -it \
  -v ~/.nanobot-eino:/root/.nanobot-eino \
  nanobot-eino status
```

---

## 链路监控（Tracing）
![langfuse链路追踪](/case/langfuse.png)

基于 Eino 框架的全局 Callback 机制，集成 [Langfuse](https://langfuse.com) 实现完整链路追踪，覆盖 LLM 调用、工具执行、记忆整理全流程。

### 启动 Langfuse

项目提供一键启动脚本，自动检测并安装 Docker，适用于 Linux / macOS / Windows：

**Linux / macOS：**

```bash
# 通过 make
make langfuse-up

# 或直接运行脚本
./scripts/start-langfuse.sh
```

**Windows (PowerShell)：**

```powershell
.\scripts\start-langfuse.ps1
```

脚本会自动完成以下步骤：
1. 检测 Docker 是否安装，未安装则提示自动安装（Linux 用 `get.docker.com`，macOS 用 `brew`，Windows 用 `winget`/`choco`）
2. 检测 Docker daemon 是否运行，未运行则自动启动
3. 启动所有 Langfuse 依赖服务（PostgreSQL、ClickHouse、Redis、MinIO）
4. 等待 Langfuse 就绪并输出访问地址

停止服务：

```bash
# Linux / macOS
make langfuse-down   # 或 ./scripts/stop-langfuse.sh

# Windows
.\scripts\stop-langfuse.ps1
```

查看日志：

```bash
make langfuse-logs
```

等待服务启动后访问 `http://localhost:3000`，注册账号并在 Settings → API Keys 创建密钥。

### 配置

在 `~/.nanobot-eino/config.yaml` 中添加：

```yaml
trace:
  enabled: true
  endpoint: "http://localhost:3000"
  publicKey: "pk-lf-..."
  secretKey: "sk-lf-..."
```

启用条件：`enabled=true` 且 `endpoint/publicKey/secretKey` 均非空。

仓库中提供了本地部署文件：`docker-compose.langfuse.yml`。

打开 Langfuse UI (`http://localhost:3000`) → Traces 页面，可以看到：

- **LLM 调用**：模型名称、输入/输出消息、Token 用量（prompt/completion/total）、延迟
- **工具执行**：工具名称、入参 JSON、返回结果、延迟、错误信息
- **记忆整理**：consolidation 触发时机、处理消息数、成功/失败状态

每条 Trace 关联 Session ID 和 User ID，支持按会话、用户筛选。

### 关闭 Tracing

设置 `trace.enabled: false`（或不配置 trace 段）即可完全关闭，零运行时开销。

### 架构

```
Nanobot-Eino
  │
  ├── Eino Global Callback ──▶ Langfuse Handler
  │     (自动追踪所有 ChatModel / Tool)
  │
  ├── pkg/trace/
  │     ├── trace.go   # Init/Shutdown，全局 handler 注册
  │     └── span.go    # 手动埋点辅助（用于记忆整理等非 Eino 组件）
  │
  └── docker-compose.langfuse.yml
        ├── Langfuse Web UI (:3000)
        ├── PostgreSQL
        ├── ClickHouse
        ├── Redis
        └── MinIO
```

## 致谢

- [OpenClaw](https://github.com/openclaw/openclaw)
- [nanobot (Python)](https://github.com/HKUDS/nanobot)
- [CloudWeGo Eino](https://github.com/cloudwego/eino)
