package watcher

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/blockfrost/blockfrost-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	networkMetricTemplate = `
# HELP cardano_validator_watcher_chain_id Chain ID
# TYPE cardano_validator_watcher_chain_id gauge
cardano_validator_watcher_chain_id{chain_id="preprod"} %d
# HELP cardano_validator_watcher_network_active_stake Total active stake in the network
# TYPE cardano_validator_watcher_network_active_stake gauge
cardano_validator_watcher_network_active_stake %d
# HELP cardano_validator_watcher_network_block_height Latest known block height
# TYPE cardano_validator_watcher_network_block_height gauge
cardano_validator_watcher_network_block_height %d
# HELP cardano_validator_watcher_network_blocks_proposed_current_epoch Number of blocks proposed in the current epoch by the network
# TYPE cardano_validator_watcher_network_blocks_proposed_current_epoch gauge
cardano_validator_watcher_network_blocks_proposed_current_epoch %d
# HELP cardano_validator_watcher_network_epoch Current epoch number
# TYPE cardano_validator_watcher_network_epoch gauge
cardano_validator_watcher_network_epoch %d
# HELP cardano_validator_watcher_network_epoch_slot Latest known epoch slot
# TYPE cardano_validator_watcher_network_epoch_slot gauge
cardano_validator_watcher_network_epoch_slot %d
# HELP cardano_validator_watcher_network_pools Total number of pools in the network
# TYPE cardano_validator_watcher_network_pools gauge
cardano_validator_watcher_network_pools %d
# HELP cardano_validator_watcher_network_slot Latest known slot
# TYPE cardano_validator_watcher_network_slot gauge
cardano_validator_watcher_network_slot %d
# HELP cardano_validator_watcher_epoch_duration Duration of an epoch in days
# TYPE cardano_validator_watcher_epoch_duration gauge
cardano_validator_watcher_epoch_duration %f
# HELP cardano_validator_watcher_next_epoch_start_time start time of the next epoch in seconds
# TYPE cardano_validator_watcher_next_epoch_start_time gauge
cardano_validator_watcher_next_epoch_start_time %d
`
)

func TestNetworkWatcher_Start(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_CollectAllMetrics", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)

		activeStake := 1000
		blockHeight := 100
		networkBlockCount := 70
		epoch := 100
		epochSlot := 100
		totalPools := 2
		networkSlot := 1000
		epochDuration := 5.0
		nextEpochStartTime := 178100200
		chainID := 1

		registry := setupRegistry(t)
		registry.metricsExpectedOutput = fmt.Sprintf(networkMetricTemplate,
			chainID,
			activeStake,
			blockHeight,
			networkBlockCount,
			epoch,
			epochSlot,
			totalPools,
			networkSlot,
			epochDuration,
			nextEpochStartTime,
		)
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_chain_id",
			"cardano_validator_watcher_network_active_stake",
			"cardano_validator_watcher_network_block_height",
			"cardano_validator_watcher_network_blocks_proposed_current_epoch",
			"cardano_validator_watcher_network_epoch",
			"cardano_validator_watcher_network_epoch_slot",
			"cardano_validator_watcher_network_pools",
			"cardano_validator_watcher_network_slot",
			"cardano_validator_watcher_epoch_duration",
			"cardano_validator_watcher_next_epoch_start_time",
		}

		// Mock the calls
		clients.bf.EXPECT().
			GetGenesisInfo(mock.Anything).
			Return(blockfrost.GenesisBlock{NetworkMagic: chainID}, nil)

		clients.bf.EXPECT().
			GetAllPools(mock.Anything).
			Return(
				[]string{
					"pool1",
					"pool2",
				},
				nil,
			)

		clients.bf.EXPECT().
			GetNetworkInfo(mock.Anything).
			Return(
				blockfrost.NetworkInfo{
					Stake: blockfrost.NetworkStake{
						Active: strconv.Itoa(activeStake),
					},
				},
				nil,
			)

		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{
					Epoch:      epoch,
					BlockCount: networkBlockCount,
					EndTime:    nextEpochStartTime,
				},
				nil,
			)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Epoch:     epoch,
					Height:    blockHeight,
					Slot:      networkSlot,
					EpochSlot: epochSlot,
				},
				nil,
			)

		ctx := setupContextWithTimeout(t, time.Second*30)
		options := NetworkWatcherOptions{
			Network:         "preprod",
			RefreshInterval: time.Second * 15,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewNetworkWatcher(clients.bf, registry.metrics, healthStore, options)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_WatcherIsNotReady", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		registry := setupRegistry(t)

		ctx := setupContextWithTimeout(t, time.Second*30)
		options := NetworkWatcherOptions{
			Network:         "preprod",
			RefreshInterval: time.Second * 15,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(false)
		watcher := NewNetworkWatcher(clients.bf, registry.metrics, healthStore, options)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})
}
