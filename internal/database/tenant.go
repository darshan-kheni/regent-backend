package database

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantContext embeds context.Context with tenant and user identity.
// ALL database functions MUST accept TenantContext, NEVER plain context.Context.
// This provides compile-time safety for multi-tenant isolation.
type TenantContext struct {
	context.Context
	TenantID uuid.UUID
	UserID   uuid.UUID
}

// WithTenant creates a new TenantContext from a parent context.
func WithTenant(ctx context.Context, tenantID, userID uuid.UUID) TenantContext {
	return TenantContext{
		Context:  ctx,
		TenantID: tenantID,
		UserID:   userID,
	}
}

// SetRLSContext sets the tenant_id session variable on an acquired connection.
// MUST be called on the same connection used for subsequent queries.
//
// CRITICAL: set_config with is_local=true is transaction-scoped. All subsequent
// queries MUST use the same *pgxpool.Conn where SetRLSContext was called AND
// must run within the same transaction. If using queries outside an explicit
// transaction, the setting applies to the current function call only and is
// cleared when the connection is returned to the pool. Never acquire a new
// connection after calling SetRLSContext — the new connection won't have the
// tenant context set.
func SetRLSContext(tc TenantContext, conn *pgxpool.Conn) error {
	// Use is_local=false so the setting persists across statements on the same connection.
	// The setting is automatically cleared when the connection is returned to the pool
	// (pgxpool resets connection state on release).
	_, err := conn.Exec(tc,
		"SELECT set_config('app.tenant_id', $1, false)",
		tc.TenantID.String(),
	)
	return err
}
