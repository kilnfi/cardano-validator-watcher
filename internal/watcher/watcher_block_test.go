package watcher

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blockfrost/blockfrost-go"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/kilnfi/cardano-validator-watcher/internal/slotleader"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// expectNoReplay lets the startup replay of the processed leader slots find an empty schedule.
func expectNoReplay(c *clients) {
	c.sl.EXPECT().GetSlotLeaders(mock.Anything, mock.Anything, mock.Anything).Return(slotleader.Schedule{}, nil).Maybe()
}

func setupPool() pools.Pools {
	return pools.Pools{
		{
			ID:       "pool-0",
			Instance: "pool-0",
			Key:      "key",
			Name:     "pool-0",
		},
	}
}

func TestBlockWatcher_Start(t *testing.T) {
	t.Parallel()
	pool := setupPool()

	t.Run("GoodPath_LoadAnReconcileState_WhenEpochIsDifferent", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		epochInState := 100
		slotInState := 100
		currentEpoch := 101

		ctx := setupContextWithTimeout(t, time.Second*5)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: currentEpoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epochInState, slotInState, time.Now()),
			)

		clients.bf.EXPECT().
			GetFirstBlockInEpoch(ctx, currentEpoch).
			Return(
				blockfrost.Block{Epoch: currentEpoch, Slot: 1, EpochSlot: 0},
				nil,
			)

		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(currentEpoch, 0, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, currentEpoch).
			Return(false, nil)

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(false)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Epoch, currentEpoch)
	})

	t.Run("GoodPath_PoolWithoutLeader", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		// handle currentSlot - 1
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot-1, epoch).
			Return(false, nil)

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot, epoch).
			Return(false, nil)

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PoolValidatedABlock", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_validated_blocks_total number of validated blocks in the current epoch
# TYPE cardano_validator_watcher_validated_blocks_total counter
cardano_validator_watcher_validated_blocks_total{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 1
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_validated_blocks_total",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				}, nil,
			)

		// handle currentSlot - 1
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot-1,
				epoch,
			).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(
				mock.Anything,
				currentSlot-1,
			).
			Return(
				blockfrost.Block{
					SlotLeader: pool[0].ID,
					Epoch:      epoch,
				},
				nil,
			)

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot,
				epoch,
			).
			Return(false, nil)

		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)

		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PoolMissedABlock", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
		# HELP cardano_validator_watcher_missed_blocks_total number of missed blocks in the current epoch
		# TYPE cardano_validator_watcher_missed_blocks_total counter
		cardano_validator_watcher_missed_blocks_total{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 1
		`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_missed_blocks_total",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		// handle currentSlot - 1
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot-1,
				epoch,
			).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(
				mock.Anything,
				currentSlot-1,
			).
			Return(blockfrost.Block{}, errors.New("Not Found"))

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot,
				epoch,
			).
			Return(false, nil)

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PoolLostASlotBattleAndGenerateOrphanedBlock", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_orphaned_blocks_total number of orphaned blocks in the current epoch
# TYPE cardano_validator_watcher_orphaned_blocks_total counter
cardano_validator_watcher_orphaned_blocks_total{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 1
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_orphaned_blocks_total",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		// handle currentSlot - 1
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot-1,
				epoch,
			).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(
				mock.Anything,
				currentSlot-1,
			).
			Return(blockfrost.Block{SlotLeader: "bad-pool"}, nil)

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot,
				epoch,
			).
			Return(false, nil)

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)

		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PoolMissedConsecutiveBlocks", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_consecutive_missed_blocks number of consecutive missed blocks in a row
# TYPE cardano_validator_watcher_consecutive_missed_blocks gauge
cardano_validator_watcher_consecutive_missed_blocks{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 2
		`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_consecutive_missed_blocks",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			ticker := time.NewTimer(time.Second * 10)
			<-ticker.C
			cancel()
		}()

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		// handle currentSlot - 1
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot-1, epoch).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(mock.Anything, currentSlot-1).
			Return(blockfrost.Block{}, errors.New("Not Found"))

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot, epoch).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(mock.Anything, currentSlot).
			Return(blockfrost.Block{}, errors.New("Not Found"))

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_MissedConsecutiveBlocksThenProposedNextBlock", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_consecutive_missed_blocks number of consecutive missed blocks in a row
# TYPE cardano_validator_watcher_consecutive_missed_blocks gauge
cardano_validator_watcher_consecutive_missed_blocks{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 0
# HELP cardano_validator_watcher_validated_blocks_total number of validated blocks in the current epoch
# TYPE cardano_validator_watcher_validated_blocks_total counter
cardano_validator_watcher_validated_blocks_total{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 1
		`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_consecutive_missed_blocks",
			"cardano_validator_watcher_validated_blocks_total",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		// handle currentSlot +1 with a missed block
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot-1, epoch).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(mock.Anything, currentSlot-1).
			Return(blockfrost.Block{}, errors.New("Not Found"))

		// handle currentSlot with a valid block
		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(mock.Anything, pool[0].ID, currentSlot, epoch).
			Return(true, nil)

		clients.bf.EXPECT().
			GetBlockBySlot(
				mock.Anything,
				currentSlot,
			).
			Return(
				blockfrost.Block{
					SlotLeader: "pool-0",
					Epoch:      epoch,
				}, nil,
			)

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_EpochTransition", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_missed_blocks_total number of missed blocks in the current epoch
# TYPE cardano_validator_watcher_missed_blocks_total counter
cardano_validator_watcher_missed_blocks_total{epoch="101",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0
# HELP cardano_validator_watcher_orphaned_blocks_total number of orphaned blocks in the current epoch
# TYPE cardano_validator_watcher_orphaned_blocks_total counter
cardano_validator_watcher_orphaned_blocks_total{epoch="101",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0
# HELP cardano_validator_watcher_validated_blocks_total number of validated blocks in the current epoch
# TYPE cardano_validator_watcher_validated_blocks_total counter
cardano_validator_watcher_validated_blocks_total{epoch="101",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 0
`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_orphaned_blocks_total",
			"cardano_validator_watcher_missed_blocks_total",
			"cardano_validator_watcher_validated_blocks_total",
		}

		epoch := 100
		initialSlot := 99
		currentSlot := 100
		nextEpoch := 101
		nextEpochSlot := 102
		nextEpochHeight := 102

		ctx := setupContextWithTimeout(t, time.Second*30)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			).Times(1)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, nextEpochHeight, epoch).Return(5000, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: nextEpochHeight,
					Slot:   nextEpochSlot,
					Hash:   "hash",
					Epoch:  nextEpoch,
				},
				nil,
			)

		// handle currentSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot,
				epoch,
			).
			Return(false, nil)

		// handle currentSlot+1 which is the last slot for the epoch
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				epoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				currentSlot+1,
				epoch,
			).
			Return(false, nil)

		// Save state before transitioning to the next epoch
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot+1, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		// handle next epoch
		clients.bf.EXPECT().
			GetLatestEpoch(
				mock.Anything,
			).
			Return(blockfrost.Epoch{Epoch: nextEpoch}, nil)

		clients.sl.EXPECT().RefreshCurrent(
			mock.Anything,
			blockfrost.Epoch{Epoch: nextEpoch},
		).Return(nil)

		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(nextEpoch, currentSlot+1, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, nextEpochHeight, nextEpoch).Return(5000, nil)

		clients.bf.EXPECT().GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height:    nextEpochHeight,
					Slot:      nextEpochSlot,
					Hash:      "hash",
					Epoch:     nextEpoch,
					EpochSlot: 1,
				},
				nil,
			)

		// handle nextEpochSlot
		clients.sl.EXPECT().
			IsSlotsEmpty(
				mock.Anything,
				pool[0].ID,
				nextEpoch,
			).
			Return(false, nil)

		clients.sl.EXPECT().
			IsSlotLeader(
				mock.Anything,
				pool[0].ID,
				nextEpochSlot,
				nextEpoch,
			).
			Return(false, nil)

		// save state with the new epoch
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(nextEpoch, nextEpochSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Second * 15,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, nextEpochSlot)
		require.Equal(t, watcher.state.Epoch, nextEpoch)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.GatherAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_PoolWithNoLeaderSlotsDoesNotStopTheWatcher", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		epoch := 100
		initialSlot := 99

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(true, nil)

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, initialSlot)
	})

	t.Run("GoodPath_PoolWithoutLeaderSlotIsSkipped", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		epoch := 100
		initialSlot := 99
		currentSlot := 101
		currentHeight := 101

		pool := setupPool()

		ctx := setupContextWithTimeout(t, time.Second*20)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(true, nil).Times(1)

		// no leader slot left: next_slot_leader is exported as 0
		clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, currentSlot, epoch).Return(0, nil)

		clients.bf.EXPECT().
			GetLatestBlock(mock.Anything).
			Return(
				blockfrost.Block{
					Height: currentHeight,
					Slot:   currentSlot,
					Hash:   "hash",
					Epoch:  epoch,
				},
				nil,
			)

		// handle currentSlot - 1
		clients.sl.EXPECT().IsSlotsEmpty(
			mock.Anything,
			pool[0].ID,
			epoch,
		).Return(true, nil)

		// handle currentSlot
		clients.sl.EXPECT().IsSlotsEmpty(
			mock.Anything,
			pool[0].ID,
			epoch,
		).Return(true, nil)

		// save state
		mockDBClient.mock.
			ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
			WithArgs(epoch, currentSlot, AnyTime{}).
			WillReturnResult(sqlmock.NewResult(1, 1))

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(true)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, currentSlot)
	})

	t.Run("SadPath_BlockFrostIsNotReachable", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		expectNoReplay(clients)
		mockDBClient := setupDB(t)
		registry := setupRegistry(t)

		epoch := 100
		initialSlot := 99

		ctx := setupContextWithTimeout(t, time.Second*10)

		// Setup Mocks for Dependencies
		clients.bf.EXPECT().
			GetLatestEpoch(mock.Anything).
			Return(
				blockfrost.Epoch{Epoch: epoch},
				nil,
			)

		mockDBClient.mock.
			ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
			WillReturnRows(
				sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).
					AddRow(epoch, initialSlot, time.Now()),
			)

		clients.sl.EXPECT().
			IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).
			Return(false, nil)

		options := BlockWatcherOptions{
			RefreshInterval: time.Minute * 1,
		}
		healthStore := NewHealthStore()
		healthStore.SetHealth(false)
		watcher := NewBlockWatcher(
			clients.cardano,
			clients.bf,
			clients.sl,
			pool,
			registry.metrics,
			mockDBClient.db,
			healthStore,
			options,
		)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, watcher.state.Slot, initialSlot)
	})
}

