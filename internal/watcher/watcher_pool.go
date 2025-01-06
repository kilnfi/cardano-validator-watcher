package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/dgraph-io/ristretto"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

// PoolWatcheropts represents the options for the pool watcher.
type PoolWatcherOptions struct {
	Network         string
	RefreshInterval time.Duration
}

// PoolWatcher represents a watcher for a set of Cardano pools.
type PoolWatcher struct {
	logger     *slog.Logger
	blockfrost blockfrost.Client
	metrics    *metrics.Collection
	pools      pools.Pools
	poolstats  pools.PoolStats
	cache      *ristretto.Cache
	opts       PoolWatcherOptions
}

var _ Watcher = (*PoolWatcher)(nil)

// NewPoolWatcher creates a new instance of PoolWatcher.
// It takes a context, a blockfrost client, and a metrics collection as parameters.
// It returns a pointer to the created PoolWatcher.
func NewPoolWatcher(
	blockfrost blockfrost.Client,
	metrics *metrics.Collection,
	pools pools.Pools,
	opts PoolWatcherOptions,
) (*PoolWatcher, error) {
	logger := slog.With(
		slog.String("component", "pool-watcher"),
	)

	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create cache: %w", err)
	}

	return &PoolWatcher{
		logger:     logger,
		blockfrost: blockfrost,
		metrics:    metrics,
		pools:      pools,
		poolstats:  pools.GetPoolStats(),
		cache:      cache,
		opts:       opts,
	}, nil
}

// Start starts the PoolWatcher and periodically calls a function.
// It uses a ticker to trigger the function call every 30 seconds.
// The function call can be canceled by canceling the provided context.
func (w *PoolWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.opts.RefreshInterval)
	defer ticker.Stop()

	for {
		if err := w.fetch(ctx); err != nil {
			w.logger.Error("unable to fetch pool data", slog.String("error", err.Error()))
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

// run executes the main logic of the PoolWatcher.
// It collects data about a pool and periodically creates Prometheus metrics.
func (w *PoolWatcher) fetch(ctx context.Context) error {
	// Get the number of watched pools
	w.metrics.MonitoredValidatorsCount.WithLabelValues("total").Set(float64(w.poolstats.Total))
	w.metrics.MonitoredValidatorsCount.WithLabelValues("active").Set(float64(w.poolstats.Active))
	w.metrics.MonitoredValidatorsCount.WithLabelValues("excluded").Set(float64(w.poolstats.Excluded))

	// Loop through the pools
	for _, pool := range w.pools.GetActivePools() {
		// Get pool metadata
		poolMetadata, err := w.getPoolMetadata(ctx, pool.ID)
		if err != nil {
			return fmt.Errorf("unable to retrieve metadata for pool '%s': %w", pool.ID, err)
		}

		// Get pool details
		poolInfo, err := w.getPoolInfo(ctx, pool.ID)
		if err != nil {
			return fmt.Errorf("unable to retrieve details for pool '%s': %w", pool.ID, err)
		}

		// Set pool saturation level
		w.metrics.PoolsSaturationLevel.WithLabelValues(pool.Name, pool.ID, pool.Instance).Set(poolInfo.LiveSaturation)

		// check if the pool has met its pledge requirements and set the metric accordingly
		livePledge, err := strconv.Atoi(poolInfo.LivePledge)
		if err != nil {
			return fmt.Errorf("unable to convert live pledge to integer: %w", err)
		}

		declaredPledge, err := strconv.Atoi(poolInfo.DeclaredPledge)
		if err != nil {
			return fmt.Errorf("unable to convert declared pledge to integer: %w", err)
		}
		if livePledge >= declaredPledge {
			w.metrics.PoolsPledgeMet.WithLabelValues(pool.Name, pool.ID, pool.Instance).Set(1)
		} else {
			w.metrics.PoolsPledgeMet.WithLabelValues(pool.Name, pool.ID, pool.Instance).Set(0)
		}

		// Get number of relay servers associated with the pool
		poolRelays, err := w.getPoolRelays(ctx, pool.ID)
		if err != nil {
			return fmt.Errorf("unable to retrieve relays for pool '%s': %w", pool.ID, err)
		}
		w.metrics.RelaysPerPool.WithLabelValues(*poolMetadata.Ticker, pool.ID, pool.Instance).Set(float64(len(poolRelays)))
	}

	return nil
}

func (w *PoolWatcher) getPoolMetadata(ctx context.Context, PoolID string) (bfAPI.PoolMetadata, error) {
	var err error
	var metadata bfAPI.PoolMetadata

	poolMetadata, ok := w.cache.Get(PoolID + "_metadata")
	if !ok {
		poolMetadata, err = w.blockfrost.GetPoolMetadata(ctx, PoolID)
		if err != nil {
			return metadata, fmt.Errorf("unable to retrieve metadata for pool '%s': %w", PoolID, err)
		}
		w.cache.SetWithTTL(PoolID+"_metadata", poolMetadata, 1, 2*w.opts.RefreshInterval)
		w.cache.Wait()
	}
	metadata = poolMetadata.(bfAPI.PoolMetadata)

	return metadata, nil
}

func (w *PoolWatcher) getPoolRelays(ctx context.Context, PoolID string) ([]bfAPI.PoolRelay, error) {
	var err error
	var relays []bfAPI.PoolRelay

	poolRelays, ok := w.cache.Get(PoolID + "_relays")
	if !ok {
		poolRelays, err = w.blockfrost.GetPoolRelays(ctx, PoolID)
		if err != nil {
			return relays, fmt.Errorf("unable to retrieve relays for pool '%s': %w", PoolID, err)
		}
		w.cache.SetWithTTL(PoolID+"_relays", poolRelays, 1, 2*w.opts.RefreshInterval)
		w.cache.Wait()
	}
	relays = poolRelays.([]bfAPI.PoolRelay)

	return relays, nil
}

func (w *PoolWatcher) getPoolInfo(ctx context.Context, PoolID string) (bfAPI.Pool, error) {
	var err error
	var pool bfAPI.Pool

	poolInfo, ok := w.cache.Get(PoolID + "_info")
	if !ok {
		poolInfo, err = w.blockfrost.GetPoolInfo(ctx, PoolID)
		if err != nil {
			return pool, fmt.Errorf("unable to retrieve info for pool '%s': %w", PoolID, err)
		}
		w.cache.SetWithTTL(PoolID+"_info", poolInfo, 1, 2*w.opts.RefreshInterval)
		w.cache.Wait()
	}
	pool = poolInfo.(bfAPI.Pool)

	return pool, nil
}
