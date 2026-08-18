package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/phrony-platform/runtime/internal/version"
)

const (
	envTelemetryEndpoint      = "PHRONY_TELEMETRY_ENDPOINT"
	envDoNotTrack             = "DO_NOT_TRACK"
	envDisableTelemetry       = "DISABLE_TELEMETRY"
	envPhronyDisableTelemetry = "PHRONY_DISABLE_TELEMETRY"

	flushTimeout     = 3 * time.Second
	defaultFlushEvery = 30 * time.Second
	maxEventsPerBatch = 32
)

// DefaultEndpoint is the production telemetry batch POST URL.
// Override with PHRONY_TELEMETRY_ENDPOINT for staging or air-gapped environments.
const DefaultEndpoint = "https://hxybgqfxykmxdhyqsvlh.supabase.co/functions/v1/telemetry"

type eventCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type batchPayload struct {
	InstallID  string       `json:"install_id"`
	AppVersion string       `json:"app_version"`
	Platform   string       `json:"platform"`
	Events     []eventCount `json:"events"`
}

// Client buffers coarse event counts and POSTs them asynchronously.
type Client struct {
	mu     sync.Mutex
	counts map[string]int
	cfg    FileConfig
}

var defaultClient = &Client{counts: make(map[string]int)}

func envTruthy(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false
	}
	return v != "0" && !strings.EqualFold(v, "false")
}

func envDisablesTelemetry() bool {
	return envTruthy(envDoNotTrack) || envTruthy(envDisableTelemetry) || envTruthy(envPhronyDisableTelemetry)
}

func resolveEndpoint() string {
	if v := strings.TrimSpace(os.Getenv(envTelemetryEndpoint)); v != "" {
		return v
	}
	return DefaultEndpoint
}

func platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func (c *Client) ensureConfig() FileConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg.InstallID != "" {
		return c.cfg
	}
	cfg, err := LoadFileConfig()
	if err != nil {
		return FileConfig{}
	}
	c.cfg = cfg
	return cfg
}

// Enabled reports whether telemetry should be collected (config on and no opt-out env).
func (c *Client) Enabled() bool {
	cfg := c.ensureConfig()
	return cfg.InstallID != "" && cfg.Enabled && !envDisablesTelemetry()
}

// Track increments a whitelisted event count when telemetry is enabled.
func (c *Client) Track(name string) {
	if !isAllowedEvent(name) {
		return
	}
	if !c.Enabled() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[name]++
}

func (c *Client) snapshotForFlush() (batchPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.counts) == 0 {
		return batchPayload{}, false
	}
	cfg := c.cfg
	if cfg.InstallID == "" {
		return batchPayload{}, false
	}
	events := make([]eventCount, 0, len(c.counts))
	for name, count := range c.counts {
		if count <= 0 {
			continue
		}
		events = append(events, eventCount{Name: name, Count: count})
		if len(events) >= maxEventsPerBatch {
			break
		}
	}
	if len(events) == 0 {
		return batchPayload{}, false
	}
	for name := range c.counts {
		delete(c.counts, name)
	}
	return batchPayload{
		InstallID:  cfg.InstallID,
		AppVersion: version.RuntimeVersion,
		Platform:   platform(),
		Events:     events,
	}, true
}

func (c *Client) flush() {
	endpoint := resolveEndpoint()
	if endpoint == "" {
		return
	}
	payload, ok := c.snapshotForFlush()
	if !ok {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

// Flush sends buffered events in a background goroutine (fire-and-forget).
func Flush() {
	defaultClient.FlushAsync()
}

// FlushAsync starts a non-blocking flush.
func (c *Client) FlushAsync() {
	go func() {
		defer func() { recover() }()
		c.flush()
	}()
}

// Enabled reports whether telemetry should be collected in this process.
func Enabled() bool {
	return defaultClient.Enabled()
}

// Track records an event on the process-default client.
func Track(name string) {
	defaultClient.Track(name)
}

// StartPeriodicFlush flushes on interval until ctx is canceled.
func StartPeriodicFlush(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultFlushEvery
	}
	go func() {
		defer func() { recover() }()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				Flush()
			}
		}
	}()
}
