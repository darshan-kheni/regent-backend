package orchestrator

import "time"

// OrchestratorConfig holds configuration for the service orchestrator.
type OrchestratorConfig struct {
	// BootDelay is the warm-up period before spawning user bundles on server start.
	BootDelay time.Duration

	// StaggerDuration is the total time window over which user spawns are spread.
	StaggerDuration time.Duration

	// MaxConcurrentLogins limits how many user bundles can be starting simultaneously.
	MaxConcurrentLogins int

	// HeartbeatInterval is how often each bundle reports health to the database.
	HeartbeatInterval time.Duration

	// DrainTimeout is the maximum time to wait for all bundles to stop during shutdown.
	DrainTimeout time.Duration

	// Supervisor tuning — overridable for testing.
	SupervisorBaseBackoff      time.Duration
	SupervisorMaxBackoff       time.Duration
	SupervisorMaxFailures      int
	SupervisorStableThreshold  time.Duration
}

// DefaultConfig returns production-ready default configuration values.
func DefaultConfig() *OrchestratorConfig {
	return &OrchestratorConfig{
		BootDelay:                  10 * time.Second,
		StaggerDuration:            30 * time.Second,
		MaxConcurrentLogins:        50,
		HeartbeatInterval:          30 * time.Second,
		DrainTimeout:               30 * time.Second,
		SupervisorBaseBackoff:      2 * time.Second,
		SupervisorMaxBackoff:       5 * time.Minute,
		SupervisorMaxFailures:      10,
		SupervisorStableThreshold:  30 * time.Second,
	}
}
