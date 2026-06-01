package tooldispatch

import "time"

// DefaultRegistryConfig returns production-oriented defaults for WorkerRegistry.
func DefaultRegistryConfig() RegistryConfig {
	return RegistryConfig{
		LeaseTTL:        30 * time.Second,
		HeartbeatGrace:  5 * time.Second,
		MaxQueuePerTool: 256,
	}
}

// RegistryConfig tunes leases, capacity queues, and backpressure.
type RegistryConfig struct {
	// LeaseTTL is how long a worker registration remains valid without heartbeat.
	LeaseTTL time.Duration
	// HeartbeatGrace extends lease expiry when a heartbeat arrives slightly late.
	HeartbeatGrace time.Duration
	// MaxQueuePerTool caps the FIFO wait queue per tool@version (0 = 256).
	MaxQueuePerTool int
	// IntegrityCheck validates workers at dispatch; nil allows all registered workers.
	IntegrityCheck IntegrityCheck
}

func (c RegistryConfig) leaseTTL() time.Duration {
	if c.LeaseTTL > 0 {
		return c.LeaseTTL
	}
	return 30 * time.Second
}

func (c RegistryConfig) maxQueuePerTool() int {
	if c.MaxQueuePerTool > 0 {
		return c.MaxQueuePerTool
	}
	return 256
}
