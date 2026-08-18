package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const configDirName = "phrony"
const configFileName = "telemetry.json"

// FileConfig is persisted under the user config directory.
type FileConfig struct {
	InstallID string `json:"install_id"`
	Enabled   bool   `json:"enabled"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, configFileName), nil
}

// LoadFileConfig reads telemetry.json, creating a default file when missing.
func LoadFileConfig() (FileConfig, error) {
	path, err := configPath()
	if err != nil {
		return FileConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := FileConfig{
				InstallID: uuid.NewString(),
				Enabled:   true,
			}
			if writeErr := saveFileConfig(path, cfg); writeErr != nil {
				return FileConfig{}, writeErr
			}
			return cfg, nil
		}
		return FileConfig{}, fmt.Errorf("read telemetry config: %w", err)
	}

	var cfg FileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return FileConfig{}, fmt.Errorf("parse telemetry config: %w", err)
	}
	if cfg.InstallID == "" {
		cfg.InstallID = uuid.NewString()
		if err := saveFileConfig(path, cfg); err != nil {
			return FileConfig{}, err
		}
	}
	return cfg, nil
}

func saveFileConfig(path string, cfg FileConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal telemetry config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write telemetry config: %w", err)
	}
	return nil
}

// Enable turns telemetry on in telemetry.json (env opt-out vars still apply).
func Enable() error {
	cfg, err := LoadFileConfig()
	if err != nil {
		return err
	}
	cfg.Enabled = true
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveFileConfig(path, cfg)
}

// Disable turns telemetry off in telemetry.json.
func Disable() error {
	cfg, err := LoadFileConfig()
	if err != nil {
		return err
	}
	cfg.Enabled = false
	path, err := configPath()
	if err != nil {
		return err
	}
	return saveFileConfig(path, cfg)
}

// Status describes effective telemetry state for operator display.
type Status struct {
	InstallID       string
	ConfigEnabled   bool
	EnvDisabled     bool
	EffectiveEnabled bool
}

// CurrentStatus returns persisted config plus env-var overrides.
func CurrentStatus() (Status, error) {
	cfg, err := LoadFileConfig()
	if err != nil {
		return Status{}, err
	}
	envOff := envDisablesTelemetry()
	return Status{
		InstallID:        cfg.InstallID,
		ConfigEnabled:    cfg.Enabled,
		EnvDisabled:      envOff,
		EffectiveEnabled: cfg.Enabled && !envOff,
	}, nil
}
