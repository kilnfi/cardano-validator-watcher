package watcher

import (
	"context"
	"testing"
	"time"

	blockfrostmocks "github.com/kilnfi/cardano-validator-watcher/internal/blockfrost/mocks"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/prometheus/client_golang/prometheus"
)

type clients struct {
	bf *blockfrostmocks.MockClient
}

func setupPools(t *testing.T) pools.Pools {
	t.Helper()
	return pools.Pools{
		{
			ID:       "pool-0",
			Instance: "pool-0",
			Key:      "key",
			Name:     "pool-0",
			Exclude:  false,
		},
		{
			ID:       "pool-1",
			Instance: "pool-1",
			Key:      "key",
			Name:     "pool-1",
			Exclude:  true,
		},
	}
}

func setupClients(t *testing.T) *clients {
	t.Helper()

	return &clients{
		bf: blockfrostmocks.NewMockClient(t),
	}
}

func setupRegistry(t *testing.T) *struct {
	registry              *prometheus.Registry
	metrics               *metrics.Collection
	metricsExpectedOutput string
	metricsUnderTest      []string
} {
	t.Helper()

	registry := prometheus.NewRegistry()
	Collection := metrics.NewCollection()
	Collection.MustRegister(registry)

	return &struct {
		registry              *prometheus.Registry
		metrics               *metrics.Collection
		metricsExpectedOutput string
		metricsUnderTest      []string
	}{
		registry:              registry,
		metrics:               Collection,
		metricsExpectedOutput: "",
		metricsUnderTest:      []string{},
	}
}

func setupContextWithTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.AfterFunc(d, cancel)
	}()
	return ctx
}
