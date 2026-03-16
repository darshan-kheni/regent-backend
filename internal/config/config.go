package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration, loaded once at startup.
type Config struct {
	Environment   string
	Port          string
	RunMigrations bool
	Database      DatabaseConfig
	Auth          AuthConfig
	CORS          CORSConfig
	RateLimit     RateLimitConfig
	Email         EmailConfig
	Gmail         GmailConfig
	Microsoft     MicrosoftConfig
	Orchestrator  OrchestratorCfg
	SMTP          SMTPConfig
	AI            AIConfig
	Redis         RedisConfig
	Notifications NotificationsConfig
	Billing       BillingConfig
	Calendar      CalendarConfig
}

// CalendarConfig holds calendar intelligence settings (Phase 9).
type CalendarConfig struct {
	SyncIntervalMinutes     int    // CALENDAR_SYNC_INTERVAL_MINUTES, default 5
	SyncWindowDays          int    // CALENDAR_SYNC_WINDOW_DAYS, default 30
	BriefLeadMinutes        int    // CALENDAR_BRIEF_LEAD_MINUTES, default 30
	NotesPromptDelayMinutes int    // CALENDAR_NOTES_PROMPT_DELAY_MINUTES, default 5
	SlotIncrementMinutes    int    // CALENDAR_SLOT_INCREMENT_MINUTES, default 15
	MaxSlotSuggestions      int    // CALENDAR_MAX_SLOT_SUGGESTIONS, default 3
	MaxQueryRangeDays       int    // CALENDAR_MAX_QUERY_RANGE_DAYS, default 90
	EventRetentionDays      int    // CALENDAR_EVENT_RETENTION_DAYS, default 30
	RetryMaxAttempts        int    // CALENDAR_RETRY_MAX_ATTEMPTS, default 3
	RetryBaseDelaySeconds   int    // CALENDAR_RETRY_BASE_DELAY_SECONDS, default 1
	GoogleClientID          string // GOOGLE_CLIENT_ID (shared with Gmail)
	GoogleClientSecret      string // GOOGLE_CLIENT_SECRET (shared with Gmail)
}

// BillingConfig holds Stripe payment integration settings.
type BillingConfig struct {
	StripeSecretKey      string
	StripePublishableKey string
	StripeWebhookSecret  string
	StripeMode           string // "test" or "live"
	StripePriceFree      string
	StripePriceAttache   string
	StripePricePrivy     string
	StripePriceEstate    string
	FrontendURL          string
}

// AIConfig holds AI provider settings.
type AIConfig struct {
	OllamaCloudURL         string        // OLLAMA_CLOUD_URL, default "https://ollama.com/api"
	OllamaAPIKey           string        // OLLAMA_CLOUD_API_KEY
	GeminiAPIKey           string        // GEMINI_API_KEY
	WorkerPoolSize         int           // AI_WORKER_POOL_SIZE, default 20
	CircuitBreakerFailures int           // AI_CB_FAILURES, default 3
	CircuitBreakerInterval time.Duration // AI_CB_INTERVAL, default 60s
	CircuitBreakerTimeout  time.Duration // AI_CB_TIMEOUT, default 5min
	CacheTTL               time.Duration // AI_CACHE_TTL, default 24h
	RateLimitPerMin        int           // AI_RATE_LIMIT_PER_MIN, default 30
	RateLimitBurst         int           // AI_RATE_LIMIT_BURST, default 60
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL      string // REDIS_URL
	PoolSize int    // REDIS_POOL_SIZE, default 10
}

// EmailConfig holds IMAP and email engine settings.
type EmailConfig struct {
	MaxConnections    int           // IMAP_MAX_CONNECTIONS, default 1500
	IDLETimeout       time.Duration // IMAP_IDLE_TIMEOUT, default 28m
	SyncDepthDays     int           // Fixed 30
	FetchBatchSize    int           // Fixed 50
	AttachmentMaxSize int64         // 25MB
	StorageBucket     string        // SUPABASE_STORAGE_BUCKET, default "attachments"
}

// GmailConfig holds Google Gmail API and Pub/Sub settings.
type GmailConfig struct {
	ClientID      string // GOOGLE_CLIENT_ID
	ClientSecret  string // GOOGLE_CLIENT_SECRET
	PubSubTopic   string // GMAIL_PUBSUB_TOPIC
	PubSubProject string // GMAIL_PUBSUB_PROJECT
}

