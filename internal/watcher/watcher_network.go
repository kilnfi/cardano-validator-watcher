package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
)

const (
	EpochDuration = 5
)

// NetworkWatcherOptions represents the network watcher options
type NetworkWatcherOptions struct {
	Network         string
	RefreshInterval time.Duration
}

// NetworkWatcher represents a network watcher
type NetworkWatcher struct {
	logger      *slog.Logger
	blockfrost  blockfrost.Client
	metrics     *metrics.Collection
	healthStore *HealthStore
	opts        NetworkWatcherOptions
}

var _ Watcher = (*NetworkWatcher)(nil)

// NewNetworkWatcher creates a new network watcher
func NewNetworkWatcher(
	blockfrost blockfrost.Client,
	metrics *metrics.Collection,
	healthStore *HealthStore,
	opts NetworkWatcherOptions,
) *NetworkWatcher {
	logger := slog.With(
		slog.String("component", "network-watcher"),
	)
	return &NetworkWatcher{
		logger:      logger,
		blockfrost:  blockfrost,
		metrics:     metrics,
		healthStore: healthStore,
		opts:        opts,
	}
}

// Start starts the block watcher.
// It returns an error if the watcher fails to start or if an error is encountered during the process.
func (w *NetworkWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.opts.RefreshInterval)
	defer ticker.Stop()

	var previousHealthStatus bool
	for {
		currentHealthStatus := w.healthStore.GetHealth()
		w.handleHealthTransition(ctx, previousHealthStatus, currentHealthStatus)
		previousHealthStatus = currentHealthStatus

		if currentHealthStatus {
			if err := w.start(ctx); err != nil {
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

// start starts the watcher and the metrics collection
func (w *NetworkWatcher) start(ctx context.Context) error {
	if err := w.collectChainInfo(ctx); err != nil {
		return fmt.Errorf("unable to collect chain info: %w", err)
	}

	if err := w.collectNetworkInfo(ctx); err != nil {
		return fmt.Errorf("unable to collect network info: %w", err)
	}

	if err := w.collectEpochInfo(ctx); err != nil {
		return fmt.Errorf("unable to collect epoch info: %w", err)
	}

	return nil
}

// handleHealthTransition handles the transition of the network watcher's health status.
// It compares the previous and current health states, and logs a warning if the network watcher
// is not ready, or an info message if it is ready.
func (w *NetworkWatcher) handleHealthTransition(ctx context.Context, previous bool, current bool) {
	if previous != current {
		if !w.healthStore.GetHealth() {
			w.logger.WarnContext(ctx,
				"💔 network watcher is not ready.",
			)
		} else {
			w.logger.InfoContext(ctx, "💚 network watcher is ready")
		}
	}
}

// collectChainInfo collects information about the chain
func (w *NetworkWatcher) collectChainInfo(ctx context.Context) error {
	w.metrics.EpochDuration.Set(float64(EpochDuration))

	chainID, err := w.blockfrost.GetGenesisInfo(ctx)
	if err != nil {
		return fmt.Errorf("unable to retrieve genesis settings: %w", err)
	}
	w.metrics.ChainID.WithLabelValues(w.opts.Network).Set(float64(chainID.NetworkMagic))

	return nil
}

// collectNetworkInfo collects information about the network
func (w *NetworkWatcher) collectNetworkInfo(ctx context.Context) error {
	pools, err := w.blockfrost.GetAllPools(ctx)
	if err != nil {
		return fmt.Errorf("unable to retrieve pools: %w", err)
	}
	w.metrics.NetworkTotalPools.Set(float64(len(pools)))

	networkInfo, err := w.blockfrost.GetNetworkInfo(ctx)
	if err != nil {
		return fmt.Errorf("unable to retrieve network info: %w", err)
	}

	networkActiveStake, err := strconv.Atoi(networkInfo.Stake.Active)
	if err != nil {
		return fmt.Errorf("unable to convert active stake to integer: %w", err)
	}
	w.metrics.NetworkActiveStake.Set(float64(networkActiveStake))

	return nil
}

// collectEpochInfo collects information about the current epoch
func (w *NetworkWatcher) collectEpochInfo(ctx context.Context) error {
	latestEpoch, err := w.blockfrost.GetLatestEpoch(ctx)
	if err != nil {
		return fmt.Errorf("unable to retrieve latest epoch: %w", err)
	}
	w.metrics.NetworkEpoch.Set(float64(latestEpoch.Epoch))
	w.metrics.NetworkCurrentEpochProposedBlocks.Set(float64(latestEpoch.BlockCount))
	w.metrics.NextEpochStartTime.Set(float64(latestEpoch.EndTime))

	latestBlock, err := w.blockfrost.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("unable to retrieve latest block: %w", err)
	}
	w.metrics.NetworkBlockHeight.Set(float64(latestBlock.Height))
	w.metrics.NetworkSlot.Set(float64(latestBlock.Slot))
	w.metrics.NetworkEpochSlot.Set(float64(latestBlock.EpochSlot))

	return nil
}
