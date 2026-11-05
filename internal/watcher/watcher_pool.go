package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	bfAPI "github.com/blockfrost/blockfrost-go"
	"github.com/dgraph-io/ristretto/v2"
	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/prometheus/client_golang/prometheus"
)

// PoolWatcheropts represents the options for the pool watcher.
type PoolWatcherOptions struct {
	Network         string
	RefreshInterval time.Duration
}

// PoolWatcher represents a watcher for a set of Cardano pools.
type PoolWatcher struct {
	logger      *slog.Logger
	blockfrost  blockfrost.Client
	metrics     *metrics.Collection
	pools       pools.Pools
	poolstats   pools.PoolStats
	healthStore *HealthStore
	cache       *ristretto.Cache[string, interface{}]
	cacheTTL    time.Duration
	opts        PoolWatcherOptions
}

var _ Watcher = (*PoolWatcher)(nil)

// NewPoolWatcher creates a new instance of PoolWatcher.
// It takes a context, a blockfrost client, and a metrics collection as parameters.
// It returns a pointer to the created PoolWatcher.
func NewPoolWatcher(
	blockfrost blockfrost.Client,
	metrics *metrics.Collection,
	pools pools.Pools,
	healthStore *HealthStore,
	opts PoolWatcherOptions,
) (*PoolWatcher, error) {
	logger := slog.With(
		slog.String("component", "pool-watcher"),
	)

	cache, err := ristretto.NewCache(&ristretto.Config[string, interface{}]{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to create cache: %w", err)
	}

	return &PoolWatcher{
		logger:      logger,
		blockfrost:  blockfrost,
		metrics:     metrics,
		pools:       pools,
		poolstats:   pools.GetPoolStats(),
		healthStore: healthStore,
		cache:       cache,
		cacheTTL:    2 * opts.RefreshInterval,
		opts:        opts,
	}, nil
}

// Start starts the PoolWatcher and periodically calls a function.
// It uses a ticker to trigger the function call every 30 seconds.
// The function call can be canceled by canceling the provided context.
func (w *PoolWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.opts.RefreshInterval)
	defer ticker.Stop()

	var previousHealthStatus bool
	for {
		currentHealthStatus := w.healthStore.GetHealth()
		w.handleHealthTransition(ctx, previousHealthStatus, currentHealthStatus)
		previousHealthStatus = currentHealthStatus

		if currentHealthStatus {
			if err := w.fetch(ctx); err != nil {
				w.logger.ErrorContext(ctx, "unable to fetch pool data", slog.String("error", err.Error()))
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

// handleHealthTransition handles the transition of the pool watcher's health status.
// It compares the previous and current health states, and logs a warning if the pool watcher
// is not ready, or an info message if it is ready.
func (w *PoolWatcher) handleHealthTransition(ctx context.Context, previous bool, current bool) {
	if previous != current {
		if !w.healthStore.GetHealth() {
			w.logger.WarnContext(ctx,
				"💔 pool watcher is not ready.",
			)
		} else {
			w.logger.InfoContext(ctx, "💚 pool watcher is ready")
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

	// A failing pool must not prevent the others from being refreshed
	var errs []error
	for _, pool := range w.pools.GetActivePools() {
		if err := w.collectPool(ctx, pool); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// collectPool refreshes the metrics of a single pool.
func (w *PoolWatcher) collectPool(ctx context.Context, pool pools.Pool) error {
	labels := []string{pool.Name, pool.ID, pool.Instance}

	// Get pool details
	poolInfo, err := w.getPoolInfo(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("unable to retrieve details for pool '%s': %w", pool.ID, err)
	}

	// Set pool saturation level and delegation
	w.metrics.PoolsSaturationLevel.WithLabelValues(labels...).Set(poolInfo.LiveSaturation)
	w.metrics.PoolLiveDelegators.WithLabelValues(labels...).Set(float64(poolInfo.LiveDelegators))

	// Amounts are returned in lovelace as strings
	amounts := []struct {
		name  string
		value string
		gauge *prometheus.GaugeVec
	}{
		{"live stake", poolInfo.LiveStake, w.metrics.PoolLiveStake},
		{"active stake", poolInfo.ActiveStake, w.metrics.PoolActiveStake},
		{"live pledge", poolInfo.LivePledge, w.metrics.PoolLivePledge},
		{"declared pledge", poolInfo.DeclaredPledge, w.metrics.PoolDeclaredPledge},
	}
	parsed := make(map[string]float64, len(amounts))
	for _, amount := range amounts {
		value, err := strconv.ParseFloat(amount.value, 64)
		if err != nil {
			return fmt.Errorf("unable to convert %s of pool '%s' to a number: %w", amount.name, pool.ID, err)
		}
		amount.gauge.WithLabelValues(labels...).Set(value)
		parsed[amount.name] = value
	}

	// check if the pool has met its pledge requirements and set the metric accordingly
	if parsed["live pledge"] >= parsed["declared pledge"] {
		w.metrics.PoolsPledgeMet.WithLabelValues(labels...).Set(1)
	} else {
		w.metrics.PoolsPledgeMet.WithLabelValues(labels...).Set(0)
	}

	// Get number of relay servers associated with the pool
	poolRelays, err := w.getPoolRelays(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("unable to retrieve relays for pool '%s': %w", pool.ID, err)
	}
	w.metrics.RelaysPerPool.WithLabelValues(labels...).Set(float64(len(poolRelays)))

	// check if a pool owner is registered to a DRep
	poolAccountInfo, err := w.getAccountInfo(ctx, poolInfo.RewardAccount)
	if err != nil {
		return fmt.Errorf("unable to retrieve account info for pool '%s': %w", pool.ID, err)
	}
	if poolAccountInfo.DrepID != nil {
		w.metrics.PoolsDRepRegistered.WithLabelValues(labels...).Set(1)
	} else {
		w.metrics.PoolsDRepRegistered.WithLabelValues(labels...).Set(0)
	}

	return nil
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
		w.cache.SetWithTTL(PoolID+"_relays", poolRelays, 1, w.cacheTTL)
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
		w.cache.SetWithTTL(PoolID+"_info", poolInfo, 1, w.cacheTTL)
		w.cache.Wait()
	}
	pool = poolInfo.(bfAPI.Pool)

	return pool, nil
}

func (w *PoolWatcher) getAccountInfo(ctx context.Context, address string) (bfAPI.Account, error) {
	var err error
	var account bfAPI.Account

	accountInfo, ok := w.cache.Get(address + "_info")
	if !ok {
		accountInfo, err = w.blockfrost.GetAccountInfo(ctx, address)
		if err != nil {
			return bfAPI.Account{}, fmt.Errorf("unable to retrieve info for address '%s': %w", address, err)
		}
		w.cache.SetWithTTL(address+"_info", accountInfo, 1, w.cacheTTL)
		w.cache.Wait()
	}
	account = accountInfo.(bfAPI.Account)
	return account, nil
}
