package email

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	// IMAPConnectionsActive tracks the number of active IMAP connections.
	IMAPConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "regent_imap_connections_active",
		Help: "Number of active IMAP connections",
	})

	// IMAPConnectionsErrored tracks the number of IMAP connections in error state.
	IMAPConnectionsErrored = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "regent_imap_connections_errored",
		Help: "Number of IMAP connections in error state",
	})

	// IMAPReconnectsTotal counts total IMAP reconnection attempts.
	IMAPReconnectsTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "regent_imap_reconnects_total",
		Help: "Total number of IMAP reconnection attempts",
	})

	// EmailsFetchedTotal counts total emails fetched, labeled by provider.
	EmailsFetchedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "regent_emails_fetched_total",
		Help: "Total emails fetched",
	}, []string{"provider"})

	// EmailFetchDuration tracks email fetch duration in seconds, labeled by provider.
	EmailFetchDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "regent_email_fetch_duration_seconds",
		Help:    "Email fetch duration in seconds",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"provider"})

	// EmailsSentTotal counts total emails sent, labeled by method (smtp, gmail_api).
	EmailsSentTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "regent_emails_sent_total",
		Help: "Total emails sent",
	}, []string{"method"})

	// SyncProgress tracks sync progress percentage per account.
	SyncProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "regent_sync_progress_pct",
		Help: "Sync progress percentage per account",
	}, []string{"account_id"})
)

func init() {
	prometheus.MustRegister(
		IMAPConnectionsActive,
		IMAPConnectionsErrored,
		IMAPReconnectsTotal,
		EmailsFetchedTotal,
		EmailFetchDuration,
		EmailsSentTotal,
		SyncProgress,
	)
}
