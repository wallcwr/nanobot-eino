package trace

import (
	"testing"

	"github.com/wall/nanobot-eino/pkg/config"
)

func TestInit_Disabled(t *testing.T) {
	cfg := config.TracingConfig{Enabled: false}
	shutdown, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init with disabled config should not error: %v", err)
	}
	shutdown()
}

func TestInit_EnabledMissingFields(t *testing.T) {
	cfg := config.TracingConfig{Enabled: true}
	_, err := Init(cfg)
	if err == nil {
		t.Fatal("Init with enabled but empty config should return error")
	}
}

func TestInit_EnabledValid(t *testing.T) {
	cfg := config.TracingConfig{
		Enabled:   true,
		Endpoint:  "http://localhost:3000",
		PublicKey: "pk-lf-test",
		SecretKey: "sk-lf-test",
	}
	shutdown, err := Init(cfg)
	if err != nil {
		t.Fatalf("Init with valid config should not error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown function should not be nil")
	}
	shutdown()
}
