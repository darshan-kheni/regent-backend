package behavior

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// UserCalibration stores user preferences that adjust WLB scoring.
type UserCalibration struct {
	IntentionalLateWorker bool              `json:"intentional_late_worker"`
	WeekendAcceptable     bool              `json:"weekend_acceptable"`
	CustomActiveHours     *ActiveHoursRange `json:"custom_active_hours,omitempty"`
}

// ActiveHoursRange defines custom business hours.
type ActiveHoursRange struct {
	Start string `json:"start"` // HH:MM format
	End   string `json:"end"`   // HH:MM format
}

// LoadCalibration reads the calibration JSONB from behavior_profiles.
func (s *BehaviorService) LoadCalibration(ctx database.TenantContext, userID uuid.UUID) (*UserCalibration, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return nil, fmt.Errorf("setting RLS context: %w", err)
	}

	var calibrationJSON []byte
	err = conn.QueryRow(ctx,
		`SELECT calibration FROM behavior_profiles WHERE user_id = $1`,
		userID,
	).Scan(&calibrationJSON)
	if err != nil {
		// No profile yet — return default calibration
		return &UserCalibration{}, nil
	}

	var cal UserCalibration
	if len(calibrationJSON) > 0 {
		if err := json.Unmarshal(calibrationJSON, &cal); err != nil {
			return &UserCalibration{}, nil
		}
	}
	return &cal, nil
}

// SaveCalibration writes calibration to behavior_profiles.calibration JSONB.
func (s *BehaviorService) SaveCalibration(ctx database.TenantContext, userID uuid.UUID, cal UserCalibration) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquiring connection: %w", err)
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		return fmt.Errorf("setting RLS context: %w", err)
	}

	calJSON, err := json.Marshal(cal)
	if err != nil {
		return fmt.Errorf("marshaling calibration: %w", err)
	}

	_, err = conn.Exec(ctx,
		`INSERT INTO behavior_profiles (tenant_id, user_id, calibration, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (user_id) DO UPDATE SET
			calibration = EXCLUDED.calibration,
			updated_at = now()`,
		ctx.TenantID, userID, calJSON,
	)
	if err != nil {
		return fmt.Errorf("saving calibration: %w", err)
	}
	return nil
}