// MicrosoftConfig holds Microsoft OAuth settings.
type MicrosoftConfig struct {
	ClientID     string // MICROSOFT_CLIENT_ID
	ClientSecret string // MICROSOFT_CLIENT_SECRET
}

// OrchestratorCfg holds settings for the always-on service orchestrator.
// Named differently from orchestrator.OrchestratorConfig to avoid confusion.
type OrchestratorCfg struct {
	BootDelay           time.Duration // ORCHESTRATOR_BOOT_DELAY, default 10s
	StaggerDuration     time.Duration // ORCHESTRATOR_STAGGER_DURATION, default 30s
	HealthInterval      time.Duration // ORCHESTRATOR_HEALTH_INTERVAL, default 30s
	MaxConcurrentLogins int           // Fixed 50
	DrainTimeout        time.Duration // ORCHESTRATOR_DRAIN_TIMEOUT, default 30s
}

// SMTPConfig holds outbound SMTP defaults.
type SMTPConfig struct {
	DefaultHost string // SMTP_DEFAULT_HOST
	DefaultPort int    // SMTP_DEFAULT_PORT, default 587
}

// DatabaseConfig holds PostgreSQL connection pool settings.
type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// AuthConfig holds authentication settings.
type AuthConfig struct {
	Mode                string // "stub" or "jwt"
	JWTSecret           string
	SupabaseURL         string
	SupabaseAnonKey     string
	SupabaseServiceKey  string
	EncryptionMasterKey string
}

// CORSConfig holds CORS settings.
type CORSConfig struct {
	AllowedOrigins []string
}

// RateLimitConfig holds rate limiting settings.
type RateLimitConfig struct {
	RequestsPerMinute int
}

// NotificationsConfig holds notification provider settings (Phase 5).
type NotificationsConfig struct {
	Twilio   TwilioConfig
	WhatsApp WhatsAppConfig
	Signal   SignalConfig
	Firebase FirebaseConfig
}

// TwilioConfig holds Twilio SMS settings.
type TwilioConfig struct {
	AccountSID string // TWILIO_ACCOUNT_SID
	AuthToken  string // TWILIO_AUTH_TOKEN
	FromNumber string // TWILIO_FROM_NUMBER
}

// WhatsAppConfig holds Meta WhatsApp Business API settings.
type WhatsAppConfig struct {
	AccessToken       string // WHATSAPP_ACCESS_TOKEN
	PhoneNumberID     string // WHATSAPP_PHONE_NUMBER_ID
	BusinessAccountID string // WHATSAPP_BUSINESS_ACCOUNT_ID
	VerifyToken       string // WHATSAPP_VERIFY_TOKEN
}

// SignalConfig holds signal-cli-rest-api settings.
type SignalConfig struct {
	APIURL     string // SIGNAL_API_URL
	FromNumber string // SIGNAL_FROM_NUMBER
}

// FirebaseConfig holds Firebase/FCM settings.
type FirebaseConfig struct {
	ProjectID       string // FIREBASE_PROJECT_ID
	CredentialsFile string // GOOGLE_APPLICATION_CREDENTIALS
}

// Load reads configuration from environment variables (and .env in development).
// Panics with a clear message if required variables are missing.
func Load() *Config {
	env := getEnvOrDefault("ENVIRONMENT", "development")

	if env == "development" {
		if err := godotenv.Load(); err != nil {
			// .env file is optional in development
			log.Printf("no .env file found: %v", err)
		}
	}

	cfg := &Config{
		Environment:   env,
		Port:          getEnvOrDefault("PORT", "8080"),
		RunMigrations: getRunMigrations(env),
		Database:      loadDatabaseConfig(),
		Auth:          loadAuthConfig(env),
		CORS:          loadCORSConfig(),
		RateLimit:     loadRateLimitConfig(),
		Email:         loadEmailConfig(),
		Gmail:         loadGmailConfig(),
		Microsoft:     loadMicrosoftConfig(),
		Orchestrator:  loadOrchestratorConfig(),
		SMTP:          loadSMTPConfig(),
		AI:            loadAIConfig(),
		Redis:         loadRedisConfig(),
		Notifications: loadNotificationsConfig(),
		Billing:       loadBillingConfig(),
		Calendar:      loadCalendarConfig(),
	}

	validate(cfg)
	return cfg
}

func loadDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		URL:             os.Getenv("DATABASE_URL"),
		MaxConns:        getEnvInt32("DB_MAX_CONNS", 25),
		MinConns:        getEnvInt32("DB_MIN_CONNS", 5),
		MaxConnLifetime: 1 * time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
	}
}

func loadAuthConfig(env string) AuthConfig {
	mode := getEnvOrDefault("AUTH_MODE", "")
	if mode == "" {
		if env == "production" {
			mode = "jwt"
		} else {
			mode = "stub"
		}
	}
	return AuthConfig{
		Mode:                mode,
		JWTSecret:           os.Getenv("JWT_SECRET"),
		SupabaseURL:         os.Getenv("SUPABASE_URL"),
		SupabaseAnonKey:     os.Getenv("SUPABASE_ANON_KEY"),
		SupabaseServiceKey:  os.Getenv("SUPABASE_SERVICE_KEY"),
		EncryptionMasterKey: os.Getenv("ENCRYPTION_MASTER_KEY"),
	}
}

func loadCORSConfig() CORSConfig {
	origins := getEnvOrDefault("ALLOWED_ORIGINS", "http://localhost:3000,https://regent.orphilia.com")
	return CORSConfig{
		AllowedOrigins: strings.Split(origins, ","),
	}
}

func loadRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: int(getEnvInt32("RATE_LIMIT_RPM", 100)),
	}
}

func getRunMigrations(env string) bool {
	val := os.Getenv("RUN_MIGRATIONS")
	if val == "" {
		return env != "production"
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return env != "production"
	}
	return b
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt32(key string, defaultVal int32) int32 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		return defaultVal
	}
	return int32(n)
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultVal
	}
	return d
}

func loadEmailConfig() EmailConfig {
	return EmailConfig{
		MaxConnections:    int(getEnvInt32("IMAP_MAX_CONNECTIONS", 1500)),
		IDLETimeout:       getEnvDuration("IMAP_IDLE_TIMEOUT", 28*time.Minute),
		SyncDepthDays:     30,
		FetchBatchSize:    50,
		AttachmentMaxSize: 25 * 1024 * 1024,
		StorageBucket:     getEnvOrDefault("SUPABASE_STORAGE_BUCKET", "attachments"),
	}
}

func loadGmailConfig() GmailConfig {
	return GmailConfig{
		ClientID:      os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret:  os.Getenv("GOOGLE_CLIENT_SECRET"),
		PubSubTopic:   os.Getenv("GMAIL_PUBSUB_TOPIC"),
		PubSubProject: os.Getenv("GMAIL_PUBSUB_PROJECT"),
	}
}

func loadMicrosoftConfig() MicrosoftConfig {
	return MicrosoftConfig{
		ClientID:     os.Getenv("MICROSOFT_CLIENT_ID"),
		ClientSecret: os.Getenv("MICROSOFT_CLIENT_SECRET"),
	}
}

func loadOrchestratorConfig() OrchestratorCfg {
	return OrchestratorCfg{
		BootDelay:           getEnvDuration("ORCHESTRATOR_BOOT_DELAY", 10*time.Second),
		StaggerDuration:     getEnvDuration("ORCHESTRATOR_STAGGER_DURATION", 30*time.Second),
		HealthInterval:      getEnvDuration("ORCHESTRATOR_HEALTH_INTERVAL", 30*time.Second),
		MaxConcurrentLogins: 50,
		DrainTimeout:        getEnvDuration("ORCHESTRATOR_DRAIN_TIMEOUT", 30*time.Second),
	}
}

func loadSMTPConfig() SMTPConfig {
	return SMTPConfig{
		DefaultHost: getEnvOrDefault("SMTP_DEFAULT_HOST", ""),
		DefaultPort: int(getEnvInt32("SMTP_DEFAULT_PORT", 587)),
	}
}

func loadAIConfig() AIConfig {
	return AIConfig{
		OllamaCloudURL:         getEnvOrDefault("OLLAMA_CLOUD_URL", "https://ollama.com/api"),
		OllamaAPIKey:           os.Getenv("OLLAMA_CLOUD_API_KEY"),
		GeminiAPIKey:           os.Getenv("GEMINI_API_KEY"),
		WorkerPoolSize:         int(getEnvInt32("AI_WORKER_POOL_SIZE", 20)),
		CircuitBreakerFailures: int(getEnvInt32("AI_CB_FAILURES", 3)),
		CircuitBreakerInterval: getEnvDuration("AI_CB_INTERVAL", 60*time.Second),
		CircuitBreakerTimeout:  getEnvDuration("AI_CB_TIMEOUT", 5*time.Minute),
		CacheTTL:               getEnvDuration("AI_CACHE_TTL", 24*time.Hour),
		RateLimitPerMin:        int(getEnvInt32("AI_RATE_LIMIT_PER_MIN", 30)),
		RateLimitBurst:         int(getEnvInt32("AI_RATE_LIMIT_BURST", 60)),
	}
}