func TestBlockWatcher_ReplayProcessedSlotsOnStart(t *testing.T) {
	t.Parallel()
	pool := setupPool()

	clients := setupClients(t)
	mockDBClient := setupDB(t)
	registry := setupRegistry(t)

	// the counters of the epoch are rebuilt from the leader slots already processed (<= 200),
	// in order: validated, missed, orphaned. Slot 250 is not processed yet and is ignored.
	registry.metricsExpectedOutput = `
# HELP cardano_validator_watcher_consecutive_missed_blocks number of consecutive missed blocks in a row
# TYPE cardano_validator_watcher_consecutive_missed_blocks gauge
cardano_validator_watcher_consecutive_missed_blocks{epoch="100",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
# HELP cardano_validator_watcher_missed_blocks_total number of missed blocks in the current epoch
# TYPE cardano_validator_watcher_missed_blocks_total counter
cardano_validator_watcher_missed_blocks_total{epoch="100",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
# HELP cardano_validator_watcher_orphaned_blocks_total number of orphaned blocks in the current epoch
# TYPE cardano_validator_watcher_orphaned_blocks_total counter
cardano_validator_watcher_orphaned_blocks_total{epoch="100",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
# HELP cardano_validator_watcher_validated_blocks_total number of validated blocks in the current epoch
# TYPE cardano_validator_watcher_validated_blocks_total counter
cardano_validator_watcher_validated_blocks_total{epoch="100",pool_id="pool-0",pool_instance="pool-0",pool_name="pool-0"} 1
`
	registry.metricsUnderTest = []string{
		"cardano_validator_watcher_consecutive_missed_blocks",
		"cardano_validator_watcher_missed_blocks_total",
		"cardano_validator_watcher_orphaned_blocks_total",
		"cardano_validator_watcher_validated_blocks_total",
	}

	epoch := 100
	slotInState := 200

	ctx := setupContextWithTimeout(t, time.Second*5)

	clients.bf.EXPECT().GetLatestEpoch(mock.Anything).Return(blockfrost.Epoch{Epoch: epoch}, nil)
	mockDBClient.mock.
		ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
		WillReturnRows(sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).AddRow(epoch, slotInState, time.Now()))

	clients.sl.EXPECT().
		GetSlotLeaders(mock.Anything, pool[0].ID, epoch).
		Return(slotleader.Schedule{Slots: []int{150, 180, 190, 250}}, nil)
	clients.bf.EXPECT().GetBlockBySlot(mock.Anything, 150).Return(blockfrost.Block{SlotLeader: pool[0].ID}, nil)
	clients.bf.EXPECT().GetBlockBySlot(mock.Anything, 180).Return(blockfrost.Block{}, errors.New("Not Found"))
	clients.bf.EXPECT().GetBlockBySlot(mock.Anything, 190).Return(blockfrost.Block{SlotLeader: "another-pool"}, nil)
	clients.sl.EXPECT().IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).Return(false, nil)

	healthStore := NewHealthStore()
	healthStore.SetHealth(false)
	watcher := NewBlockWatcher(clients.cardano, clients.bf, clients.sl, pool, registry.metrics, mockDBClient.db, healthStore,
		BlockWatcherOptions{RefreshInterval: time.Minute})

	err := watcher.Start(ctx)
	require.ErrorIs(t, err, context.Canceled)

	b := bytes.NewBufferString(registry.metricsExpectedOutput)
	require.NoError(t, testutil.GatherAndCompare(registry.registry, b, registry.metricsUnderTest...))
}

