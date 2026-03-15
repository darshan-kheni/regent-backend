package auth

// Config holds auth-specific configuration derived from the main config.
type Config struct {
	SupabaseURL        string
	SupabaseAnonKey    string
	SupabaseServiceKey string
	JWKSURL            string // derived: SupabaseURL + "/auth/v1/.well-known/jwks.json"
	Issuer             string // derived: SupabaseURL + "/auth/v1"
}

// NewConfig creates an auth Config with derived URLs.
func NewConfig(supabaseURL, anonKey, serviceKey string) Config {
	return Config{
		SupabaseURL:        supabaseURL,
		SupabaseAnonKey:    anonKey,
		SupabaseServiceKey: serviceKey,
		JWKSURL:            supabaseURL + "/auth/v1/.well-known/jwks.json",
		Issuer:             supabaseURL + "/auth/v1",
	}
}
