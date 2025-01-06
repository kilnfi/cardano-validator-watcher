package watcher

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/blockfrost/blockfrost-go"
	bfMocks "github.com/kilnfi/cardano-validator-watcher/internal/blockfrost/mocks"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	metricsOutputTemplate = `
# HELP cardano_validator_watcher_monitored_validators number of validators monitored by the watcher
# TYPE cardano_validator_watcher_monitored_validators gauge
cardano_validator_watcher_monitored_validators{status="active"} %d
cardano_validator_watcher_monitored_validators{status="excluded"} %d
cardano_validator_watcher_monitored_validators{status="total"} %d
# HELP cardano_validator_watcher_pool_pledge_met Whether the pool has met its pledge requirements or not (0 or 1)
# TYPE cardano_validator_watcher_pool_pledge_met gauge
cardano_validator_watcher_pool_pledge_met{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} %d
# HELP cardano_validator_watcher_pool_relays Count of relays associated with each pool
# TYPE cardano_validator_watcher_pool_relays gauge
cardano_validator_watcher_pool_relays{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} %d
# HELP cardano_validator_watcher_pool_saturation_level The current saturation level of the pool in percent
# TYPE cardano_validator_watcher_pool_saturation_level gauge
cardano_validator_watcher_pool_saturation_level{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} %f
`
)

func TestPoolWatcher_Start(t *testing.T) {
	t.Parallel()
	t.Run("GoodPath_CollectAllMetrics", func(t *testing.T) {
		t.Parallel()

		pools := setupPools(t)
		poolStats := pools.GetPoolStats()
		saturation := 0.5
		declaredPledge := "100"
		livePledge := "200"
		relays := []blockfrost.PoolRelay{
			{
				Ipv4: &[]string{"10.10.10.10"}[0],
				DNS:  &[]string{"relays.example.com"}[0],
				Port: 3001,
			},
		}

		metricsExpectedOutput := fmt.Sprintf(metricsOutputTemplate,
			poolStats.Active,
			poolStats.Excluded,
			poolStats.Total,
			1,
			len(relays),
			saturation,
		)

		metricsUnderTest := []string{
			"cardano_validator_watcher_monitored_validators",
			"cardano_validator_watcher_pool_pledge_met",
			"cardano_validator_watcher_pool_relays",
			"cardano_validator_watcher_pool_saturation_level",
		}

		registry := prometheus.NewRegistry()
		metrics := metrics.NewCollection()
		metrics.MustRegister(registry)

		bf := bfMocks.NewMockClient(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			<-ticker.C
			cancel()
		}()

		// Mocks
		bf.EXPECT().GetPoolMetadata(mock.Anything, pools[0].ID).Return(
			blockfrost.PoolMetadata{Ticker: &pools[0].Name}, nil,
		)
		bf.EXPECT().GetPoolInfo(mock.Anything, pools[0].ID).Return(
			blockfrost.Pool{
				LiveSaturation: saturation,
				LivePledge:     livePledge,
				DeclaredPledge: declaredPledge,
			}, nil,
		)
		bf.EXPECT().GetPoolRelays(
			mock.Anything,
			pools[0].ID,
		).Return(
			relays, nil,
		)

		options := PoolWatcherOptions{RefreshInterval: 10 * time.Second, Network: "preprod"}
		watcher, err := NewPoolWatcher(
			bf,
			metrics,
			pools,
			options,
		)
		require.NoError(t, err)
		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		b := bytes.NewBufferString(metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry, b, metricsUnderTest...)
		require.NoError(t, err)
	})
}
