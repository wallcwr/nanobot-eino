package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/peterh/liner"
	"github.com/spf13/cobra"
	"github.com/wall/nanobot-eino/pkg/agent"
	"github.com/wall/nanobot-eino/pkg/app"
	"github.com/wall/nanobot-eino/pkg/config"
	"github.com/wall/nanobot-eino/pkg/cron"
	"github.com/wall/nanobot-eino/pkg/memory"
	"github.com/wall/nanobot-eino/pkg/session"
	"github.com/wall/nanobot-eino/pkg/tools"
	"github.com/wall/nanobot-eino/pkg/trace"
	"github.com/wall/nanobot-eino/pkg/workspace"
)

func newAgentCmd() *cobra.Command {
	var message string
	var raw bool

	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Chat with the agent directly",
		Long:  "Start an interactive chat session, or send a single message with -m.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgent(message, raw)
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "send a single message and exit")
	cmd.Flags().BoolVar(&raw, "raw", false, "output raw text without markdown rendering")

	return cmd
}

func runAgent(message string, raw bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := app.NewSignalChannel()
	go func() {
		<-sigCh
		cancel()
	}()

	cfg := mustLoadConfig()

	traceShutdown, err := trace.Init(cfg.Trace)
	if err != nil {
		return fmt.Errorf("init tracing: %w", err)
	}
	defer traceShutdown()

	promptDir := cfg.ResolvePromptDir()
	skillsDir := cfg.ResolveSkillsDir()

	if err := workspace.SyncTemplates(promptDir); err != nil {
		slog.Warn("Template sync failed", "error", err)
	}

	memStore, err := memory.NewMemoryStore(cfg.ResolveMemoryDir())
	if err != nil {
		return fmt.Errorf("init memory: %w", err)
	}

	sessionMgr, err := session.NewSessionManager(cfg.ResolveSessionsDir())
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	modelCfg := app.BuildModelConfig(cfg)

	sessionID := "cli:user-1"
	toolCfg := buildCLIToolConfig(cfg, sessionID)

	cronSvc := cron.NewCronService(cfg.ResolveCronStorePath(), func(ctx context.Context, job *cron.CronJob) error {
		slog.Info("Cron job triggered", "module", "cli", "job", job.Name)
		return nil
	})
	if err := cronSvc.Start(ctx); err != nil {
		slog.Warn("Cron service start failed", "error", err)
	}
	defer cronSvc.Stop()

	bot, err := agent.NewAgent(ctx, modelCfg, toolCfg, memStore, promptDir,
		skillsDir, cronSvc, sessionMgr, cfg.Agent.ContextWindowTokens, cfg.Agent.MaxStep, nil)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	var renderer *glamour.TermRenderer
	if !raw {
		renderer, err = glamour.NewTermRenderer(glamour.WithAutoStyle())
		if err != nil {
			slog.Warn("Markdown renderer unavailable, using raw output", "error", err)
		}
	}

	if message != "" {
		return handleSingleMessage(ctx, bot, sessionID, message, renderer)
	}

	return runInteractive(ctx, bot, sessionID, renderer)
}

func handleSingleMessage(ctx context.Context, bot *agent.Agent, sessionID, input string, renderer *glamour.TermRenderer) error {
	response, err := collectResponse(ctx, bot, sessionID, input)
	if err != nil {
		return err
	}
	printResponse(response, renderer)
	return nil
}

func runInteractive(ctx context.Context, bot *agent.Agent, sessionID string, renderer *glamour.TermRenderer) error {
	line := liner.NewLiner()
	defer line.Close()
	line.SetCtrlCAborts(true)

	historyPath := config.GetCLIHistoryPath()
	if f, err := os.Open(historyPath); err == nil {
		line.ReadHistory(f)
		f.Close()
	}
	defer func() {
		_ = os.MkdirAll(filepath.Dir(historyPath), 0755)
		if f, err := os.Create(historyPath); err == nil {
			line.WriteHistory(f)
			f.Close()
		}
	}()

	fmt.Println("Nanobot Eino (type 'exit' or Ctrl-D to quit)")
	fmt.Println()

	for {
		input, err := line.Prompt("> ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				continue
			}
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}
		if strings.ToLower(input) == "exit" {
			break
		}

		line.AppendHistory(input)

		spin := startSpinner()
		response, chatErr := collectResponse(ctx, bot, sessionID, input)
		spin.Stop()

		if chatErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", chatErr)
			continue
		}

		printResponse(response, renderer)
	}

	return nil
}

func collectResponse(ctx context.Context, bot *agent.Agent, sessionID, input string) (string, error) {
	reader, err := bot.ChatStream(ctx, sessionID, input)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var buf strings.Builder
	for {
		msg, recvErr := reader.Recv()
		if recvErr != nil {
			if recvErr != io.EOF {
				return buf.String(), recvErr
			}
			break
		}
		buf.WriteString(msg.Content)
	}
	return buf.String(), nil
}

func printResponse(text string, renderer *glamour.TermRenderer) {
	if text == "" {
		return
	}
	if renderer != nil {
		if rendered, err := renderer.Render(text); err == nil {
			fmt.Print(rendered)
			return
		}
	}
	fmt.Println(text)
}

func parseSessionTarget(sessionID string) (string, string) {
	if strings.Contains(sessionID, ":") {
		parts := strings.SplitN(sessionID, ":", 2)
		return parts[0], parts[1]
	}
	return "cli", sessionID
}

func buildCLIToolConfig(cfg *config.Config, sessionID string) tools.ToolConfig {
	defaultChannel, defaultChatID := parseSessionTarget(sessionID)
	toolCfg := tools.ToolConfig{
		Workspace:           cfg.WorkspacePath(),
		RestrictToWorkspace: cfg.Tools.RestrictToWorkspace,
		ExtraReadDirs:       cfg.Tools.ExtraReadDirs,
		DefaultChannel:      defaultChannel,
		DefaultChatID:       defaultChatID,
	}
	toolCfg.Web.Search = tools.WebSearchConfig{
		Provider:   cfg.Tools.Web.Search.Provider,
		APIKey:     cfg.Tools.Web.Search.APIKey,
		BaseURL:    cfg.Tools.Web.Search.BaseURL,
		MaxResults: cfg.Tools.Web.Search.MaxResults,
	}
	if strings.TrimSpace(toolCfg.Web.Search.Provider) == "" {
		toolCfg.Web.Search.Provider = "tavily"
	}
	toolCfg.Exec = tools.ShellConfig{
		Timeout:       cfg.Tools.Exec.Timeout.Duration,
		MaxOutput:     cfg.Tools.Exec.MaxOutput,
		DenyPatterns:  cfg.Tools.Exec.DenyPatterns,
		AllowPatterns: cfg.Tools.Exec.AllowPatterns,
		PathAppend:    cfg.Tools.Exec.PathAppend,
	}
	return toolCfg
}

type thinkingSpinner struct {
	stop chan struct{}
	done sync.WaitGroup
}

func startSpinner() *thinkingSpinner {
	s := &thinkingSpinner{stop: make(chan struct{})}
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Fprintf(os.Stderr, "\r\033[K")
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s thinking...", frames[i%len(frames)])
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return s
}

func (s *thinkingSpinner) Stop() {
	close(s.stop)
	s.done.Wait()
}
