package common

import "os"

const (
	envDatabaseURL = "RUNTIME_DATABASE_URL"
	envGRPCAddr    = "RUNTIME_GRPC_ADDR"
	envRuntimeAddr = "PHRONY_RUNTIME_ADDR"

	defaultGRPCAddr    = "127.0.0.1:7777"
	defaultRuntimeAddr = "127.0.0.1:7777"
)

type Settings struct {
	DatabaseURL string `validate:"required"`
	GRPCAddr    string `validate:"required"`
	RuntimeAddr string `validate:"required"`
}

// LoadSettings reads configuration from the process environment.
func LoadSettings() (Settings, error) {
	settings := Settings{
		DatabaseURL: os.Getenv(envDatabaseURL),
		GRPCAddr:    os.Getenv(envGRPCAddr),
		RuntimeAddr: os.Getenv(envRuntimeAddr),
	}
	if settings.GRPCAddr == "" {
		settings.GRPCAddr = defaultGRPCAddr
	}
	if settings.RuntimeAddr == "" {
		settings.RuntimeAddr = defaultRuntimeAddr
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// Validate checks that required settings are present.
func (s Settings) Validate() error {
	return settingsValidator.Struct(s)
}

// ResolveRuntimeAddr returns the gRPC address for the phrony CLI.
// flag takes precedence over PHRONY_RUNTIME_ADDR, then the default.
func ResolveRuntimeAddr(flag string) string {
	if flag != "" {
		return flag
	}
	if addr := os.Getenv(envRuntimeAddr); addr != "" {
		return addr
	}
	return defaultRuntimeAddr
}
