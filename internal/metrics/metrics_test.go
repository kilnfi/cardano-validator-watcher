package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestNewCollection(t *testing.T) {
	metrics := NewCollection()
	require.NotNil(t, metrics)
}

func TestMustRegister(t *testing.T) {
	metrics := NewCollection()

	// register metrics that need labels
	metrics.ChainID.WithLabelValues("test_chain").Set(1)
	metrics.RelaysPerPool.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(5)
	metrics.PoolsPledgeMet.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1)
	metrics.PoolsSaturationLevel.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(85)
	metrics.PoolLiveStake.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1e12)
	metrics.PoolActiveStake.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1e12)
	metrics.PoolLiveDelegators.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(42)
	metrics.PoolLivePledge.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1e9)
	metrics.PoolDeclaredPledge.WithLabelValues("pool_name", "pool_id", "pool_instance").Set(1e9)
	metrics.MonitoredValidatorsCount.WithLabelValues("active").Set(10)
	metrics.MissedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Inc()
	metrics.ConsecutiveMissedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Inc()
	metrics.ValidatedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Inc()
	metrics.OrphanedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Inc()
	metrics.ExpectedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Set(2)
	metrics.NextSlotLeader.WithLabelValues("pool_name", "pool_id", "pool_instance", "epoch").Set(2)

	registry := prometheus.NewRegistry()
	metrics.MustRegister(registry)

	// The expected number of metrics to be registered, based on the definitions provided in the Collection struct.
	expectedMetricsCount := 27

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

func TestDeleteEpochSeries(t *testing.T) {
	metrics := NewCollection()

	for _, epoch := range []string{"100", "101"} {
		metrics.ExpectedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", epoch).Set(2)
		metrics.ConsecutiveMissedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", epoch).Set(1)
		metrics.NextSlotLeader.WithLabelValues("pool_name", "pool_id", "pool_instance", epoch).Set(3)
	}

	metrics.DeleteEpochSeries("100")

	require.Equal(t, 1, testutil.CollectAndCount(metrics.ExpectedBlocks))
	require.Equal(t, 1, testutil.CollectAndCount(metrics.ConsecutiveMissedBlocks))
	require.Equal(t, 1, testutil.CollectAndCount(metrics.NextSlotLeader))
	require.InDelta(t, 2, testutil.ToFloat64(metrics.ExpectedBlocks.WithLabelValues("pool_name", "pool_id", "pool_instance", "101")), 0)
}
