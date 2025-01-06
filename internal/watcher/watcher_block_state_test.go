package watcher

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/blockfrost/blockfrost-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type AnyTime struct{}

// Match satisfies sqlmock.Argument interface
func (a AnyTime) Match(v driver.Value) bool {
	_, ok := v.(time.Time)
	return ok
}

func TestBlockWatcherState_Save(t *testing.T) {
	t.Parallel()
	t.Run("GoodPath_StateIsUpdatedCorrectly", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		mockDBClient := setupDB(t)
		state := NewBlockWatcherState(mockDBClient.db, clients.bf)
		state.Epoch = 100
		state.Slot = 100
		mockDBClient.mock.ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").WithArgs(state.Epoch, state.Slot, AnyTime{}).WillReturnResult(sqlmock.NewResult(1, 1))

		err := state.Save(context.Background(), state.Epoch, state.Slot)
		require.NoError(t, err)
	})
}

func TestBlockWatcherState_Load(t *testing.T) {
	t.Parallel()
	t.Run("GoodPath_StateIsLoadedCorrectly", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		mockDBClient := setupDB(t)
		state := NewBlockWatcherState(mockDBClient.db, clients.bf)
		expectedDate := time.Now()

		mockDBClient.mock.ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").WillReturnRows(sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).AddRow(100, 100, expectedDate))

		err := state.Load(context.Background())
		require.NoError(t, err)
		require.Equal(t, 100, state.Epoch)
		require.Equal(t, 100, state.Slot)
		require.Equal(t, expectedDate, state.LastUpdate)
	})
}

func TestBlockWatcherState_LoadAndReconcile(t *testing.T) {
	t.Parallel()
	t.Run("GoodPath_StateIsLoadedAndReconciledCorrectly", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		mockDBClient := setupDB(t)
		state := NewBlockWatcherState(mockDBClient.db, clients.bf)
		state.Epoch = 100
		state.Slot = 100
		expectedDate := time.Now()

		mockDBClient.mock.ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").WillReturnRows(sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).AddRow(100, 100, expectedDate))

		err := state.LoadAndReconcile(context.Background(), 100)
		require.NoError(t, err)
		require.Equal(t, 100, state.Epoch)
		require.Equal(t, 100, state.Slot)
		require.Equal(t, expectedDate, state.LastUpdate)
	})

	t.Run("GoodPath_StateIsReconciledCorrectlyWithNewEpoch", func(t *testing.T) {
		t.Parallel()
		clients := setupClients(t)
		mockDBClient := setupDB(t)
		state := NewBlockWatcherState(mockDBClient.db, clients.bf)
		state.Epoch = 100
		state.Slot = 100

		expectedEpoch := 101
		expectedSlot := 101
		expectedDate := time.Date(2024, time.October, 29, 18, 29, 26, 0, time.UTC)

		// mocks
		mockDBClient.mock.ExpectQuery("SELECT epoch, slot, last_update FROM block_watcher_state LIMIT 1").WillReturnRows(sqlmock.NewRows([]string{"epoch", "slot", "last_update"}).AddRow(state.Epoch, state.Slot, expectedDate))
		clients.bf.EXPECT().GetFirstBlockInEpoch(mock.Anything, expectedEpoch).Return(blockfrost.Block{Epoch: expectedEpoch, Slot: expectedSlot}, nil)
		mockDBClient.mock.ExpectExec("INSERT OR REPLACE INTO block_watcher_state (id, epoch, slot, last_update) VALUES (1, ?, ?, ?)").WithArgs(expectedEpoch, expectedSlot, AnyTime{}).WillReturnResult(sqlmock.NewResult(1, 1))

		err := state.LoadAndReconcile(context.Background(), expectedEpoch)
		require.NoError(t, err)
		require.Equal(t, expectedEpoch, state.Epoch)
		require.Equal(t, expectedSlot, state.Slot)
		require.NotEmpty(t, state.LastUpdate)
	})
}
