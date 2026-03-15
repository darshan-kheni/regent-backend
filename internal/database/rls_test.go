//go:build integration

package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFixture holds IDs for a single tenant's test data hierarchy.
type testFixture struct {
	tenantID  uuid.UUID
	userID    uuid.UUID
	accountID uuid.UUID
	emailID   uuid.UUID
}

func setupTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	_ = godotenv.Load("/Users/dea/Desktop/GolandProjects/Regent/backend/.env")
	dbURL := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, dbURL, "DATABASE_URL must be set")

	cfg, err := pgxpool.ParseConfig(dbURL)
	require.NoError(t, err)
	cfg.MaxConns = 5

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(context.Background()))

	t.Cleanup(func() { pool.Close() })
	return pool
}

// insertFixture inserts a full chain of test data for one tenant using a superuser
// connection (no RLS). Returns all IDs.
func insertFixture(t *testing.T, pool *pgxpool.Pool, suffix string) testFixture {
	t.Helper()
	ctx := context.Background()

	f := testFixture{
		tenantID:  uuid.New(),
		userID:    uuid.New(),
		accountID: uuid.New(),
		emailID:   uuid.New(),
	}

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	// Insert as superuser (bypasses RLS) to set up test data cleanly.
	_, err = conn.Exec(ctx,
		`INSERT INTO tenants (id, name, slug, plan) VALUES ($1, $2, $3, 'free')`,
		f.tenantID, "Test Tenant "+suffix, "test-"+suffix+"-"+f.tenantID.String()[:8])
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO users (id, tenant_id, email, full_name) VALUES ($1, $2, $3, $4)`,
		f.userID, f.tenantID, fmt.Sprintf("user-%s@test.regent.ai", suffix), "Test User "+suffix)
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO user_accounts (id, user_id, tenant_id, provider, email_address) VALUES ($1, $2, $3, 'gmail', $4)`,
		f.accountID, f.userID, f.tenantID, fmt.Sprintf("account-%s@test.regent.ai", suffix))
	require.NoError(t, err)

	_, err = conn.Exec(ctx,
		`INSERT INTO emails (id, tenant_id, user_id, account_id, message_id, uid, from_address, received_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		f.emailID, f.tenantID, f.userID, f.accountID,
		fmt.Sprintf("<%s-%s@test.regent.ai>", suffix, f.emailID.String()[:8]),
		time.Now().UnixNano()%1000000,
		fmt.Sprintf("sender-%s@example.com", suffix), time.Now())
	require.NoError(t, err)

	return f
}

// cleanupFixture removes all test data for a tenant as superuser (cascade from tenants).
func cleanupFixture(t *testing.T, pool *pgxpool.Pool, f testFixture) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()

	// Delete in reverse FK order as superuser (bypasses RLS).
	conn.Exec(ctx, "DELETE FROM usage_logs WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM automation_rules WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM subscriptions WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM email_categories WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM email_summaries WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM draft_replies WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM emails WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM user_accounts WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM users WHERE tenant_id = $1", f.tenantID)
	conn.Exec(ctx, "DELETE FROM tenants WHERE id = $1", f.tenantID)
}

// queryIDsAsRole returns all row IDs visible to a tenant, using a non-superuser role.
func queryIDsAsRole(t *testing.T, pool *pgxpool.Pool, tenantID uuid.UUID, table, idCol string) []uuid.UUID {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	// Drop to authenticated role so RLS policies take effect.
	_, err = tx.Exec(ctx, "SET LOCAL ROLE authenticated")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID.String())
	require.NoError(t, err)

	rows, err := tx.Query(ctx, fmt.Sprintf("SELECT %s FROM %s", idCol, table))
	require.NoError(t, err)
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

// rlsUpdateCount attempts to UPDATE a specific row as a given tenant with role drop.
func rlsUpdateCount(t *testing.T, pool *pgxpool.Pool, asTenant uuid.UUID, table, idCol string, targetID uuid.UUID, setClause string) int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SET LOCAL ROLE authenticated")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", asTenant.String())
	require.NoError(t, err)

	tag, err := tx.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s WHERE %s = $1", table, setClause, idCol), targetID)
	require.NoError(t, err)
	return tag.RowsAffected()
}

// rlsDeleteCount attempts to DELETE a specific row as a given tenant with role drop.
func rlsDeleteCount(t *testing.T, pool *pgxpool.Pool, asTenant uuid.UUID, table, idCol string, targetID uuid.UUID) int64 {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "SET LOCAL ROLE authenticated")
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", asTenant.String())
	require.NoError(t, err)

	tag, err := tx.Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = $1", table, idCol), targetID)
	require.NoError(t, err)
	return tag.RowsAffected()
}

func TestRLSIsolation(t *testing.T) {
	pool := setupTestPool(t)

	// Verify that the 'authenticated' role exists in Supabase (it should by default).
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	var roleExists bool
	err = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = 'authenticated')").Scan(&roleExists)
	require.NoError(t, err)
	conn.Release()
	require.True(t, roleExists, "Supabase 'authenticated' role must exist for RLS tests")

	fixtureA := insertFixture(t, pool, "alpha")
	t.Cleanup(func() { cleanupFixture(t, pool, fixtureA) })

	fixtureB := insertFixture(t, pool, "bravo")
	t.Cleanup(func() { cleanupFixture(t, pool, fixtureB) })

	// --- tenants table ---
	t.Run("tenants", func(t *testing.T) {
		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "tenants", "id")
		assert.Contains(t, idsA, fixtureA.tenantID, "Tenant A should see its own tenant row")
		assert.NotContains(t, idsA, fixtureB.tenantID, "Tenant A must not see Tenant B's row")

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "tenants", "id")
		assert.Contains(t, idsB, fixtureB.tenantID)
		assert.NotContains(t, idsB, fixtureA.tenantID)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "tenants", "id", fixtureB.tenantID, "name = 'hacked'")
		assert.Equal(t, int64(0), affected, "Tenant A must not be able to UPDATE Tenant B's row")

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "tenants", "id", fixtureB.tenantID)
		assert.Equal(t, int64(0), affected, "Tenant A must not be able to DELETE Tenant B's row")
	})

	// --- users table ---
	t.Run("users", func(t *testing.T) {
		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "users", "id")
		assert.Contains(t, idsA, fixtureA.userID)
		assert.NotContains(t, idsA, fixtureB.userID)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "users", "id")
		assert.Contains(t, idsB, fixtureB.userID)
		assert.NotContains(t, idsB, fixtureA.userID)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "users", "id", fixtureB.userID, "full_name = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "users", "id", fixtureB.userID)
		assert.Equal(t, int64(0), affected)
	})

	// --- user_accounts table ---
	t.Run("user_accounts", func(t *testing.T) {
		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "user_accounts", "id")
		assert.Contains(t, idsA, fixtureA.accountID)
		assert.NotContains(t, idsA, fixtureB.accountID)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "user_accounts", "id")
		assert.Contains(t, idsB, fixtureB.accountID)
		assert.NotContains(t, idsB, fixtureA.accountID)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "user_accounts", "id", fixtureB.accountID, "display_name = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "user_accounts", "id", fixtureB.accountID)
		assert.Equal(t, int64(0), affected)
	})

	// --- emails table ---
	t.Run("emails", func(t *testing.T) {
		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "emails", "id")
		assert.Contains(t, idsA, fixtureA.emailID)
		assert.NotContains(t, idsA, fixtureB.emailID)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "emails", "id")
		assert.Contains(t, idsB, fixtureB.emailID)
		assert.NotContains(t, idsB, fixtureA.emailID)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "emails", "id", fixtureB.emailID, "subject = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "emails", "id", fixtureB.emailID)
		assert.Equal(t, int64(0), affected)
	})

	// --- email_categories table ---
	t.Run("email_categories", func(t *testing.T) {
		ctx := context.Background()
		catIDA := uuid.New()
		catIDB := uuid.New()

		// Insert as superuser
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO email_categories (id, tenant_id, email_id, category, confidence, model_used) VALUES ($1, $2, $3, 'work', 0.95, 'qwen3:4b')`,
			catIDA, fixtureA.tenantID, fixtureA.emailID)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO email_categories (id, tenant_id, email_id, category, confidence, model_used) VALUES ($1, $2, $3, 'personal', 0.88, 'qwen3:4b')`,
			catIDB, fixtureB.tenantID, fixtureB.emailID)
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM email_categories WHERE id IN ($1, $2)", catIDA, catIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "email_categories", "id")
		assert.Contains(t, idsA, catIDA)
		assert.NotContains(t, idsA, catIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "email_categories", "id")
		assert.Contains(t, idsB, catIDB)
		assert.NotContains(t, idsB, catIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "email_categories", "id", catIDB, "category = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "email_categories", "id", catIDB)
		assert.Equal(t, int64(0), affected)
	})

	// --- email_summaries table ---
	t.Run("email_summaries", func(t *testing.T) {
		ctx := context.Background()
		sumIDA := uuid.New()
		sumIDB := uuid.New()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO email_summaries (id, tenant_id, email_id, summary, model_used) VALUES ($1, $2, $3, 'Summary A', 'qwen3:8b')`,
			sumIDA, fixtureA.tenantID, fixtureA.emailID)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO email_summaries (id, tenant_id, email_id, summary, model_used) VALUES ($1, $2, $3, 'Summary B', 'qwen3:8b')`,
			sumIDB, fixtureB.tenantID, fixtureB.emailID)
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM email_summaries WHERE id IN ($1, $2)", sumIDA, sumIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "email_summaries", "id")
		assert.Contains(t, idsA, sumIDA)
		assert.NotContains(t, idsA, sumIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "email_summaries", "id")
		assert.Contains(t, idsB, sumIDB)
		assert.NotContains(t, idsB, sumIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "email_summaries", "id", sumIDB, "summary = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "email_summaries", "id", sumIDB)
		assert.Equal(t, int64(0), affected)
	})

	// --- draft_replies table ---
	t.Run("draft_replies", func(t *testing.T) {
		ctx := context.Background()
		draftIDA := uuid.New()
		draftIDB := uuid.New()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO draft_replies (id, tenant_id, email_id, body, variant, model_used, confidence) VALUES ($1, $2, $3, 'Draft A', 'professional', 'gemma3:12b', 0.90)`,
			draftIDA, fixtureA.tenantID, fixtureA.emailID)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO draft_replies (id, tenant_id, email_id, body, variant, model_used, confidence) VALUES ($1, $2, $3, 'Draft B', 'casual', 'gemma3:12b', 0.85)`,
			draftIDB, fixtureB.tenantID, fixtureB.emailID)
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM draft_replies WHERE id IN ($1, $2)", draftIDA, draftIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "draft_replies", "id")
		assert.Contains(t, idsA, draftIDA)
		assert.NotContains(t, idsA, draftIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "draft_replies", "id")
		assert.Contains(t, idsB, draftIDB)
		assert.NotContains(t, idsB, draftIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "draft_replies", "id", draftIDB, "body = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "draft_replies", "id", draftIDB)
		assert.Equal(t, int64(0), affected)
	})

	// --- automation_rules table ---
	t.Run("automation_rules", func(t *testing.T) {
		ctx := context.Background()
		ruleIDA := uuid.New()
		ruleIDB := uuid.New()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO automation_rules (id, tenant_id, user_id, name, trigger_type) VALUES ($1, $2, $3, 'Rule A', 'email_received')`,
			ruleIDA, fixtureA.tenantID, fixtureA.userID)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO automation_rules (id, tenant_id, user_id, name, trigger_type) VALUES ($1, $2, $3, 'Rule B', 'schedule')`,
			ruleIDB, fixtureB.tenantID, fixtureB.userID)
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM automation_rules WHERE id IN ($1, $2)", ruleIDA, ruleIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "automation_rules", "id")
		assert.Contains(t, idsA, ruleIDA)
		assert.NotContains(t, idsA, ruleIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "automation_rules", "id")
		assert.Contains(t, idsB, ruleIDB)
		assert.NotContains(t, idsB, ruleIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "automation_rules", "id", ruleIDB, "name = 'hacked'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "automation_rules", "id", ruleIDB)
		assert.Equal(t, int64(0), affected)
	})

	// --- subscriptions table ---
	t.Run("subscriptions", func(t *testing.T) {
		ctx := context.Background()
		subIDA := uuid.New()
		subIDB := uuid.New()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO subscriptions (id, tenant_id, stripe_customer_id, plan) VALUES ($1, $2, $3, 'free')`,
			subIDA, fixtureA.tenantID, "cus_test_a_"+fixtureA.tenantID.String()[:8])
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO subscriptions (id, tenant_id, stripe_customer_id, plan) VALUES ($1, $2, $3, 'attache')`,
			subIDB, fixtureB.tenantID, "cus_test_b_"+fixtureB.tenantID.String()[:8])
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM subscriptions WHERE id IN ($1, $2)", subIDA, subIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "subscriptions", "id")
		assert.Contains(t, idsA, subIDA)
		assert.NotContains(t, idsA, subIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "subscriptions", "id")
		assert.Contains(t, idsB, subIDB)
		assert.NotContains(t, idsB, subIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "subscriptions", "id", subIDB, "plan = 'estate'")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "subscriptions", "id", subIDB)
		assert.Equal(t, int64(0), affected)
	})

	// --- usage_logs table ---
	t.Run("usage_logs", func(t *testing.T) {
		ctx := context.Background()
		logIDA := uuid.New()
		logIDB := uuid.New()

		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO usage_logs (id, tenant_id, user_id, action, tokens_used) VALUES ($1, $2, $3, 'categorize', 100)`,
			logIDA, fixtureA.tenantID, fixtureA.userID)
		require.NoError(t, err)
		_, err = conn.Exec(ctx,
			`INSERT INTO usage_logs (id, tenant_id, user_id, action, tokens_used) VALUES ($1, $2, $3, 'summarize', 200)`,
			logIDB, fixtureB.tenantID, fixtureB.userID)
		require.NoError(t, err)
		conn.Release()

		t.Cleanup(func() {
			c, err := pool.Acquire(ctx)
			if err == nil {
				c.Exec(ctx, "DELETE FROM usage_logs WHERE id IN ($1, $2)", logIDA, logIDB)
				c.Release()
			}
		})

		idsA := queryIDsAsRole(t, pool, fixtureA.tenantID, "usage_logs", "id")
		assert.Contains(t, idsA, logIDA)
		assert.NotContains(t, idsA, logIDB)

		idsB := queryIDsAsRole(t, pool, fixtureB.tenantID, "usage_logs", "id")
		assert.Contains(t, idsB, logIDB)
		assert.NotContains(t, idsB, logIDA)

		affected := rlsUpdateCount(t, pool, fixtureA.tenantID, "usage_logs", "id", logIDB, "tokens_used = 999")
		assert.Equal(t, int64(0), affected)

		affected = rlsDeleteCount(t, pool, fixtureA.tenantID, "usage_logs", "id", logIDB)
		assert.Equal(t, int64(0), affected)
	})
}
