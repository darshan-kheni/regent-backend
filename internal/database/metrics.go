package database

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	dbPoolActiveConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_active_connections",
		Help: "Number of active database connections",
	})
	dbPoolIdleConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_idle_connections",
		Help: "Number of idle database connections",
	})
	dbPoolTotalConns = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_total_connections",
		Help: "Total number of database connections",
	})
)

func init() {
	prometheus.MustRegister(dbPoolActiveConns, dbPoolIdleConns, dbPoolTotalConns)
}

// UpdatePoolMetrics updates Prometheus gauges with current pool statistics.
func UpdatePoolMetrics(pool *pgxpool.Pool) {
	stat := pool.Stat()
	dbPoolActiveConns.Set(float64(stat.AcquiredConns()))
	dbPoolIdleConns.Set(float64(stat.IdleConns()))
	dbPoolTotalConns.Set(float64(stat.TotalConns()))
}
