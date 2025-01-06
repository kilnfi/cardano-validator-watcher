package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	bf "github.com/blockfrost/blockfrost-go"
	"github.com/jmoiron/sqlx"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/kilnfi/cardano-validator-watcher/internal/slotleader"
)

// Maximum number of slots per epoch.
const maxSlotPerEpoch = 432000

// BlockWatcherOptions represents the options for the block watcher.
type BlockWatcherOptions struct {
	RefreshInterval time.Duration
}

// BlockWatcher represents a watcher for Cardano blocks.
type BlockWatcher struct {
	logger *slog.Logger

	state             *BlockWatcherState
	cardano           cardano.CardanoClient
	blockfrost        blockfrost.Client
	slotLeaderService slotleader.SlotLeader
	metrics           *metrics.Collection
	pools             pools.Pools
	poolStats         pools.PoolStats
	db                *sqlx.DB
	healthStore       *HealthStore
	opts              BlockWatcherOptions
}

var _ Watcher = (*BlockWatcher)(nil)

// NewBlockWatcher creates a new BlockWatcher instance.
func NewBlockWatcher(
	cardano cardano.CardanoClient,
	blockfrost blockfrost.Client,
	slotLeader slotleader.SlotLeader,
	pools pools.Pools,
	metrics *metrics.Collection,
	db *sqlx.DB,
	healthStore *HealthStore,
	opts BlockWatcherOptions,
) *BlockWatcher {
	logger := slog.With(
		slog.String("component", "block-watcher"),
	)

	state := NewBlockWatcherState(db, blockfrost)
	return &BlockWatcher{
		logger:            logger,
		cardano:           cardano,
		blockfrost:        blockfrost,
		slotLeaderService: slotLeader,
		metrics:           metrics,
		pools:             pools,
		poolStats:         pools.GetPoolStats(),
		state:             state,
		db:                db,
		healthStore:       healthStore,
		opts:              opts,
	}
}

// Start starts the block watcher.
// It returns an error if the watcher fails to start or if an error is encountered during the process.
func (w *BlockWatcher) Start(ctx context.Context) error {
	// Initialize the state
	if err := w.initState(ctx); err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	// Initialize metrics
	w.initMetrics()

	// Ensure that each pool has leader slots assigned
	if err := w.ensurePoolHasLeaderSlots(ctx); err != nil {
		var noSlotsFound *ErrNoSlotsAssignedToPool
		if errors.As(err, &noSlotsFound) {
			w.logger.Error(
				fmt.Sprintf("🚨 pool %s has no leader slots assigned. Is it a new pool ?", noSlotsFound.PoolID),
				slog.Int("epoch", noSlotsFound.Epoch),
			)
			return err
		}
	}

	ticker := time.NewTicker(w.opts.RefreshInterval)
	defer ticker.Stop()
	var previousHealthStatus bool

	for {
		currentHealthStatus := w.healthStore.GetHealth()
		w.handleHealthTransition(previousHealthStatus, currentHealthStatus)
		previousHealthStatus = currentHealthStatus

		if currentHealthStatus {
			err := w.start(ctx)
			if err != nil {
				var slotLeaderRefreshError *slotleader.ErrSlotLeaderRefresh
				if errors.As(err, &slotLeaderRefreshError) {
					return err
				}
				w.logger.Error("watcher started but failed with the following error", slog.String("error", err.Error()))
			}
		}

		select {
		case <-ctx.Done():
			w.logger.Info("stopping watcher")
			return fmt.Errorf("context done in watcher: %w", ctx.Err())
		case <-ticker.C:
			continue
		}
	}
}

// handleHealthTransition handles the transition of the block watcher's health status.
// It compares the previous and current health states, and logs a warning if the block watcher
// is not ready, or an info message if it is ready.
func (w *BlockWatcher) handleHealthTransition(previous bool, current bool) {
	if previous != current {
		if !w.healthStore.GetHealth() {
			w.logger.Warn(
				"💔 block watcher is not ready.",
			)
		} else {
			w.logger.Info("💚 block watcher is ready")
		}
	}
}

