package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
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

	// Leader election parameters, shared by all pools. Without them only the odds are skipped.
	election, err := w.getLeaderElection(ctx)
	if err != nil {
		w.logger.WarnContext(ctx, "unable to compute the leader slot odds", slog.String("error", err.Error()))
	}

	// A failing pool must not prevent the others from being refreshed
	var errs []error
	for _, pool := range w.pools.GetActivePools() {
		if err := w.collectPool(ctx, pool, election); err != nil {
			errs = append(errs, err)
		}
	}
	for _, pool := range w.pools.GetExcludedPools() {
		if err := w.collectExcludedPool(ctx, pool, election); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// collectExcludedPool keeps excluded pools visible: they are not monitored block by block, but
// exporting their production and leader slot odds lets an alert catch an excluded pool that
// starts producing (or should), so an exclusion cannot be forgotten.
func (w *PoolWatcher) collectExcludedPool(ctx context.Context, pool pools.Pool, election *leaderElection) error {
	labels := []string{pool.Name, pool.ID, pool.Instance}

	poolInfo, err := w.getPoolInfo(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("unable to retrieve details for excluded pool '%s': %w", pool.ID, err)
	}

	w.metrics.PoolExcluded.WithLabelValues(labels...).Set(1)
	w.metrics.PoolBlocksMinted.WithLabelValues(labels...).Set(float64(poolInfo.BlocksMinted))

	activeStake, err := strconv.ParseFloat(poolInfo.ActiveStake, 64)
	if err != nil {
		return fmt.Errorf("unable to convert active stake of pool '%s' to a number: %w", pool.ID, err)
	}
	w.setLeaderOdds(labels, activeStake, election)

	return nil
}

// setLeaderOdds exports the leader slot odds of the epoch for the given active stake.
func (w *PoolWatcher) setLeaderOdds(labels []string, activeStake float64, election *leaderElection) {
	if election == nil {
		return
	}
	expected, noSlot := leaderOdds(activeStake/election.activeStake, election.activeSlotsCoeff, election.epochLength)
	w.metrics.PoolExpectedLeaderSlots.WithLabelValues(labels...).Set(expected)
	w.metrics.PoolNoLeaderSlotProbability.WithLabelValues(labels...).Set(noSlot)
}

// leaderElection holds the network values needed to compute the leader slot odds of a pool.
type leaderElection struct {
	activeSlotsCoeff float64
	epochLength      int
	activeStake      float64
}

func (w *PoolWatcher) getLeaderElection(ctx context.Context) (*leaderElection, error) {
	genesis, ok := w.cache.Get("genesis")
	if !ok {
		info, err := w.blockfrost.GetGenesisInfo(ctx)
		if err != nil {
			return nil, fmt.Errorf("unable to retrieve genesis info: %w", err)
		}
		genesis = info
		// genesis parameters never change
		w.cache.Set("genesis", info, 1)
		w.cache.Wait()
	}
	genesisInfo := genesis.(bfAPI.GenesisBlock)

	network, err := w.blockfrost.GetNetworkInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve network info: %w", err)
	}
	activeStake, err := strconv.ParseFloat(network.Stake.Active, 64)
	if err != nil || activeStake <= 0 {
		return nil, fmt.Errorf("invalid network active stake %q", network.Stake.Active)
	}

	return &leaderElection{
		activeSlotsCoeff: float64(genesisInfo.ActiveSlotsCoefficient),
		epochLength:      genesisInfo.EpochLength,
		activeStake:      activeStake,
	}, nil
}

// leaderOdds returns, for a relative stake sigma, the mean number of leader slots expected
// over an epoch and the probability to get none. Ouroboros Praos elects a pool on each slot
// with probability phi(sigma) = 1 - (1 - f)^sigma, f being the active slots coefficient.
func leaderOdds(sigma, activeSlotsCoeff float64, epochLength int) (expected, noSlotProbability float64) {
	phi := 1 - math.Pow(1-activeSlotsCoeff, sigma)
	slots := float64(epochLength)
	return slots * phi, math.Pow(1-phi, slots)
}

// collectPool refreshes the metrics of a single pool.
func (w *PoolWatcher) collectPool(ctx context.Context, pool pools.Pool, election *leaderElection) error {
	labels := []string{pool.Name, pool.ID, pool.Instance}

	// Get pool details
	poolInfo, err := w.getPoolInfo(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("unable to retrieve details for pool '%s': %w", pool.ID, err)
	}

	// Set pool saturation level and delegation
	w.metrics.PoolExcluded.WithLabelValues(labels...).Set(0)
	w.metrics.PoolBlocksMinted.WithLabelValues(labels...).Set(float64(poolInfo.BlocksMinted))
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

	// Leader slot odds for the epoch, from the stake snapshot used for the election
	w.setLeaderOdds(labels, parsed["active stake"], election)

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
