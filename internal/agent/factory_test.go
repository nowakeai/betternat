package agent

import (
	"testing"

	"github.com/nowakeai/betternat/internal/config"
)

func TestDefaultEngineFactorySelectsLoxiLB(t *testing.T) {
	engine, err := (DefaultEngineFactory{}).NewEngine(config.DatapathConfig{Engine: "loxilb"})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	if engine.Name() != "loxilb" {
		t.Fatalf("engine = %q", engine.Name())
	}
}

func TestDefaultEngineFactoryRejectsUnknownEngine(t *testing.T) {
	_, err := (DefaultEngineFactory{}).NewEngine(config.DatapathConfig{Engine: "unknown"})
	if err == nil {
		t.Fatal("expected unknown engine error")
	}
}
