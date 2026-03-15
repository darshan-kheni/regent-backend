package auth

import (
	"testing"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		password string
		wantErr  string
	}{
		{
			name:     "valid password",
			password: "SecurePass123",
			wantErr:  "",
		},
		{
			name:     "valid with special chars",
			password: "MyP@ssw0rd!xyz",
			wantErr:  "",
		},
		{
			name:     "too short",
			password: "Short1Aa",
			wantErr:  "password must be at least 12 characters",
		},
		{
			name:     "missing uppercase",
			password: "alllowercase1",
			wantErr:  "password must contain at least one uppercase letter",
		},
		{
			name:     "missing lowercase",
			password: "ALLUPPERCASE1",
			wantErr:  "password must contain at least one lowercase letter",
		},
		{
			name:     "missing digit",
			password: "NoDigitsHereAbc",
			wantErr:  "password must contain at least one digit",
		},
		{
			name:     "empty string",
			password: "",
			wantErr:  "password must be at least 12 characters",
		},
		{
			name:     "exactly 12 chars valid",
			password: "Abcdefghij1k",
			wantErr:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePassword(tt.password)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePassword(%q) unexpected error: %v", tt.password, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidatePassword(%q) expected error containing %q, got nil", tt.password, tt.wantErr)
				} else if err.Error() != tt.wantErr {
					t.Errorf("ValidatePassword(%q) error = %q, want %q", tt.password, err.Error(), tt.wantErr)
				}
			}
		})
	}
}
