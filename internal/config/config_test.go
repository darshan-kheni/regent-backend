package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_MissingDatabaseURL_Panics(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "")

	assert.Panics(t, func() {
		Load()
	}, "should panic when DATABASE_URL is empty")
}

func TestLoad_DefaultValues(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")

	cfg := Load()

	assert.Equal(t, "development", cfg.Environment)
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, int32(25), cfg.Database.MaxConns)
	assert.Equal(t, int32(5), cfg.Database.MinConns)
	assert.Equal(t, 100, cfg.RateLimit.RequestsPerMinute)
	assert.True(t, cfg.RunMigrations)
}

func TestLoad_ProductionRequiresJWTSecret(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")
	t.Setenv("JWT_SECRET", "")

	assert.Panics(t, func() {
		Load()
	}, "should panic when JWT_SECRET is empty in production")
}

func TestLoad_ProductionWithJWTSecret(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")
	t.Setenv("JWT_SECRET", "super-secret-key")
	t.Setenv("SUPABASE_SERVICE_KEY", "test-service-key")
	t.Setenv("SUPABASE_URL", "https://test.supabase.co")

	cfg := Load()

	assert.Equal(t, "production", cfg.Environment)
	assert.Equal(t, "super-secret-key", cfg.Auth.JWTSecret)
	assert.Equal(t, "jwt", cfg.Auth.Mode)
	assert.Equal(t, "test-service-key", cfg.Auth.SupabaseServiceKey)
}

func TestLoad_InvalidEnvironment_Panics(t *testing.T) {
	t.Setenv("ENVIRONMENT", "invalid")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")

	assert.Panics(t, func() {
		Load()
	}, "should panic on invalid environment")
}

func TestLoad_AllowedOriginsParsed(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000,https://regent.orphilia.com,https://example.com")

	cfg := Load()

	require.Len(t, cfg.CORS.AllowedOrigins, 3)
	assert.Equal(t, "http://localhost:3000", cfg.CORS.AllowedOrigins[0])
	assert.Equal(t, "https://regent.orphilia.com", cfg.CORS.AllowedOrigins[1])
	assert.Equal(t, "https://example.com", cfg.CORS.AllowedOrigins[2])
}

func TestLoad_RunMigrationsDefault(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		envVar   string
		expected bool
	}{
		{"dev default", "development", "", true},
		{"prod default", "production", "", false},
		{"explicit true", "production", "true", true},
		{"explicit false", "development", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", tt.env)
			t.Setenv("DATABASE_URL", "postgresql://localhost/test")
			if tt.env == "production" {
				t.Setenv("JWT_SECRET", "test-secret")
				t.Setenv("SUPABASE_SERVICE_KEY", "test-service-key")
				t.Setenv("SUPABASE_URL", "https://test.supabase.co")
			}
			if tt.envVar != "" {
				t.Setenv("RUN_MIGRATIONS", tt.envVar)
			} else {
				os.Unsetenv("RUN_MIGRATIONS")
			}

			cfg := Load()
			assert.Equal(t, tt.expected, cfg.RunMigrations)
		})
	}
}

func TestLoad_CustomPort(t *testing.T) {
	t.Setenv("ENVIRONMENT", "development")
	t.Setenv("DATABASE_URL", "postgresql://localhost/test")
	t.Setenv("PORT", "9090")

	cfg := Load()
	assert.Equal(t, "9090", cfg.Port)
}

// --- Email Config Tests ---

func TestLoadEmailConfig(t *testing.T) {
	// Clear any env overrides so we get defaults.
	os.Unsetenv("IMAP_MAX_CONNECTIONS")
	os.Unsetenv("IMAP_IDLE_TIMEOUT")

	cfg := loadEmailConfig()
	assert.Equal(t, 1500, cfg.MaxConnections)
	assert.Equal(t, 28*time.Minute, cfg.IDLETimeout)
	assert.Equal(t, 30, cfg.SyncDepthDays)
	assert.Equal(t, 50, cfg.FetchBatchSize)
	assert.Equal(t, int64(25*1024*1024), cfg.AttachmentMaxSize)
	assert.Equal(t, "attachments", cfg.StorageBucket)
}