// initState initializes the state of the block watcher.
// It returns an error if the state initialization fails.
func (w *BlockWatcher) initState(ctx context.Context) error {
	epoch, err := w.blockfrost.GetLatestEpoch(ctx)
	if err != nil {
		return fmt.Errorf("failed to get the latest epoch: %w", err)
	}

	// Reconcile and load the state
	w.logger.Info("Starting to load and reconcile state", slog.Int("epoch", epoch.Epoch))
	if err := w.state.LoadAndReconcile(ctx, epoch.Epoch); err != nil {
		return fmt.Errorf("failed to reconcile and load the state: %w", err)
	}
	w.logger.Info("State loaded and reconciled successfully", slog.Int("epoch", epoch.Epoch))

	return nil
}

// start starts the block watcher.
func (w *BlockWatcher) start(ctx context.Context) error {
	var epochTransition bool

	block, err := w.blockfrost.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest block: %w", err)
	}

	// fetch and display next slot leaders.
	if err := w.fetchAndLogNextSlotLeaders(ctx, block); err != nil {
		return fmt.Errorf("failed to fetch and log next slot leaders: %w", err)
	}

	// Start the watcher and detect epoch transition
	epochTransition, err = w.startWatcherAndDetectEpochTransition(ctx, block)
	if err != nil {
		return err
	}

	// We have detected a new epoch and we want to update the state to the new epoch
	// and refresh the slot leader schedule for each pools
	if epochTransition {
		if err := w.handleEpochtransition(ctx); err != nil {
			return err
		}
		// Reset the metrics for the new epoch
		w.initMetrics()
	}
	return nil
}

// ensurePoolHasLeaderSlots ensures that each pool has leader slots assigned.
// It returns an error if a pool has no leader slots assigned.
func (w *BlockWatcher) ensurePoolHasLeaderSlots(ctx context.Context) error {
	for _, pool := range w.pools.GetActivePools() {
		if pool.AllowEmptySlots {
			continue
		}
		empty, err := w.slotLeaderService.IsSlotsEmpty(ctx, pool.ID, w.state.Epoch)
		if err != nil {
			w.logger.Error("failed to check if pool has leader slots", slog.String("pool_id", pool.ID), slog.String("error", err.Error()))
			continue
		}
		if empty {
			return &ErrNoSlotsAssignedToPool{PoolID: pool.ID, Epoch: w.state.Epoch}
		}
	}
	return nil
}

// handleEpochtransition handles the epoch transition.
// It updates the state to the new epoch and refreshes the slot leader schedule for each pool.
// It returns an error if the epoch transition fails.
func (w *BlockWatcher) handleEpochtransition(ctx context.Context) error {
	epoch, err := w.blockfrost.GetLatestEpoch(ctx)
	if err != nil {
		return fmt.Errorf("handleEpochTransition: failed to get latest epoch: %w", err)
	}

	if err := w.state.Save(ctx, epoch.Epoch, w.state.Slot); err != nil {
		return fmt.Errorf("handleEpochTransition: failed to save state after epoch transition: %w", err)
	}

	// Update the slot leader schedule for each pool
	w.logger.Info("🔄 Refreshing slot leader schedule for the new epoch", slog.Int("epoch", epoch.Epoch))
	if err := w.slotLeaderService.Refresh(ctx, epoch); err != nil {
		return fmt.Errorf("handleEpochTransition: failed to refresh slot leader schedule for the new epoch: %w", err)
	}

	// Ensure that each pool has leader slots assigned
	if err := w.ensurePoolHasLeaderSlots(ctx); err != nil {
		var noSlotsFound *ErrNoSlotsAssignedToPool
		if errors.As(err, &noSlotsFound) {
			w.logger.Error(
				fmt.Sprintf("🚨 pool %s has no leader slots assigned. Is it a new pool ?", noSlotsFound.PoolID),
				slog.Int("epoch", noSlotsFound.Epoch),
			)
			return err
		}
	}

	return nil
}

