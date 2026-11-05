package watcher

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
	"time"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// expectLeaderElection provides the network values used to compute the leader slot odds:
// 5% active slots, 432000 slots per epoch and 5.8e15 lovelace of active stake.
// expectExcludedPool answers for pool-1, which is excluded in setupPools.
func expectExcludedPool(c *clients) {
	c.bf.EXPECT().GetPoolInfo(mock.Anything, "pool-1").
		Return(bfAPI.Pool{ActiveStake: "1000000", BlocksMinted: 42}, nil).Maybe()
}

func expectLeaderElection(c *clients) {
	c.bf.EXPECT().GetGenesisInfo(mock.Anything).
		Return(bfAPI.GenesisBlock{ActiveSlotsCoefficient: 0.05, EpochLength: 432000}, nil).Maybe()
	c.bf.EXPECT().GetNetworkInfo(mock.Anything).
		Return(bfAPI.NetworkInfo{Stake: bfAPI.NetworkStake{Active: "5800000000000000"}}, nil).Maybe()
}

func TestPoolWatcher_Start(t *testing.T) {
	t.Parallel()
	pool := setupPools(t)

	t.Run("GoodPath_AllMetricsCollected", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectLeaderElection(clients)
		expectExcludedPool(clients)
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
cardano_validator_watcher_pool_relays{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 2
# HELP cardano_validator_watcher_pool_saturation_level The current saturation level of the pool in percent
# TYPE cardano_validator_watcher_pool_saturation_level gauge
cardano_validator_watcher_pool_saturation_level{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0.75
# HELP cardano_validator_watcher_pool_live_stake Current stake delegated to the pool in lovelace (updated in real time)
# TYPE cardano_validator_watcher_pool_live_stake gauge
cardano_validator_watcher_pool_live_stake{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 5.8e+13
# HELP cardano_validator_watcher_pool_active_stake Stake of the pool in lovelace taken from the snapshot used for the current epoch
# TYPE cardano_validator_watcher_pool_active_stake gauge
cardano_validator_watcher_pool_active_stake{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 5.7e+13
# HELP cardano_validator_watcher_pool_live_delegators Current number of delegators of the pool
# TYPE cardano_validator_watcher_pool_live_delegators gauge
cardano_validator_watcher_pool_live_delegators{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1234
# HELP cardano_validator_watcher_pool_live_pledge Current stake of the pool owners in lovelace
# TYPE cardano_validator_watcher_pool_live_pledge gauge
cardano_validator_watcher_pool_live_pledge{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1e+06
# HELP cardano_validator_watcher_pool_declared_pledge Pledge declared in the pool registration certificate in lovelace
# TYPE cardano_validator_watcher_pool_declared_pledge gauge
cardano_validator_watcher_pool_declared_pledge{pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 500000
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_pool_drep_registered",
			"cardano_validator_watcher_pool_pledge_met",
			"cardano_validator_watcher_pool_relays",
			"cardano_validator_watcher_pool_saturation_level",
			"cardano_validator_watcher_pool_live_stake",
			"cardano_validator_watcher_pool_active_stake",
			"cardano_validator_watcher_pool_live_delegators",
			"cardano_validator_watcher_pool_live_pledge",
			"cardano_validator_watcher_pool_declared_pledge",
		}

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LiveStake:      "58000000000000",
				ActiveStake:    "57000000000000",
				LiveDelegators: 1234,
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
			Return(bfAPI.Account{DrepID: &drepID}, nil)

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

		// ~0.98% of the active stake: ~218 leader slots expected, no chance to get none
		expected := testutil.ToFloat64(registry.metrics.PoolExpectedLeaderSlots.WithLabelValues("pool-0", "pool-0", "pool-0"))
		require.InDelta(t, 217.7, expected, 0.1)
		noSlot := testutil.ToFloat64(registry.metrics.PoolNoLeaderSlotProbability.WithLabelValues("pool-0", "pool-0", "pool-0"))
		require.Less(t, noSlot, 1e-90)
	})

	t.Run("GoodPath_PledgeNotMet_NoDRep", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectLeaderElection(clients)
		expectExcludedPool(clients)
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

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.5,
				LiveStake:      "1000000",
				ActiveStake:    "1000000",
				LivePledge:     "500000",
				DeclaredPledge: "1000000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{{}}, nil)

		clients.bf.EXPECT().
			GetAccountInfo(mock.Anything, "stake1test").
			Return(bfAPI.Account{DrepID: nil}, nil)

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

	t.Run("SadPath_GetPoolInfoFails", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectLeaderElection(clients)
		expectExcludedPool(clients)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

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
		expectLeaderElection(clients)
		expectExcludedPool(clients)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LiveStake:      "1000000",
				ActiveStake:    "1000000",
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
		expectLeaderElection(clients)
		expectExcludedPool(clients)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LiveStake:      "1000000",
				ActiveStake:    "1000000",
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
		expectLeaderElection(clients)
		expectExcludedPool(clients)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LiveStake:      "1000000",
				ActiveStake:    "1000000",
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
		expectLeaderElection(clients)
		expectExcludedPool(clients)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies

		clients.bf.EXPECT().
			GetPoolInfo(mock.Anything, pool[0].ID).
			Return(bfAPI.Pool{
				LiveSaturation: 0.75,
				LiveStake:      "1000000",
				ActiveStake:    "1000000",
				LivePledge:     "1000000",
				DeclaredPledge: "500000",
				RewardAccount:  "stake1test",
			}, nil)

		clients.bf.EXPECT().
			GetPoolRelays(mock.Anything, pool[0].ID).
			Return([]bfAPI.PoolRelay{{}, {}}, nil)

		clients.bf.EXPECT().
			GetAccountInfo(mock.Anything, "stake1test").
			Return(bfAPI.Account{}, errors.New("account API error"))

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
		expectLeaderElection(clients)
		expectExcludedPool(clients)
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

func TestLeaderOdds(t *testing.T) {
	t.Parallel()

	// mainnet values: f = 0.05, 432000 slots per epoch
	expected, noSlot := leaderOdds(0.001, 0.05, 432000)
	require.InDelta(t, 22.16, expected, 0.01)
	require.InDelta(t, math.Exp(-expected), noSlot, 1e-12)

	// a small pool (~1.4 slots expected) has about 1 chance in 4 to get no slot
	expected, noSlot = leaderOdds(6.2e-5, 0.05, 432000)
	require.InDelta(t, 1.37, expected, 0.01)
	require.InDelta(t, 0.25, noSlot, 0.01)

	expected, noSlot = leaderOdds(0, 0.05, 432000)
	require.InDelta(t, 0, expected, 0)
	require.InDelta(t, 1, noSlot, 0)
}

func TestPoolWatcher_ExcludedPoolStaysVisible(t *testing.T) {
	t.Parallel()
	pool := setupPools(t)

	clients := setupClients(t)
	expectLeaderElection(clients)
	registry := setupRegistry(t)
	ctx := setupContextWithTimeout(t, time.Second*5)

	clients.bf.EXPECT().GetPoolInfo(mock.Anything, pool[0].ID).Return(bfAPI.Pool{
		LiveStake: "1000000", ActiveStake: "1000000", LivePledge: "1", DeclaredPledge: "1",
		RewardAccount: "stake1test", BlocksMinted: 7,
	}, nil)
	clients.bf.EXPECT().GetPoolRelays(mock.Anything, pool[0].ID).Return([]bfAPI.PoolRelay{{}}, nil)
	clients.bf.EXPECT().GetAccountInfo(mock.Anything, "stake1test").Return(bfAPI.Account{}, nil)
	clients.bf.EXPECT().GetPoolInfo(mock.Anything, pool[1].ID).Return(bfAPI.Pool{ActiveStake: "58000000000000", BlocksMinted: 42}, nil)

	healthStore := NewHealthStore()
	healthStore.SetHealth(true)
	watcher, err := NewPoolWatcher(clients.bf, registry.metrics, pool, healthStore, PoolWatcherOptions{RefreshInterval: time.Minute})
	require.NoError(t, err)
	require.ErrorIs(t, watcher.Start(ctx), context.Canceled)

	excluded := []string{pool[1].Name, pool[1].ID, pool[1].Instance}
	require.InDelta(t, 1, testutil.ToFloat64(registry.metrics.PoolExcluded.WithLabelValues(excluded...)), 0)
	require.InDelta(t, 42, testutil.ToFloat64(registry.metrics.PoolBlocksMinted.WithLabelValues(excluded...)), 0)
	require.InDelta(t, 221.5, testutil.ToFloat64(registry.metrics.PoolExpectedLeaderSlots.WithLabelValues(excluded...)), 0.1)
	require.InDelta(t, 0, testutil.ToFloat64(registry.metrics.PoolExcluded.WithLabelValues(pool[0].Name, pool[0].ID, pool[0].Instance)), 0)
}
