package watcher

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blockfrost/blockfrost-go"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
	promutils "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestWatcherStatus(t *testing.T) {
	t.Parallel()

	t.Run("GoodPath_WatcherIsReady", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)

		metricsExpectedOutput := `
# HELP cardano_validator_watcher_health_status Health status of the Cardano validator watcher: 1 = healthy, 0 = unhealthy
# TYPE cardano_validator_watcher_health_status gauge
cardano_validator_watcher_health_status 1
`
		metricsUnderTest := []string{
			"cardano_validator_watcher_health_status",
		}

		registry := prometheus.NewRegistry()
		metrics := metrics.NewCollection()
		metrics.MustRegister(registry)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			ticker := time.NewTimer(time.Second * 10)
			<-ticker.C
			cancel()
		}()

		// Mock the calls
		clients.bf.EXPECT().Health(mock.Anything).Return(blockfrost.Health{IsHealthy: true}, nil)
		clients.cardano.EXPECT().Ping(ctx).Return(nil)

		healthStore := &HealthStore{}
		watcher := NewStatusWatcher(clients.bf, clients.cardano, metrics, healthStore)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		b := bytes.NewBufferString(metricsExpectedOutput)
		err = promutils.CollectAndCompare(registry, b, metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_WatcherIsNotReadyWhenBlockFrostIsDown", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)

		metricsExpectedOutput := `
# HELP cardano_validator_watcher_health_status Health status of the Cardano validator watcher: 1 = healthy, 0 = unhealthy
# TYPE cardano_validator_watcher_health_status gauge
cardano_validator_watcher_health_status 0
`
		metricsUnderTest := []string{
			"cardano_validator_watcher_health_status",
		}

		registry := prometheus.NewRegistry()
		metrics := metrics.NewCollection()
		metrics.MustRegister(registry)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			ticker := time.NewTimer(time.Second * 10)
			<-ticker.C
			cancel()
		}()

		// Mock the calls
		clients.bf.EXPECT().Health(mock.Anything).Return(blockfrost.Health{IsHealthy: false}, nil)
		clients.cardano.EXPECT().Ping(ctx).Return(nil)

		healthStore := &HealthStore{}
		watcher := NewStatusWatcher(clients.bf, clients.cardano, metrics, healthStore)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		b := bytes.NewBufferString(metricsExpectedOutput)
		err = promutils.CollectAndCompare(registry, b, metricsUnderTest...)
		require.NoError(t, err)
	})

	t.Run("SadPath_WatcherIsNotReadyWhenCardanoNodeIsDown", func(t *testing.T) {
		t.Parallel()

		clients := setupClients(t)

		metricsExpectedOutput := `
# HELP cardano_validator_watcher_health_status Health status of the Cardano validator watcher: 1 = healthy, 0 = unhealthy
# TYPE cardano_validator_watcher_health_status gauge
cardano_validator_watcher_health_status 0
`
		metricsUnderTest := []string{
			"cardano_validator_watcher_health_status",
		}

		registry := prometheus.NewRegistry()
		metrics := metrics.NewCollection()
		metrics.MustRegister(registry)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			ticker := time.NewTimer(time.Second * 10)
			<-ticker.C
			cancel()
		}()

		// Mock the calls
		clients.bf.EXPECT().Health(mock.Anything).Return(blockfrost.Health{IsHealthy: true}, nil)
		clients.cardano.EXPECT().Ping(ctx).Return(errors.New("cardano node is down"))

		healthStore := &HealthStore{}
		watcher := NewStatusWatcher(clients.bf, clients.cardano, metrics, healthStore)
		err := watcher.Start(ctx)
		require.ErrorIs(t, err, context.Canceled)
		b := bytes.NewBufferString(metricsExpectedOutput)
		err = promutils.CollectAndCompare(registry, b, metricsUnderTest...)
		require.NoError(t, err)
	})
}