// startWatcherAndDetectEpochTransition starts the watcher and detects epoch transition.
// It returns true if an epoch transition is detected, otherwise false.
func (w *BlockWatcher) startWatcherAndDetectEpochTransition(ctx context.Context, block bf.Block) (bool, error) {
	var startSlot, endSlot int
	var epochTransition bool

	startSlot = w.state.Slot + 1
	endSlot = block.Slot

	// Log the start and end slots for debugging purposes
	w.logger.Debug("start slot", slog.Int("slot", startSlot))
	w.logger.Debug("end slot", slog.Int("slot", endSlot))

	// Detecting epoch transition:
	// We compare the epoch of the latest received block with the epoch stored in the state.
	// If they differ, we've transitioned to a new epoch.
	// In this case, we handle all remaining slots from the previous epoch, even if they are empty.
	epochTransition = block.Epoch > w.state.Epoch
	if epochTransition {
		w.logger.Info("🚀 A new epoch has started.", slog.Int("epoch", block.Epoch))
		// Each epoch has a maximum of 432000 slots. However, due to reasons like empty or missed slots,
		// an epoch might end with fewer slots. To ensure no slot is missed, we process all slots from the previous epoch.
		//
		// The last slot of the current epoch is calculated by adding the number of remaining slots in the previous epoch
		// to the slot number of the last received block in the previous epoch.
		lastBlockInPreviousEpoch, err := w.blockfrost.GetLastBlockFromPreviousEpoch(ctx, w.state.Epoch)
		if err != nil {
			return epochTransition, fmt.Errorf("startWatcherAndDetectEpochTransition: failed to get last block from previous epoch: %w", err)
		}
		remainingSlots := maxSlotPerEpoch - lastBlockInPreviousEpoch.EpochSlot
		endSlot = lastBlockInPreviousEpoch.Slot + remainingSlots
	}

	if startSlot <= endSlot {
		latestSlotProcessed, err := w.processSlots(ctx, w.state.Epoch, startSlot, endSlot)

		defer func() {
			saveErr := w.state.Save(ctx, w.state.Epoch, latestSlotProcessed)
			if saveErr != nil {
				w.logger.Error("failed to save block watcher state", slog.Int("slot", latestSlotProcessed), slog.String("error", saveErr.Error()))
			}
		}()

		if err != nil {
			return epochTransition, fmt.Errorf("startWatcherAndDetectEpochTransition: failed to process all slots up to slot ID %d, stopped at slot ID %d: %w", endSlot, latestSlotProcessed, err)
		}

		w.metrics.LatestSlotProcessedByBlockWatcher.Set(float64(latestSlotProcessed))
	}

	return epochTransition, nil
}

// processSlots processes the slots and checks if the pool is a leader.
func (w *BlockWatcher) processSlots(ctx context.Context, epoch int, startSlot int, endSlot int) (int, error) {
	for slot := startSlot; slot <= endSlot; slot++ {
		w.logger.Info(
			fmt.Sprintf(
				"🔍 Processing slot %d - 🔑 Monitoring %d pools (%d active, %d excluded)",
				slot,
				w.poolStats.Total,
				w.poolStats.Active,
				w.poolStats.Excluded,
			),
			slog.Int("epoch", epoch),
		)

		for _, pool := range w.pools.GetActivePools() {
			err := w.processPoolSlot(ctx, epoch, pool, slot)
			if err != nil {
				return slot, err
			}
		}
	}
	return endSlot, nil
}