func TestBlockWatcher_FailedLeaderSlotIsRetried(t *testing.T) {
	t.Parallel()
	pool := setupPool()

	clients := setupClients(t)
	expectNoReplay(clients)
	mockDBClient := setupDB(t)
	registry := setupRegistry(t)

	epoch := 100
	slotInState := 99
	leaderSlot := 100

	ctx := setupContextWithTimeout(t, time.Second*5)

	clients.bf.EXPECT().GetLatestEpoch(mock.Anything).Return(blockfrost.Epoch{Epoch: epoch}, nil)
	mockDBClient.mock.
		ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").
		WillReturnRows(sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).AddRow(epoch, slotInState, time.Now()))
	clients.sl.EXPECT().IsSlotsEmpty(mock.Anything, pool[0].ID, epoch).Return(false, nil)
	clients.sl.EXPECT().GetNextSlotLeader(mock.Anything, pool[0].ID, leaderSlot, epoch).Return(0, nil)
	clients.bf.EXPECT().GetLatestBlock(mock.Anything).
		Return(blockfrost.Block{Height: leaderSlot, Slot: leaderSlot, Epoch: epoch, EpochSlot: 50}, nil)
	clients.sl.EXPECT().IsSlotLeader(mock.Anything, pool[0].ID, leaderSlot, epoch).Return(true, nil)
	clients.bf.EXPECT().GetBlockBySlot(mock.Anything, leaderSlot).Return(blockfrost.Block{}, errors.New("HTTP 500"))

	// the failed slot is not saved as processed: it is retried on the next tick
	mockDBClient.mock.
		ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").
		WithArgs(epoch, slotInState, AnyTime{}).
		WillReturnResult(sqlmock.NewResult(1, 1))

	healthStore := NewHealthStore()
	healthStore.SetHealth(true)
	watcher := NewBlockWatcher(clients.cardano, clients.bf, clients.sl, pool, registry.metrics, mockDBClient.db, healthStore,
		BlockWatcherOptions{RefreshInterval: time.Minute})

	err := watcher.Start(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, slotInState, watcher.state.Slot)
	require.InDelta(t, 0, testutil.ToFloat64(registry.metrics.MissedBlocks.WithLabelValues(pool[0].Name, pool[0].ID, pool[0].Instance, "100")), 0)
	require.InDelta(t, 0, testutil.ToFloat64(registry.metrics.ValidatedBlocks.WithLabelValues(pool[0].Name, pool[0].ID, pool[0].Instance, "100")), 0)
}

