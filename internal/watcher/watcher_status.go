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
	RefreshInterval   = 15 * time.Second
	RefreshMultiplier = 2
)

type HealthStore struct {
	mu sync.RWMutex

	health          bool
	lastRefreshTime time.Time
}

func NewHealthStore() *HealthStore {
	return &HealthStore{}
}

func (r *HealthStore) SetHealth(health bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.health = health
	r.lastRefreshTime = time.Now()
}

func (r *HealthStore) GetHealth() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.health
}

type StatusWatcher struct {
	logger      *slog.Logger
	blockfrost  blockfrost.Client
	cardano     cardano.CardanoClient
	metrics     *metrics.Collection
	healthStore *HealthStore
}

func NewStatusWatcher(
	blockfrost blockfrost.Client,
	cardano cardano.CardanoClient,
	metrics *metrics.Collection,
	healthStore *HealthStore,
) *StatusWatcher {
	logger := slog.With(
		slog.String("component", "status-watcher"),
	)

	return &StatusWatcher{
		logger:      logger,
		blockfrost:  blockfrost,
		cardano:     cardano,
		metrics:     metrics,
		healthStore: healthStore,
	}
}

func (w *StatusWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()

	for {
		w.checkStatus(ctx)

		select {
		case <-ctx.Done():
			w.logger.Info("stopping watcher")
			return fmt.Errorf("context done in watcher: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (w *StatusWatcher) checkStatus(ctx context.Context) {
	if w.healthStore.lastRefreshTime.IsZero() || time.Since(w.healthStore.lastRefreshTime) < RefreshMultiplier*RefreshInterval {
		status, err := w.blockfrost.Health(ctx)
		if err != nil {
			w.logger.Error("unable to check blockfrost health", slog.String("error", err.Error()))
		}

		if !status.IsHealthy {
			w.logger.Error("Blockfrost API is not responding")
		}

		isConnected, err := w.checkCardanoNodeConnection(ctx)
		if err != nil {
			w.logger.Error("Cardano node is not responding", slog.String("error", err.Error()))
		}

		if !status.IsHealthy || !isConnected {
			w.metrics.HealthStatus.Set(0)
			w.healthStore.SetHealth(false)
		} else {
			w.metrics.HealthStatus.Set(1)
			w.healthStore.SetHealth(true)
		}
	}
}

// checkCardanoNodeConnection checks the connection to the cardano-node socket.
// It returns true if the connection is successful, otherwise false.
func (w *StatusWatcher) checkCardanoNodeConnection(ctx context.Context) (bool, error) {
	if err := w.cardano.Ping(ctx); err != nil {
		return false, fmt.Errorf("unable to connect to cardano node: %w", err)
	}
	return true, nil
}
