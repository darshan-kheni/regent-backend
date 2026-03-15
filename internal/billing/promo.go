package billing

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/database"
)

// PromoResult is returned from validation and describes what a promo code does.
type PromoResult struct {
	Valid           bool   `json:"valid"`
	Type            string `json:"type,omitempty"`
	Message         string `json:"message"`
	DiscountPercent *int   `json:"discount_percent,omitempty"`
	TrialDays       *int   `json:"trial_days,omitempty"`
	Plan            string `json:"plan,omitempty"`
}

// PromoCode represents a row in the promo_codes table.
type PromoCode struct {
	ID              uuid.UUID
	Code            string
	Type            string // "discount" or "trial"
	DiscountPercent *int
	TrialDays       *int
	Plan            string
	MaxUses         *int
	CurrentUses     int
	ValidFrom       time.Time
	ValidUntil      *time.Time
	Active          bool
	CreatedBy       *uuid.UUID
	CreatedAt       time.Time
}

// PromoService handles promo code validation and redemption.
type PromoService struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

// NewPromoService creates a new PromoService.
func NewPromoService(pool *pgxpool.Pool, rdb *redis.Client) *PromoService {
	return &PromoService{pool: pool, rdb: rdb}
}

// Validate checks whether a promo code is valid for the given user without applying it.
func (s *PromoService) Validate(ctx database.TenantContext, code string, userID uuid.UUID) (*PromoResult, error) {
	promo, err := s.lookupCode(ctx, code)
	if err != nil {
		return &PromoResult{Valid: false, Message: "Invalid promo code"}, nil
	}

	if err := s.checkEligibility(ctx, promo, userID); err != nil {
		return &PromoResult{Valid: false, Message: err.Error()}, nil
	}

	result := &PromoResult{
		Valid:   true,
		Type:    promo.Type,
		Plan:    promo.Plan,
		Message: fmt.Sprintf("Code %s is valid", promo.Code),
	}
	if promo.Type == "discount" {
		result.DiscountPercent = promo.DiscountPercent
		result.Message = fmt.Sprintf("%d%% discount on %s plan", *promo.DiscountPercent, promo.Plan)
	} else {
		result.TrialDays = promo.TrialDays
		result.Message = fmt.Sprintf("%d-day free trial of %s plan", *promo.TrialDays, promo.Plan)
	}

	return result, nil
}

