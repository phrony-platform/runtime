package telemetry

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteNotice_disabledInConfig(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("DISABLE_TELEMETRY", "")
	t.Setenv("PHRONY_DISABLE_TELEMETRY", "")

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	cfg.Enabled = false
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := saveFileConfig(path, cfg); err != nil {
		t.Fatalf("saveFileConfig: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteNotice(&buf); err != nil {
		t.Fatalf("WriteNotice: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "Telemetry is enabled") {
		t.Fatalf("output = %q, want disabled message", out)
	}
	if !strings.Contains(out, "Telemetry is disabled") {
		t.Fatalf("output = %q, want disabled message", out)
	}
}

func TestWriteNotice_enabledInConfig(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("DISABLE_TELEMETRY", "")
	t.Setenv("PHRONY_DISABLE_TELEMETRY", "")

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	cfg.Enabled = true
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := saveFileConfig(path, cfg); err != nil {
		t.Fatalf("saveFileConfig: %v", err)
	}

	var buf bytes.Buffer
	if err := WriteNotice(&buf); err != nil {
		t.Fatalf("WriteNotice: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Telemetry is enabled") {
		t.Fatalf("output = %q, want enabled message", out)
	}
}

func TestWriteNotice_envDisabled(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "1")

	var buf bytes.Buffer
	if err := WriteNotice(&buf); err != nil {
		t.Fatalf("WriteNotice: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "DO_NOT_TRACK") {
		t.Fatalf("output = %q, want env opt-out message", out)
	}
}

func TestEnabled_matchesConfig(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "")
	t.Setenv("DISABLE_TELEMETRY", "")
	t.Setenv("PHRONY_DISABLE_TELEMETRY", "")

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := LoadFileConfig()
	if err != nil {
		t.Fatalf("LoadFileConfig: %v", err)
	}
	cfg.Enabled = false
	path, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	if err := saveFileConfig(path, cfg); err != nil {
		t.Fatalf("saveFileConfig: %v", err)
	}

	defaultClient = &Client{counts: make(map[string]int)}

	if Enabled() {
		t.Fatal("expected telemetry disabled when config enabled=false")
	}

	cfg.Enabled = true
	if err := saveFileConfig(path, cfg); err != nil {
		t.Fatalf("saveFileConfig: %v", err)
	}
	defaultClient = &Client{counts: make(map[string]int)}

	if !Enabled() {
		t.Fatal("expected telemetry enabled when config enabled=true")
	}
}
