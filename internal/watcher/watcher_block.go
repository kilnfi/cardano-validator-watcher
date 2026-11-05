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

// outcomeKind is the result of a slot the pool was leader on.
type outcomeKind int

const (
	outcomeValidated outcomeKind = iota
	outcomeMissed
	outcomeOrphaned
)

// slotOutcome is the evaluated result of a leader slot, recorded once the whole slot is processed.
type slotOutcome struct {
	pool  pools.Pool
	slot  int
	kind  outcomeKind
	block bf.Block
}

// epochCounts holds the block outcomes of a pool for the current epoch.
type epochCounts struct {
	validated         int
	missed            int
	orphaned          int
	consecutiveMissed int
}

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

	// Rebuild the counters of the current epoch before creating them: Prometheus must not
	// see them go back to zero on a restart, or increase() based alerts would fire.
	w.initMetrics(w.replayProcessedSlots(ctx))

	w.logPoolsWithoutLeaderSlots(ctx)

	ticker := time.NewTicker(w.opts.RefreshInterval)
	defer ticker.Stop()
	var previousHealthStatus bool

	for {
		currentHealthStatus := w.healthStore.GetHealth()
		w.handleHealthTransition(ctx, previousHealthStatus, currentHealthStatus)
		previousHealthStatus = currentHealthStatus

		if currentHealthStatus {
			err := w.start(ctx)
			if err == nil {
				w.metrics.LastSuccess.WithLabelValues("block-watcher").SetToCurrentTime()
			}
			if err != nil {
				var slotLeaderRefreshError *slotleader.ErrSlotLeaderRefresh
				if errors.As(err, &slotLeaderRefreshError) {
					return err
				}
				w.logger.ErrorContext(ctx, "watcher started but failed with the following error", slog.String("error", err.Error()))
			}
		}

		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "stopping watcher")
			return fmt.Errorf("context done in watcher: %w", ctx.Err())
		case <-ticker.C:
			continue
		}
	}
}

