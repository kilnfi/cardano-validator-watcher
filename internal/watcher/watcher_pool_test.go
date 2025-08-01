package watcher

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testTicker = "TEST"

func TestPoolWatcher_Start(t *testing.T) {
	t.Parallel()
	pool := setupPools(t)

	t.Run("GoodPath_AllMetricsCollected", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_pool_drep_registered Whether the pool owner is registered to a DRep (0 or 1)
# TYPE cardano_validator_watcher_pool_drep_registered gauge
cardano_validator_watcher_pool_drep_registered{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
# HELP cardano_validator_watcher_pool_pledge_met Whether the pool has met its pledge requirements or not (0 or 1)
# TYPE cardano_validator_watcher_pool_pledge_met gauge
cardano_validator_watcher_pool_pledge_met{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
# HELP cardano_validator_watcher_pool_relays Count of relays associated with each pool
# TYPE cardano_validator_watcher_pool_relays gauge
cardano_validator_watcher_pool_relays{pool_id="pool-0",pool_instance="pool-0",pool_name="` + testTicker + `"} 2
# HELP cardano_validator_watcher_pool_saturation_level The current saturation level of the pool in percent
# TYPE cardano_validator_watcher_pool_saturation_level gauge
cardano_validator_watcher_pool_saturation_level{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0.75
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_pool_drep_registered",
			"cardano_validator_watcher_pool_pledge_met",
			"cardano_validator_watcher_pool_relays",
			"cardano_validator_watcher_pool_saturation_level",
		}

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LivePledge:     "1000000",
				DeclaredPledge: "500000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{{}, {}}, nil)

		drepID := "drep1test"
		clients.bf.EXPECT().
			GetAccountInfo(mock.Anything, "stake1test").
			Return(blockfrost.Account{DrepID: &drepID}, nil)

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PledgeNotMet_NoDRep", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_pool_drep_registered Whether the pool owner is registered to a DRep (0 or 1)
# TYPE cardano_validator_watcher_pool_drep_registered gauge
cardano_validator_watcher_pool_drep_registered{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0
# HELP cardano_validator_watcher_pool_pledge_met Whether the pool has met its pledge requirements or not (0 or 1)
# TYPE cardano_validator_watcher_pool_pledge_met gauge
cardano_validator_watcher_pool_pledge_met{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_pool_drep_registered",
			"cardano_validator_watcher_pool_pledge_met",
		}

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.5,
				LivePledge:     "500000",
				DeclaredPledge: "1000000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{{}}, nil)

		clients.bf.EXPECT().
			GetAccountInfo(mock.Anything, "stake1test").
			Return(blockfrost.Account{DrepID: nil}, nil)

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_GetPoolMetadataFails", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{}, errors.New("metadata API error"))

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_GetPoolInfoFails", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{}, errors.New("pool info API error"))

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_InvalidLivePledgeConversion", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LivePledge:     "invalid_number",
				DeclaredPledge: "500000",
				RewardAccount:  "stake1test",
			}, nil)

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_InvalidDeclaredPledgeConversion", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LivePledge:     "1000000",
				DeclaredPledge: "not_a_number",
				RewardAccount:  "stake1test",
			}, nil)

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_GetPoolRelaysFails", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LivePledge:     "1000000",
				DeclaredPledge: "500000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{}, errors.New("relays API error"))

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_GetAccountInfoFails", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		ticker := testTicker
		clients.bf.EXPECT().
			GetPoolMetadata(mock.Anything, pool[0].ID).
			Return(bfAPI.PoolMetadata{Ticker: &ticker}, nil)

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LivePledge:     "1000000",
				DeclaredPledge: "500000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{{}, {}}, nil)

		clients.bf.EXPECT().
			GetAccountInfo(mock.Anything, "stake1test").
			Return(blockfrost.Account{}, errors.New("account API error"))

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("SadPath_WatcherIsNotReady", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*2)

		// No mocks setup - no calls should be made when not healthy

		options := PoolWatcherOptions{
			RefreshInterval: time.Minute * 1,
			Network:         "testnet",
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(false)
		watcher, err := NewPoolWatcher(
			clients.bf,
			registry.metrics,
			pool,
			healthStore,
			options,
		)
		require.NoError(t, err)

		err = watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}