func TestLoadEmailConfig_CustomValues(t *testing.T) {
	t.Setenv("IMAP_MAX_CONNECTIONS", "500")
	t.Setenv("IMAP_IDLE_TIMEOUT", "20m")
	t.Setenv("SUPABASE_STORAGE_BUCKET", "custom-bucket")

	cfg := loadEmailConfig()
	assert.Equal(t, 500, cfg.MaxConnections)
	assert.Equal(t, 20*time.Minute, cfg.IDLETimeout)
	assert.Equal(t, "custom-bucket", cfg.StorageBucket)
}

// --- Orchestrator Config Tests ---

func TestLoadOrchestratorConfig(t *testing.T) {
	// Clear any env overrides.
	os.Unsetenv("ORCHESTRATOR_BOOT_DELAY")
	os.Unsetenv("ORCHESTRATOR_STAGGER_DURATION")
	os.Unsetenv("ORCHESTRATOR_HEALTH_INTERVAL")
	os.Unsetenv("ORCHESTRATOR_DRAIN_TIMEOUT")

	cfg := loadOrchestratorConfig()
	assert.Equal(t, 10*time.Second, cfg.BootDelay)
	assert.Equal(t, 30*time.Second, cfg.StaggerDuration)
	assert.Equal(t, 50, cfg.MaxConcurrentLogins)
	assert.Equal(t, 30*time.Second, cfg.HealthInterval)
	assert.Equal(t, 30*time.Second, cfg.DrainTimeout)
}

func TestLoadOrchestratorConfig_CustomValues(t *testing.T) {
	t.Setenv("ORCHESTRATOR_BOOT_DELAY", "5s")
	t.Setenv("ORCHESTRATOR_STAGGER_DURATION", "1m")
	t.Setenv("ORCHESTRATOR_DRAIN_TIMEOUT", "45s")

	cfg := loadOrchestratorConfig()
	assert.Equal(t, 5*time.Second, cfg.BootDelay)
	assert.Equal(t, 1*time.Minute, cfg.StaggerDuration)
	assert.Equal(t, 45*time.Second, cfg.DrainTimeout)
}

// --- getEnvDuration Tests ---

func TestGetEnvDuration_Default(t *testing.T) {
	d := getEnvDuration("NONEXISTENT_TEST_VAR_XYZ", 5*time.Second)
	assert.Equal(t, 5*time.Second, d)
}

func TestGetEnvDuration_WithValue(t *testing.T) {
	t.Setenv("TEST_DURATION_VAR", "15s")
	d := getEnvDuration("TEST_DURATION_VAR", 5*time.Second)
	assert.Equal(t, 15*time.Second, d)
}

func TestGetEnvDuration_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TEST_DURATION_INVALID", "not-a-duration")
	d := getEnvDuration("TEST_DURATION_INVALID", 5*time.Second)
	assert.Equal(t, 5*time.Second, d)
}

func TestGetEnvDuration_ComplexDuration(t *testing.T) {
	t.Setenv("TEST_DURATION_COMPLEX", "2h30m")
	d := getEnvDuration("TEST_DURATION_COMPLEX", 5*time.Second)
	assert.Equal(t, 2*time.Hour+30*time.Minute, d)
}

// --- SMTP Config Tests ---

func TestLoadSMTPConfig_Defaults(t *testing.T) {
	os.Unsetenv("SMTP_DEFAULT_HOST")
	os.Unsetenv("SMTP_DEFAULT_PORT")

	cfg := loadSMTPConfig()
	assert.Equal(t, "", cfg.DefaultHost)
	assert.Equal(t, 587, cfg.DefaultPort)
}

func TestLoadSMTPConfig_CustomValues(t *testing.T) {
	t.Setenv("SMTP_DEFAULT_HOST", "smtp.example.com")
	t.Setenv("SMTP_DEFAULT_PORT", "465")

	cfg := loadSMTPConfig()
	assert.Equal(t, "smtp.example.com", cfg.DefaultHost)
	assert.Equal(t, 465, cfg.DefaultPort)
}