func loadRedisConfig() RedisConfig {
	return RedisConfig{
		URL:      os.Getenv("REDIS_URL"),
		PoolSize: int(getEnvInt32("REDIS_POOL_SIZE", 10)),
	}
}

func loadBillingConfig() BillingConfig {
	return BillingConfig{
		StripeSecretKey:      os.Getenv("STRIPE_SECRET_KEY"),
		StripePublishableKey: os.Getenv("STRIPE_PUBLISHABLE_KEY"),
		StripeWebhookSecret:  os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeMode:           getEnvOrDefault("STRIPE_MODE", "test"),
		StripePriceFree:      os.Getenv("STRIPE_PRICE_FREE"),
		StripePriceAttache:   os.Getenv("STRIPE_PRICE_ATTACHE"),
		StripePricePrivy:     os.Getenv("STRIPE_PRICE_PRIVY"),
		StripePriceEstate:    os.Getenv("STRIPE_PRICE_ESTATE"),
		FrontendURL:          getEnvOrDefault("FRONTEND_URL", "http://localhost:3000"),
	}
}

func loadCalendarConfig() CalendarConfig {
	return CalendarConfig{
		SyncIntervalMinutes:     int(getEnvInt32("CALENDAR_SYNC_INTERVAL_MINUTES", 5)),
		SyncWindowDays:          int(getEnvInt32("CALENDAR_SYNC_WINDOW_DAYS", 30)),
		BriefLeadMinutes:        int(getEnvInt32("CALENDAR_BRIEF_LEAD_MINUTES", 30)),
		NotesPromptDelayMinutes: int(getEnvInt32("CALENDAR_NOTES_PROMPT_DELAY_MINUTES", 5)),
		SlotIncrementMinutes:    int(getEnvInt32("CALENDAR_SLOT_INCREMENT_MINUTES", 15)),
		MaxSlotSuggestions:      int(getEnvInt32("CALENDAR_MAX_SLOT_SUGGESTIONS", 3)),
		MaxQueryRangeDays:       int(getEnvInt32("CALENDAR_MAX_QUERY_RANGE_DAYS", 90)),
		EventRetentionDays:      int(getEnvInt32("CALENDAR_EVENT_RETENTION_DAYS", 30)),
		RetryMaxAttempts:        int(getEnvInt32("CALENDAR_RETRY_MAX_ATTEMPTS", 3)),
		RetryBaseDelaySeconds:   int(getEnvInt32("CALENDAR_RETRY_BASE_DELAY_SECONDS", 1)),
		GoogleClientID:          os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret:      os.Getenv("GOOGLE_CLIENT_SECRET"),
	}
}

func loadNotificationsConfig() NotificationsConfig {
	return NotificationsConfig{
		Twilio: TwilioConfig{
			AccountSID: os.Getenv("TWILIO_ACCOUNT_SID"),
			AuthToken:  os.Getenv("TWILIO_AUTH_TOKEN"),
			FromNumber: os.Getenv("TWILIO_FROM_NUMBER"),
		},
		WhatsApp: WhatsAppConfig{
			AccessToken:       os.Getenv("WHATSAPP_ACCESS_TOKEN"),
			PhoneNumberID:     os.Getenv("WHATSAPP_PHONE_NUMBER_ID"),
			BusinessAccountID: os.Getenv("WHATSAPP_BUSINESS_ACCOUNT_ID"),
			VerifyToken:       os.Getenv("WHATSAPP_VERIFY_TOKEN"),
		},
		Signal: SignalConfig{
			APIURL:     os.Getenv("SIGNAL_API_URL"),
			FromNumber: os.Getenv("SIGNAL_FROM_NUMBER"),
		},
		Firebase: FirebaseConfig{
			ProjectID:       os.Getenv("FIREBASE_PROJECT_ID"),
			CredentialsFile: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		},
	}
}