// Apply validates and atomically redeems a promo code for the given user.
func (s *PromoService) Apply(ctx database.TenantContext, code string, userID uuid.UUID) error {
	promo, err := s.lookupCode(ctx, code)
	if err != nil {
		return fmt.Errorf("invalid promo code: %w", err)
	}

	if err := s.checkEligibility(ctx, promo, userID); err != nil {
		return fmt.Errorf("promo not eligible: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Atomic claim: increment uses only if still below max
	var claimedID uuid.UUID
	err = tx.QueryRow(ctx,
		`UPDATE promo_codes
		 SET current_uses = current_uses + 1
		 WHERE id = $1
		   AND (max_uses IS NULL OR current_uses < max_uses)
		 RETURNING id`,
		promo.ID,
	).Scan(&claimedID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return fmt.Errorf("promo code is no longer available")
		}
		return fmt.Errorf("claiming promo code: %w", err)
	}

	// Route to type-specific logic
	switch promo.Type {
	case "discount":
		// Insert redemption record
		if err := s.insertRedemption(ctx, tx, promo, userID, nil); err != nil {
			return fmt.Errorf("inserting discount redemption: %w", err)
		}
		// Apply Stripe discount (done outside tx since it's an external call)
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing discount redemption: %w", err)
		}
		if err := s.applyDiscount(ctx, promo); err != nil {
			slog.Error("billing: failed to apply Stripe discount, redemption recorded",
				"code", promo.Code,
				"tenant_id", ctx.TenantID,
				"error", err,
			)
			return fmt.Errorf("applying Stripe discount: %w", err)
		}
		return nil

	case "trial":
		trialEnd := time.Now().UTC().Add(time.Duration(*promo.TrialDays) * 24 * time.Hour)
		if err := s.insertRedemption(ctx, tx, promo, userID, &trialEnd); err != nil {
			return fmt.Errorf("inserting trial redemption: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing trial redemption: %w", err)
		}
		if err := s.provisionTrial(ctx, userID, promo); err != nil {
			slog.Error("billing: failed to provision trial, redemption recorded",
				"code", promo.Code,
				"tenant_id", ctx.TenantID,
				"error", err,
			)
			return fmt.Errorf("provisioning trial: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unknown promo type: %s", promo.Type)
	}
}

// ListAll returns all promo codes (admin use, no RLS).
func (s *PromoService) ListAll(ctx context.Context) ([]PromoCode, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, code, type, discount_percent, trial_days, plan,
		        max_uses, current_uses, valid_from, valid_until, active,
		        created_by, created_at
		 FROM promo_codes
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("querying promo codes: %w", err)
	}
	defer rows.Close()

	var codes []PromoCode
	for rows.Next() {
		var pc PromoCode
		if err := rows.Scan(
			&pc.ID, &pc.Code, &pc.Type, &pc.DiscountPercent, &pc.TrialDays,
			&pc.Plan, &pc.MaxUses, &pc.CurrentUses, &pc.ValidFrom, &pc.ValidUntil,
			&pc.Active, &pc.CreatedBy, &pc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning promo code: %w", err)
		}
		codes = append(codes, pc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating promo codes: %w", err)
	}
	return codes, nil
}

// Create inserts a new promo code (admin use).
func (s *PromoService) Create(ctx context.Context, pc PromoCode) (*PromoCode, error) {
	var created PromoCode
	err := s.pool.QueryRow(ctx,
		`INSERT INTO promo_codes (code, type, discount_percent, trial_days, plan, max_uses, valid_from, valid_until, active, created_by)
		 VALUES (UPPER($1), $2, $3, $4, $5, $6, COALESCE($7, now()), $8, true, $9)
		 RETURNING id, code, type, discount_percent, trial_days, plan, max_uses, current_uses, valid_from, valid_until, active, created_by, created_at`,
		pc.Code, pc.Type, pc.DiscountPercent, pc.TrialDays, pc.Plan,
		pc.MaxUses, pc.ValidFrom, pc.ValidUntil, pc.CreatedBy,
	).Scan(
		&created.ID, &created.Code, &created.Type, &created.DiscountPercent,
		&created.TrialDays, &created.Plan, &created.MaxUses, &created.CurrentUses,
		&created.ValidFrom, &created.ValidUntil, &created.Active, &created.CreatedBy,
		&created.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating promo code: %w", err)
	}
	return &created, nil
}

// Deactivate sets a promo code to inactive.
func (s *PromoService) Deactivate(ctx context.Context, codeID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		"UPDATE promo_codes SET active = false WHERE id = $1",
		codeID,
	)
	if err != nil {
		return fmt.Errorf("deactivating promo code: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("promo code not found")
	}
	return nil
}

// lookupCode fetches a promo code by its code string.
func (s *PromoService) lookupCode(ctx context.Context, code string) (PromoCode, error) {
	var pc PromoCode
	err := s.pool.QueryRow(ctx,
		`SELECT id, code, type, discount_percent, trial_days, plan,
		        max_uses, current_uses, valid_from, valid_until, active
		 FROM promo_codes
		 WHERE UPPER(code) = UPPER($1)`,
		strings.TrimSpace(code),
	).Scan(
		&pc.ID, &pc.Code, &pc.Type, &pc.DiscountPercent, &pc.TrialDays,
		&pc.Plan, &pc.MaxUses, &pc.CurrentUses, &pc.ValidFrom, &pc.ValidUntil,
		&pc.Active,
	)
	if err != nil {
		return PromoCode{}, fmt.Errorf("looking up promo code: %w", err)
	}
	return pc, nil
}

// checkEligibility verifies that the promo code can be used by this user.
func (s *PromoService) checkEligibility(ctx database.TenantContext, promo PromoCode, userID uuid.UUID) error {
	if !promo.Active {
		return fmt.Errorf("promo code is no longer active")
	}

	now := time.Now().UTC()
	if now.Before(promo.ValidFrom) {
		return fmt.Errorf("promo code is not yet valid")
	}
	if promo.ValidUntil != nil && now.After(*promo.ValidUntil) {
		return fmt.Errorf("promo code has expired")
	}

	if promo.MaxUses != nil && promo.CurrentUses >= *promo.MaxUses {
		return fmt.Errorf("promo code has reached maximum uses")
	}

	// Check if user already redeemed this code
	var exists bool
	err := s.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM promo_redemptions WHERE code_id = $1 AND user_id = $2)",
		promo.ID, userID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking redemption history: %w", err)
	}
	if exists {
		return fmt.Errorf("you have already used this promo code")
	}

	return nil
}

// insertRedemption records a promo code redemption within an existing transaction.
func (s *PromoService) insertRedemption(ctx database.TenantContext, tx pgx.Tx, promo PromoCode, userID uuid.UUID, trialEnd *time.Time) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO promo_redemptions (tenant_id, code_id, user_id, applied_plan, trial_end_date)
		 VALUES ($1, $2, $3, $4, $5)`,
		ctx.TenantID, promo.ID, userID, promo.Plan, trialEnd,
	)
	if err != nil {
		return fmt.Errorf("inserting redemption: %w", err)
	}
	return nil
}
