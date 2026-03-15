package briefings

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RegisterDeviceToken registers or updates a device token for push notifications.
// Uses ON CONFLICT to upsert on duplicate token.
func RegisterDeviceToken(ctx context.Context, pool *pgxpool.Pool,
	userID, tenantID uuid.UUID, token, platform, deviceName, appVersion string) error {

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO device_tokens (user_id, tenant_id, token, platform, device_name, app_version)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (token) DO UPDATE SET
		    user_id = EXCLUDED.user_id,
		    last_used_at = NOW(),
		    device_name = EXCLUDED.device_name,
		    app_version = EXCLUDED.app_version`,
		userID, tenantID, token, platform, deviceName, appVersion)
	return err
}

// DeregisterDeviceToken removes a device token.
func DeregisterDeviceToken(ctx context.Context, pool *pgxpool.Pool, token string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Exec(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	return err
}

// GetUserDeviceTokens loads all device tokens for a user.
func GetUserDeviceTokens(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	rows, err := conn.Query(ctx,
		`SELECT token FROM device_tokens WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// CleanupStaleTokens deletes device tokens not used in 60 days.
func CleanupStaleTokens(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Release()

	result, err := conn.Exec(ctx,
		`DELETE FROM device_tokens WHERE last_used_at < NOW() - INTERVAL '60 days'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
