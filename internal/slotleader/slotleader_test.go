package slotleader

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blockfrost/blockfrost-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestRefresh(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_RefreshSlotLeaders", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = `
		# HELP cardano_validator_watcher_expected_blocks number of expected blocks in the current epoch
		# TYPE cardano_validator_watcher_expected_blocks gauge
		cardano_validator_watcher_expected_blocks{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 2
		`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{
					Epoch: epoch,
					Nonce: "nonce",
				},
				nil,
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}),
			)

		clients.cardano.EXPECT().LeaderLogs(mock.Anything, "current", "nonce", pools[0]).Return(nil)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.NoError(t, err)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("GoodPath_Refresh_SlotsAlreadyRefreshed", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = `
		# HELP cardano_validator_watcher_expected_blocks number of expected blocks in the current epoch
		# TYPE cardano_validator_watcher_expected_blocks gauge
		cardano_validator_watcher_expected_blocks{epoch="100", pool_id="pool-0", pool_instance="pool-0", pool_name="pool-0"} 2
		`
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{
					Epoch: epoch,
					Nonce: "nonce",
				},
				nil,
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.NoError(t, err)

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_Refresh_UnableToCheckIfSlotsAreRefreshed", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = ``
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{
					Epoch: epoch,
					Nonce: "nonce",
				},
				nil,
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).WillReturnError(errors.New("timeout"))

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.ErrorContains(t, err, "unable to check if slots are already refreshed for pool")

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_Refresh_UnableToGetSlotLeaders", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = ``
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{
					Epoch: epoch,
					Nonce: "nonce",
				},
				nil,
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(errors.New("timeout"))

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.ErrorContains(t, err, "unable to get slot leaders for pool")

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_Refresh_UnableToGetEpochParameters", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = ``
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{},
				errors.New("timeout"),
			)

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.ErrorContains(t, err, "unable to get epoch parameters")

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_Refresh_UnableToCalculateLeaderLogs", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		registry.metricsExpectedOutput = ``
		registry.metricsUnderTest = []string{
			"cardano_validator_watcher_expected_blocks",
		}

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		clients.bf.EXPECT().
			GetEpochParameters(mock.Anything, epoch).
			Return(
				blockfrost.EpochParameters{
					Epoch: epoch,
					Nonce: "nonce",
				},
				nil,
			)

		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}),
			)

		clients.cardano.EXPECT().LeaderLogs(mock.Anything, "current", "nonce", pools[0]).Return(errors.New("cardano timeout"))

		err := slotLeaderService.Refresh(context.Background(), blockfrost.Epoch{Epoch: epoch})
		require.ErrorContains(t, err, "unable to refresh slot leaders for pool")

		b := bytes.NewBufferString(registry.metricsExpectedOutput)
		err = testutil.CollectAndCompare(registry.registry, b, registry.metricsUnderTest...)
		require.NoError(t, err)
	})
}

func TestGetNextSlotLeader(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_GetNextSlotLeader", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		slot, err := slotLeaderService.GetNextSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.NoError(t, err)
		require.Equal(t, 1000, slot)
	})

	t.Run("SadPath_GetNextSlotLeader_UnableToGetSlotLeaders", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(errors.New("timeout"))

		slot, err := slotLeaderService.GetNextSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.ErrorContains(t, err, "unable to get slot leaders for pool")
		require.Equal(t, 0, slot)
	})

	t.Run("SadPath_GetNextSlotLeader_NoSlotLeadersFound", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}),
			)

		slot, err := slotLeaderService.GetNextSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.ErrorContains(t, err, "no slot leaders found for pool")
		require.Equal(t, 0, slot)
	})

	t.Run("GoodPath_GetNextSlotLeader_NoNextSlots", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 5000

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)

		slot, err := slotLeaderService.GetNextSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.NoError(t, err)
		require.Equal(t, 0, slot)
	})
}

func TestIsRefresh(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_IsRefresh", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"id", "epoch", "pool_id", "slot_qty", "slots", "hash"}).AddRow(
					1,
					epoch,
					pools[0].ID,
					2,
					"[1000, 2000]",
					"hash",
				),
			)
		isRefresh, err := slotLeaderService.isRefresh(context.Background(), pools[0].ID, epoch)
		require.NoError(t, err)
		require.True(t, isRefresh)
	})

	t.Run("SadPath_IsRefresh_UnableToFetchSlots", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT * FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(errors.New("timeout"))

		isRefresh, err := slotLeaderService.isRefresh(context.Background(), pools[0].ID, epoch)
		require.ErrorContains(t, err, "unable to check if slots are already refreshed for pool")
		require.False(t, isRefresh)
	})
}

func TestIsSlotLeader(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_IsSlotLeader_IsLeader", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 1000

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"slots"}).AddRow(
					"[1000, 2000]",
				),
			)
		isLeader, err := slotLeaderService.IsSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.NoError(t, err)
		require.True(t, isLeader)
	})

	t.Run("GoodPath_IsSlotLeader_NotLeader", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 1001

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"slots"}).AddRow(
					"[1000, 2000]",
				),
			)
		isLeader, err := slotLeaderService.IsSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.NoError(t, err)
		require.False(t, isLeader)
	})

	t.Run("SadPath_IsSlotLeader_UnableToGetSlotLeaders", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(errors.New("timeout"))

		isLeader, err := slotLeaderService.IsSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.ErrorContains(t, err, fmt.Sprintf("unable to check if slot %d is a leader for pool %s", height, pools[0].ID))
		require.False(t, isLeader)
	})

	t.Run("SadPath_IsSlotLeader_NoSlotLeadersFound", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100
		height := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(sql.ErrNoRows)
		isLeader, err := slotLeaderService.IsSlotLeader(context.Background(), pools[0].ID, height, epoch)
		require.NoError(t, err)
		require.False(t, isLeader)
	})
}

func TestIsSlotEmpty(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_IsSlotEmpty_Empty", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots, slot_qty FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"slots", "slot_qty"}).AddRow(
					"[]",
					0,
				),
			)

		isEmpty, err := slotLeaderService.IsSlotsEmpty(context.Background(), pools[0].ID, epoch)
		require.NoError(t, err)
		require.True(t, isEmpty)
	})

	t.Run("GoodPath_IsSlotEmpty_NotEmpty", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots, slot_qty FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnRows(
				sqlmock.NewRows([]string{"slots", "slot_qty"}).AddRow(
					"[1000, 2000]",
					2,
				),
			)

		isEmpty, err := slotLeaderService.IsSlotsEmpty(context.Background(), pools[0].ID, epoch)
		require.NoError(t, err)
		require.False(t, isEmpty)
	})

	t.Run("SadPath_IsSlotEmpty_UnableToGetSlotLeaders", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		pools := setupPools(t)
		db := setupDB(t)
		registry := setupRegistry(t)
		epoch := 100

		slotLeaderService := NewSlotLeaderService(
			db.db,
			clients.cardano,
			clients.bf, pools,
			registry.metrics,
		)

		// setup mocks
		db.mock.ExpectQuery("SELECT slots, slot_qty FROM slots WHERE pool_id = ? AND epoch = ?").
			WithArgs(pools[0].ID, epoch).
			WillReturnError(errors.New("timeout"))

		isEmpty, err := slotLeaderService.IsSlotsEmpty(context.Background(), pools[0].ID, epoch)
		require.ErrorContains(t, err, "unable to check if slots are empty")
		require.False(t, isEmpty)
	})
}
