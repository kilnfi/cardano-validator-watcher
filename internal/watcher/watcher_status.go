package watcher

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
)

const (
	// DefaultRefreshInterval is used when no refresh interval is configured.
	DefaultRefreshInterval = 15 * time.Second
)

// HealthStore holds the health of the dependencies of the watcher.
type HealthStore struct {
	mu sync.RWMutex

	blockfrost  bool
	cardanoNode bool
}

func NewHealthStore() *HealthStore {
	return &HealthStore{}
}

// SetHealth sets the health of every dependency.
func (r *HealthStore) SetHealth(health bool) {
	r.SetDependencies(health, health)
}

// SetDependencies records the health of each dependency.
func (r *HealthStore) SetDependencies(blockfrost, cardanoNode bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.blockfrost = blockfrost
	r.cardanoNode = cardanoNode
}

// GetHealth reports whether every dependency is healthy.
func (r *HealthStore) GetHealth() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.blockfrost && r.cardanoNode
}

// IsBlockfrostHealthy reports whether Blockfrost is healthy. It is enough for the
// watchers that only read Blockfrost: they keep running when the cardano node is down.
func (r *HealthStore) IsBlockfrostHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.blockfrost
}

type StatusWatcher struct {
	logger          *slog.Logger
	blockfrost      blockfrost.Client
	cardano         cardano.CardanoClient
	metrics         *metrics.Collection
	healthStore     *HealthStore
	refreshInterval time.Duration
}

func NewStatusWatcher(
	blockfrost blockfrost.Client,
	cardano cardano.CardanoClient,
	metrics *metrics.Collection,
	healthStore *HealthStore,
	refreshInterval time.Duration,
) *StatusWatcher {
	logger := slog.With(
		slog.String("component", "status-watcher"),
	)

	if refreshInterval <= 0 {
		refreshInterval = DefaultRefreshInterval
	}

	return &StatusWatcher{
		logger:          logger,
		blockfrost:      blockfrost,
		cardano:         cardano,
		metrics:         metrics,
		healthStore:     healthStore,
		refreshInterval: refreshInterval,
	}
}

func (w *StatusWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.refreshInterval)
	defer ticker.Stop()

	for {
		w.checkStatus(ctx)

		select {
		case <-ctx.Done():
			w.logger.InfoContext(ctx, "stopping watcher")
			return fmt.Errorf("context done in watcher: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// checkStatus probes Blockfrost and the Cardano node on every call and updates
// the health store accordingly. It must run unconditionally on each tick: each
// probe opens a fresh connection (cardano-cli spawns a new process, Blockfrost a
// new HTTP request), so an unhealthy state recovers as soon as the upstream comes
// back. Gating this behind a "last refresh was recent" guard risks freezing the
// health state permanently if the goroutine is ever starved (e.g. GC pause under
// memory pressure) for longer than the guard window.
func (w *StatusWatcher) checkStatus(ctx context.Context) {
	status, err := w.blockfrost.Health(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "unable to check blockfrost health", slog.String("error", err.Error()))
	}

	if !status.IsHealthy {
		w.logger.ErrorContext(ctx, "Blockfrost API is not responding")
	}

	isConnected, err := w.checkCardanoNodeConnection(ctx)
	if err != nil {
		w.logger.ErrorContext(ctx, "Cardano node is not responding", slog.String("error", err.Error()))
	}

	w.healthStore.SetDependencies(status.IsHealthy, isConnected)
	w.metrics.DependencyUp.WithLabelValues("blockfrost").Set(boolToFloat(status.IsHealthy))
	w.metrics.DependencyUp.WithLabelValues("cardano_node").Set(boolToFloat(isConnected))
	w.metrics.HealthStatus.Set(boolToFloat(status.IsHealthy && isConnected))
}

// checkCardanoNodeConnection checks the connection to the cardano-node socket.
// It returns true if the connection is successful, otherwise false.
func (w *StatusWatcher) checkCardanoNodeConnection(ctx context.Context) (bool, error) {
	if err := w.cardano.Ping(ctx); err != nil {
		return false, fmt.Errorf("unable to connect to cardano node: %w", err)
	}
	return true, nil
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}
