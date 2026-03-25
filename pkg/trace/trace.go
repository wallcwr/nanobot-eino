package trace

import (
	"fmt"
	"log/slog"

	"github.com/cloudwego/eino-ext/callbacks/langfuse"
	"github.com/cloudwego/eino/callbacks"
	"github.com/wall/nanobot-eino/pkg/config"
)

// Init creates a Langfuse callback handler and registers it globally.
// When cfg.Enabled is false, returns a no-op shutdown function with zero overhead.
// The returned shutdown function MUST be called before process exit to flush pending traces.
func Init(cfg config.TracingConfig) (shutdown func(), err error) {
	if !cfg.Enabled {
		return func() {}, nil
	}

	if cfg.Endpoint == "" || cfg.PublicKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("trace: enabled but missing required fields (endpoint, publicKey, secretKey)")
	}

	handler, flusher := langfuse.NewLangfuseHandler(&langfuse.Config{
		Host:      cfg.Endpoint,
		PublicKey: cfg.PublicKey,
		SecretKey: cfg.SecretKey,
	})

	callbacks.AppendGlobalHandlers(handler)

	slog.Info("Tracing enabled", "module", "trace", "endpoint", cfg.Endpoint)

	return flusher, nil
}