func TestBlockWatcher_EventLogs(t *testing.T) {
	t.Parallel()
	pool := setupPool()
	clients := setupClients(t)
	mockDBClient := setupDB(t)
	registry := setupRegistry(t)
	ctx := context.Background()

	watcher := NewBlockWatcher(clients.cardano, clients.bf, clients.sl, pool, registry.metrics, mockDBClient.db, NewHealthStore(),
		BlockWatcherOptions{RefreshInterval: time.Minute})
	var logs bytes.Buffer
	watcher.logger = slog.New(slog.NewTextHandler(&logs, nil))
	watcher.state.Epoch = 100
	watcher.initMetrics(nil)

	// the counters behind the logs follow the outcomes
	watcher.recordOutcome(ctx, 100, slotOutcome{pool: pool[0], slot: 10, kind: outcomeMissed})
	watcher.recordOutcome(ctx, 100, slotOutcome{pool: pool[0], slot: 11, kind: outcomeMissed})
	watcher.recordOutcome(ctx, 100, slotOutcome{pool: pool[0], slot: 12, kind: outcomeOrphaned,
		block: blockfrost.Block{SlotLeader: "pool-winner", Hash: "abc"}})
	watcher.recordOutcome(ctx, 100, slotOutcome{pool: pool[0], slot: 13, kind: outcomeValidated})
	require.Contains(t, logs.String(), "consecutive_missed=2")
	require.Contains(t, logs.String(), "lost the slot battle for slot 12 to pool pool-winner")
	require.Equal(t, epochCounts{validated: 1, missed: 2, orphaned: 1}, watcher.counts[pool[0].ID])

	// the epoch summary reports the schedule and the counters
	clients.sl.EXPECT().GetSlotLeaders(mock.Anything, pool[0].ID, 100).Return(slotleader.Schedule{Quantity: 5}, nil)
	watcher.logEpochSummary(ctx, 100)
	require.Contains(t, logs.String(), "Epoch 100 summary for pool pool-0: 5 scheduled, 1 validated, 2 missed, 1 orphaned")

	// the next leader slot is logged at INFO once per change
	logs.Reset()
	block := blockfrost.Block{Slot: 1000, Time: 1700000000}
	watcher.logNextLeaderSlot(ctx, pool[0], 1060, 60, block)
	watcher.logNextLeaderSlot(ctx, pool[0], 1060, 30, block)
	require.Equal(t, 1, strings.Count(logs.String(), "Next leader slot for pool pool-0: slot 1060 in 1m0s"))
	watcher.logNextLeaderSlot(ctx, pool[0], 0, 0, block)
	require.Contains(t, logs.String(), "has no more leader slot in this epoch")
}
