package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestNewCollection(t *testing.T) {
	metrics := NewCollection()
	require.NotNil(t, metrics)
}

func TestMustRegister(t *testing.T) {
	metrics := NewCollection()

	// register metrics that need labels
	metrics.RelaysPerPool.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(5)
	metrics.PoolsPledgeMet.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1)
	metrics.PoolsSaturationLevel.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(85)
	metrics.MonitoredValidatorsCount.WithLabelValues("active").Set(10)

	registry := prometheus.NewRegistry()
	metrics.MustRegister(registry)

	// The expected number of metrics to be registered, based on the definitions provided in the Collection struct.
	expectedMetricsCount := 4

	var totalRegisteredMetrics int
	size, _ := registry.Gather()
	for _, item := range size {
		if strings.HasPrefix(*item.Name, "cardano_validator_watcher") {
			totalRegisteredMetrics++
		}
	}

	require.NotNil(t, metrics)
	require.Equal(t, expectedMetricsCount, totalRegisteredMetrics)
}