// handleHealthTransition handles the transition of the block watcher's health status.
// It compares the previous and current health states, and logs a warning if the block watcher
// is not ready, or an info message if it is ready.
func (w *BlockWatcher) handleHealthTransition(ctx context.Context, previous bool, current bool) {
	if previous != current {
		if !w.healthStore.GetHealth() {
			w.logger.WarnContext(ctx,
				"💔 block watcher is not ready.",
			)
		} else {
			w.logger.InfoContext(ctx, "💚 block watcher is ready")
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
	w.logger.InfoContext(ctx, "Starting to load and reconcile state", slog.Int("epoch", epoch.Epoch))
	if err := w.state.LoadAndReconcile(ctx, epoch.Epoch); err != nil {
		return fmt.Errorf("failed to reconcile and load the state: %w", err)
	}
	w.logger.InfoContext(ctx, "State loaded and reconciled successfully", slog.Int("epoch", epoch.Epoch))

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
		w.initMetrics(nil)
	}
	return nil
}

// logPoolsWithoutLeaderSlots logs the pools without any leader slot in the current epoch.
// This is expected for pools with little stake, so it is not an error: expected_blocks is
// exported as 0 and pool_no_leader_slot_probability tells whether it is abnormal.
func (w *BlockWatcher) logPoolsWithoutLeaderSlots(ctx context.Context) {
	for _, pool := range w.pools.GetActivePools() {
		empty, err := w.slotLeaderService.IsSlotsEmpty(ctx, pool.ID, w.state.Epoch)
		if err != nil {
			w.logger.ErrorContext(ctx, "failed to check if pool has leader slots", slog.String("pool_id", pool.ID), slog.String("error", err.Error()))
			continue
		}
		if empty {
			w.logger.InfoContext(ctx,
				fmt.Sprintf("pool %s has no leader slot assigned in this epoch", pool.Name),
				slog.String("pool_id", pool.ID),
				slog.Int("epoch", w.state.Epoch),
			)
		}
	}
}

// handleEpochtransition handles the epoch transition.
// It updates the state to the new epoch and refreshes the slot leader schedule for each pool.
// It returns an error if the epoch transition fails.
func (w *BlockWatcher) handleEpochtransition(ctx context.Context) error {
	epoch, err := w.blockfrost.GetLatestEpoch(ctx)
	if err != nil {
		return fmt.Errorf("handleEpochTransition: failed to get latest epoch: %w", err)
	}

	// Refresh the schedule before saving the new epoch: if it fails, the state stays on the
	// previous epoch so the transition (and the refresh) is retried on the next tick.
	w.logger.InfoContext(ctx, "🔄 Refreshing slot leader schedule for the new epoch", slog.Int("epoch", epoch.Epoch))
	if err := w.slotLeaderService.RefreshCurrent(ctx, epoch); err != nil {
		return fmt.Errorf("handleEpochTransition: failed to refresh slot leader schedule for the new epoch: %w", err)
	}

	previousEpoch := w.state.Epoch
	if err := w.state.Save(ctx, epoch.Epoch, w.state.Slot); err != nil {
		return fmt.Errorf("handleEpochTransition: failed to save state after epoch transition: %w", err)
	}

	// Stop exporting the gauges of the epoch that just ended
	w.metrics.DeleteEpochSeries(strconv.Itoa(previousEpoch))

	w.logPoolsWithoutLeaderSlots(ctx)

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
	w.logger.DebugContext(ctx, "start slot", slog.Int("slot", startSlot))
	w.logger.DebugContext(ctx, "end slot", slog.Int("slot", endSlot))

	// Detecting epoch transition:
	// We compare the epoch of the latest received block with the epoch stored in the state.
	// If they differ, we've transitioned to a new epoch.
	// In this case, we handle all remaining slots from the previous epoch, even if they are empty.
	epochTransition = block.Epoch > w.state.Epoch
	if epochTransition {
		w.logger.InfoContext(ctx, "🚀 A new epoch has started.", slog.Int("epoch", block.Epoch))
		// Process every remaining slot of the previous epoch, even empty ones: it ends right
		// before the first slot of the new epoch, which is derived from the latest block.
		endSlot = block.Slot - block.EpochSlot - 1
	}

	if startSlot <= endSlot {
		latestSlotProcessed, err := w.processSlots(ctx, w.state.Epoch, startSlot, endSlot)

		defer func() {
			saveErr := w.state.Save(ctx, w.state.Epoch, latestSlotProcessed)
			if saveErr != nil {
				w.logger.ErrorContext(ctx, "failed to save block watcher state", slog.Int("slot", latestSlotProcessed), slog.String("error", saveErr.Error()))
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
		w.logger.DebugContext(ctx,
			fmt.Sprintf(
				"🔍 Processing slot %d - 🔑 Monitoring %d pools (%d active, %d excluded)",
				slot,
				w.poolStats.Total,
				w.poolStats.Active,
				w.poolStats.Excluded,
			),
			slog.Int("epoch", epoch),
		)

		// A slot is recorded only once every pool has been evaluated: on error it is retried
		// on the next tick instead of being skipped, and pools already evaluated are not counted twice.
		var outcomes []slotOutcome
		for _, pool := range w.pools.GetActivePools() {
			outcome, isLeader, err := w.processPoolSlot(ctx, epoch, pool, slot)
			if err != nil {
				return slot - 1, err
			}
			if isLeader {
				outcomes = append(outcomes, outcome)
			}
		}
		for _, outcome := range outcomes {
			w.recordOutcome(ctx, epoch, outcome)
		}
	}
	return endSlot, nil
}

// processPoolSlot evaluates the slot for the given pool. It returns the outcome and true
// when the pool was leader on the slot, without recording it.
func (w *BlockWatcher) processPoolSlot(ctx context.Context, epoch int, pool pools.Pool, slot int) (slotOutcome, bool, error) {
	// check if pool has slots assigned.
	empty, err := w.slotLeaderService.IsSlotsEmpty(ctx, pool.ID, epoch)
	if err != nil {
		return slotOutcome{}, false, fmt.Errorf("processPoolSlot: failed to check if pool has leader slots: %w", err)
	}
	if empty {
		w.logger.DebugContext(ctx,
			fmt.Sprintf("pool %s has no leader slot assigned in this epoch", pool.Name),
			slog.Int("epoch", epoch),
			slog.String("pool_id", pool.ID),
		)
		return slotOutcome{}, false, nil
	}

	// check if we are leader on the slot and handle it.
	isLeader, err := w.slotLeaderService.IsSlotLeader(ctx, pool.ID, slot, epoch)
	if err != nil {
		return slotOutcome{}, false, fmt.Errorf("processPoolSlot: failed to check if pool is leader for slot: %w", err)
	}
	if isLeader {
		w.logger.InfoContext(ctx,
			fmt.Sprintf("👑 Pool %s leads slot %d", pool.Name, slot),
			slog.Int("epoch", epoch),
			slog.String("pool_id", pool.ID),
		)
		outcome, err := w.classifyLeaderSlot(ctx, pool, slot)
		if err != nil {
			return slotOutcome{}, false, fmt.Errorf("processPoolSlot: %w", err)
		}
		return outcome, true, nil
	}

	// We are not leader for the slot
	w.logger.DebugContext(ctx,
		fmt.Sprintf("😴 pool %s is not a leader for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
	)
	return slotOutcome{}, false, nil
}

// classifyLeaderSlot checks the state of a slot the pool was leader on.
// - Leader but slot not found on-chain: Missed block
// - Leader and slot found on-chain: Validated block
// - Leader, slot found on-chain but not proposed by us: Orphaned block (Slot battle lost)
func (w *BlockWatcher) classifyLeaderSlot(ctx context.Context, pool pools.Pool, slot int) (slotOutcome, error) {
	block, err := w.blockfrost.GetBlockBySlot(ctx, slot)
	switch {
	case err != nil:
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return slotOutcome{pool: pool, slot: slot, kind: outcomeMissed}, nil
		}
		return slotOutcome{}, fmt.Errorf("failed to fetch block on slot %d: %w", slot, err)
	case block.SlotLeader == pool.ID:
		return slotOutcome{pool: pool, slot: slot, kind: outcomeValidated, block: block}, nil
	default:
		return slotOutcome{pool: pool, slot: slot, kind: outcomeOrphaned, block: block}, nil
	}
}

// recordOutcome updates the metrics and logs for a leader slot outcome.
func (w *BlockWatcher) recordOutcome(ctx context.Context, epoch int, outcome slotOutcome) {
	switch outcome.kind {
	case outcomeMissed:
		w.logMissedBlock(ctx, outcome.pool, outcome.slot, epoch)
	case outcomeValidated:
		w.logValidatedBlock(ctx, outcome.pool, outcome.slot, epoch, outcome.block)
	case outcomeOrphaned:
		w.logOrphanedBlock(ctx, outcome.pool, outcome.slot, epoch, outcome.block)
	}
}

// replayProcessedSlots rebuilds the block counters of the current epoch after a restart by
// classifying again the leader slots already processed (up to the slot saved in the state).
// The counters only live in memory: without this, a restart in the middle of an epoch loses
// the blocks counted so far. It is best effort: on error the pool starts from zero, as before.
func (w *BlockWatcher) replayProcessedSlots(ctx context.Context) map[string]epochCounts {
	counts := make(map[string]epochCounts)
	for _, pool := range w.pools.GetActivePools() {
		poolCounts, err := w.replayPoolSlots(ctx, pool)
		if err != nil {
			w.logger.WarnContext(ctx, "unable to rebuild the block counters of the epoch, starting from zero",
				slog.String("pool_id", pool.ID),
				slog.Int("epoch", w.state.Epoch),
				slog.String("error", err.Error()),
			)
			continue
		}
		counts[pool.ID] = poolCounts
	}
	return counts
}

func (w *BlockWatcher) replayPoolSlots(ctx context.Context, pool pools.Pool) (epochCounts, error) {
	var counts epochCounts
	schedule, err := w.slotLeaderService.GetSlotLeaders(ctx, pool.ID, w.state.Epoch)
	if err != nil {
		return counts, fmt.Errorf("unable to get slot leaders: %w", err)
	}

	for _, slot := range schedule.Slots {
		if slot > w.state.Slot {
			continue
		}
		outcome, err := w.classifyLeaderSlot(ctx, pool, slot)
		if err != nil {
			return epochCounts{}, err
		}
		switch outcome.kind {
		case outcomeMissed:
			counts.missed++
			counts.consecutiveMissed++
		case outcomeValidated:
			counts.validated++
			counts.consecutiveMissed = 0
		case outcomeOrphaned:
			counts.orphaned++
		}
	}
	return counts, nil
}

// logMissedBlock records a missed block in metrics and logs the occurrence.
func (w *BlockWatcher) logMissedBlock(ctx context.Context, pool pools.Pool, slot, epoch int) {
	w.logger.InfoContext(ctx, fmt.Sprintf("❌ Pool %s missed block for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch), slog.String("pool_id", pool.ID))

	w.metrics.MissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
	w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
}

func (w *BlockWatcher) logValidatedBlock(ctx context.Context, pool pools.Pool, slot, epoch int, block bf.Block) {
	w.logger.InfoContext(ctx,
		fmt.Sprintf("✅ Pool %s proposed block for slot %d", pool.Name, slot),
		slog.Int("epoch", epoch),
		slog.String("pool_id", pool.ID),
		slog.Int("block_num", block.Height),
		slog.Int("epoch_slot", block.EpochSlot),
	)

	w.metrics.ValidatedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Inc()
	w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(epoch)).Set(0)
}

func (w *BlockWatcher) logOrphanedBlock(ctx context.Context, pool pools.Pool, slot, epoch int, block bf.Block) {
	w.logger.InfoContext(ctx,
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

		w.logger.DebugContext(ctx,
			fmt.Sprintf("🕰  Next slot leader for pool %s is slot %d, remaining slots: %d", pool.Name, nextSlot, remainingSlots),
			slog.String("pool_id", pool.ID),
			slog.Int("epoch", w.state.Epoch),
		)
		w.metrics.NextSlotLeader.WithLabelValues(pool.Name, pool.ID, pool.Instance, strconv.Itoa(w.state.Epoch)).Set(float64(nextSlot))
	}
	return nil
}

// initMetrics initializes the block metrics of the current epoch, starting from the given
// counts (nil or a missing pool means zero).
func (w *BlockWatcher) initMetrics(counts map[string]epochCounts) {
	w.metrics.MissedBlocks.Reset()
	w.metrics.ValidatedBlocks.Reset()
	w.metrics.OrphanedBlocks.Reset()

	epoch := strconv.Itoa(w.state.Epoch)
	for _, pool := range w.pools.GetActivePools() {
		c := counts[pool.ID]
		w.metrics.MissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, epoch).Add(float64(c.missed))
		w.metrics.ConsecutiveMissedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, epoch).Set(float64(c.consecutiveMissed))
		w.metrics.ValidatedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, epoch).Add(float64(c.validated))
		w.metrics.OrphanedBlocks.WithLabelValues(pool.Name, pool.ID, pool.Instance, epoch).Add(float64(c.orphaned))
	}
}
