package middleware

import "github.com/darshan-kheni/regent/internal/config"

func testAuthConfig() config.AuthConfig {
	return config.AuthConfig{
		Mode:            "stub",
		JWTSecret:       "test-secret",
		SupabaseURL:     "https://test.supabase.co",
		SupabaseAnonKey: "test-anon-key",
	}
}
