package behavior

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/darshan-kheni/regent/internal/database"
)

// computeToneConsistency checks if tone distribution shifted significantly over 7 days.
// ok < 15% shift, warn 15-30%, critical > 30%
func (s *BehaviorService) computeToneConsistency(ctx database.TenantContext, userID uuid.UUID, date time.Time) StressIndicator {
	ind := StressIndicator{Metric: "tone_consistency", Status: "ok"}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		ind.Detail = "failed to acquire connection"
		return ind
	}
	defer conn.Release()

	if err := database.SetRLSContext(ctx, conn); err != nil {
		ind.Detail = "failed to set RLS context"
		return ind
	}

	// Get tone distributions for the last 7 daily periods
	rows, err := conn.Query(ctx,
		`SELECT period_start, tone_distribution
		 FROM communication_metrics
		 WHERE user_id = $1 AND period_type = 'daily'
		   AND period_start >= $2::date - interval '7 days'
		   AND period_start <= $2
		   AND tone_distribution IS NOT NULL
		 ORDER BY period_start DESC`,
		userID, date.Format("2006-01-02"),
	)
	if err != nil {
		ind.Detail = "failed to query tone data"
		return ind
	}
	defer rows.Close()

	var distributions []map[string]float64
	for rows.Next() {
		var periodStart time.Time
		var toneJSON []byte
		if err := rows.Scan(&periodStart, &toneJSON); err != nil {
			continue
		}
		var dist map[string]float64
		if err := json.Unmarshal(toneJSON, &dist); err != nil {
			continue
		}
		distributions = append(distributions, dist)
	}

	if len(distributions) < 3 {
		ind.Value = "N/A"
		ind.Detail = "Insufficient tone data (need 3+ days)"
		return ind
	}

	// Find dominant tone from baseline (older entries)
	baseline := aggregateToneDistributions(distributions[1:])
	current := distributions[0]

	// Find dominant tone in baseline
	var dominantTone string
	var dominantPct float64
	for tone, pct := range baseline {
		if pct > dominantPct {
			dominantTone = tone
			dominantPct = pct
		}
	}

	if dominantTone == "" {
		ind.Value = "N/A"
		ind.Detail = "No dominant tone detected"
		return ind
	}

	// Calculate shift
	currentDominantPct := current[dominantTone]
	shift := math.Abs(currentDominantPct - dominantPct)

	ind.Value = fmt.Sprintf("%s %.0f%%", dominantTone, currentDominantPct)

	switch {
	case shift > 30:
		ind.Status = "critical"
		ind.Delta = fmt.Sprintf("-%.0f%% from %.0f%%", shift, dominantPct)
		ind.Detail = fmt.Sprintf("Major tone shift: %s dropped from %.0f%% to %.0f%%", dominantTone, dominantPct, currentDominantPct)
	case shift > 15:
		ind.Status = "warn"
		ind.Delta = fmt.Sprintf("-%.0f%% from %.0f%%", shift, dominantPct)
		ind.Detail = fmt.Sprintf("Tone shift detected: %s changed by %.0f%%", dominantTone, shift)
	default:
		ind.Status = "ok"
		ind.Delta = fmt.Sprintf("%.0f%% stable", shift)
		ind.Detail = "Tone distribution stable"
	}

	return ind
}

// aggregateToneDistributions averages multiple tone distributions.
func aggregateToneDistributions(dists []map[string]float64) map[string]float64 {
	totals := make(map[string]float64)
	count := float64(len(dists))
	if count == 0 {
		return totals
	}
	for _, dist := range dists {
		for tone, pct := range dist {
			totals[tone] += pct
		}
	}
	for tone := range totals {
		totals[tone] /= count
	}
	return totals
}
