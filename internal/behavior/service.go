package behavior

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/darshan-kheni/regent/internal/ai"
)

// BehaviorService handles all behavior intelligence computations.
type BehaviorService struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
	ai   ai.AIProvider
}

// NewBehaviorService creates a new BehaviorService.
func NewBehaviorService(pool *pgxpool.Pool, rdb *redis.Client, aiProvider ai.AIProvider) *BehaviorService {
	return &BehaviorService{
		pool: pool,
		rdb:  rdb,
		ai:   aiProvider,
	}
}
