package email

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegistered(t *testing.T) {
	t.Parallel()

	// Use Describe to verify all collectors are registered, since Vec types
	// don't appear in Gather() until they have at least one label observation.
	collectors := []prometheus.Collector{
		IMAPConnectionsActive,
		IMAPConnectionsErrored,
		IMAPReconnectsTotal,
		EmailsFetchedTotal,
		EmailFetchDuration,
		EmailsSentTotal,
		SyncProgress,
	}

	for _, c := range collectors {
		ch := make(chan *prometheus.Desc, 10)
		c.Describe(ch)
		close(ch)

		desc := <-ch
		require.NotNil(t, desc, "collector should have at least one descriptor")
		assert.NotEmpty(t, desc.String(), "descriptor should have a non-empty string representation")
	}
}

func TestIMAPConnectionsActive(t *testing.T) {
	t.Parallel()

	IMAPConnectionsActive.Set(42)

	m := &dto.Metric{}
	err := IMAPConnectionsActive.Write(m)
	require.NoError(t, err)
	assert.Equal(t, float64(42), m.GetGauge().GetValue())

	// Reset for other tests.
	IMAPConnectionsActive.Set(0)
}

func TestEmailsFetchedTotal(t *testing.T) {
	t.Parallel()

	counter := EmailsFetchedTotal.WithLabelValues("gmail")
	counter.Add(5)

	m := &dto.Metric{}
	err := counter.(prometheus.Metric).Write(m)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, m.GetCounter().GetValue(), float64(5))
}

func TestEmailFetchDuration(t *testing.T) {
	t.Parallel()

	observer := EmailFetchDuration.WithLabelValues("imap")
	observer.Observe(1.5)

	// Gather to confirm the metric was recorded.
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	var found bool
	for _, f := range families {
		if f.GetName() == "regent_email_fetch_duration_seconds" {
			found = true
			require.NotEmpty(t, f.GetMetric())
			histogram := f.GetMetric()[0].GetHistogram()
			assert.Greater(t, histogram.GetSampleCount(), uint64(0))
		}
	}
	assert.True(t, found, "histogram metric should exist")
}