// processSlot processes the slot for the given pool.
// It returns an error if the slot processing fails.
func (w *BlockWatcher) processPoolSlot(ctx context.Context, epoch int, pool pools.Pool, slot int) error {
	// check if pool has slots assigned.
	empty, err := w.slotLeaderService.IsSlotsEmpty(ctx, pool.ID, epoch)
	if err != nil {
		return fmt.Errorf("processPoolSlot: failed to check if pool has leader slots: %w", err)
	}
	if empty && pool.AllowEmptySlots {
		w.logger.Info(
			fmt.Sprintf("🦄 pool %s has no leader slots assigned but allow empty slots is enabled", pool.Name),
			slog.Int("epoch", epoch),
			slog.String("pool_id", pool.ID),
		)
		return nil
	}

	// check if we are leader on the slot and handle it.
	isLeader, err := w.slotLeaderService.IsSlotLeader(ctx, pool.ID, slot, epoch)
	if err != nil {
		return fmt.Errorf("processPoolSlot: failed to check if pool is leader for slot: %w", err)
	}
	if isLeader {
		if err := w.processLeaderSlot(ctx, epoch, pool, slot); err != nil {
			return err
		}
		return nil
	}

	// We are not leader for the slot
	w.logger.Info(
		fmt.Sprintf("😴 pool %s is not a leader for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
	)
	return nil
}

// handleSlotLeader handles the slot leader and check the state of the slot.
// - Leader but slot not found on-chain: Missed block
// - Leader and slot found on-chain: Validated block
// - Leader, slot found on-chain but not proposed by us: Orphaned block (Slot battle lost)
// - Not leader: No action (Sleeping pool)
func (w *BlockWatcher) processLeaderSlot(ctx context.Context, epoch int, pool pools.Pool, slot int) error {
	w.logger.Info(
		fmt.Sprintf("👑 Pool %s leads slot %d", pool.Name, slot),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
	)

	block, err := w.blockfrost.GetBlockBySlot(ctx, slot)
	switch {
	case err != nil:
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			w.logMissedBlock(pool, slot, epoch)
		} else {
			return fmt.Errorf("processLeaderSlot: failed to fetch block on slot %d: %w", slot, err)
		}
	case block.SlotLeader == pool.ID:
		w.logValidatedBlock(pool, slot, epoch, block)
	default:
		w.logOrphanedBlock(pool, slot, epoch, block)
	}
	return nil
}

// logMissedBlock records a missed block in metrics and logs the occurrence.
func (w *BlockWatcher) logMissedBlock(pool pools.Pool, slot, epoch int) {
	w.logger.Info(fmt.Sprintf("❌ Pool %s missed block for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch), slog.String("pool_id", pool.ID))

	w.metrics.MissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
	w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
}

func (w *BlockWatcher) logValidatedBlock(pool pools.Pool, slot, epoch int, block bf.Block) {
	w.logger.Info(
		fmt.Sprintf("✅ Pool %s proposed block for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
		slog.Int("block_num", block.Height),
		slog.Int("epoch_slot", block.EpochSlot),
	)

	w.metrics.ValidatedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
	w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Set(0)
}

func (w *BlockWatcher) logOrphanedBlock(pool pools.Pool, slot, epoch int, block bf.Block) {
	w.logger.Info(
		fmt.Sprintf("🥊 Pool %s lost the battle for slot %d",
			pool.Name,
			slot,
		),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
		slog.Int("block_num", block.Height),
		slog.Int("epoch_slot", block.EpochSlot),
	)
	w.metrics.OrphanedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
}

// fetchAndLogNextSlotLeaders fetches and displays the next slot leaders for each pool.
// and expose the next slot leader as a metric.
func (w *BlockWatcher) fetchAndLogNextSlotLeaders(ctx context.Context, block bf.Block) error {
	for _, pool := range w.pools.GetActivePools() {
		if pool.AllowEmptySlots {
			continue
		}
		var remainingSlots int
		nextSlot, err := w.slotLeaderService.GetNextSlotLeader(ctx, pool.ID, block.Slot, w.state.Epoch)
		if err != nil {
			return fmt.Errorf("failed to get next slot leader: %w", err)
		}

		// If the next slot is 0 and no error that's mean we are at the end of the epoch
		// and the pool has no more slots.
		if nextSlot == 0 {
			remainingSlots = 0
		} else {
			remainingSlots = nextSlot - block.Slot
		}

		w.logger.Info(
			fmt.Sprintf("🕰  Next slot leader for pool %s is slot %d, remaining slots: %d", pool.Name, nextSlot, remainingSlots),
			slog.String("pool_id", pool.ID),
			slog.Int("epoch", w.state.Epoch),
		)
		w.metrics.NextSlotLeader.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Set(float64(nextSlot))
	}
	return nil
}

// initMetrics initializes the metrics for the block watcher.
func (w *BlockWatcher) initMetrics() {
	w.metrics.MissedBlocks.Reset()
	w.metrics.ValidatedBlocks.Reset()
	w.metrics.OrphanedBlocks.Reset()

	for _, pool := range w.pools.GetActivePools() {
		w.metrics.MissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Add(0)
		w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Add(0)
		w.metrics.ValidatedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Add(0)
		w.metrics.OrphanedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Add(0)
	}
}
