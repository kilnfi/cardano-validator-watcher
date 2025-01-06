package slotleader

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jmoiron/sqlx"
	blockfrostmocks "github.com/kilnfi/cardano-validator-watcher/internal/blockfrost/mocks"
	cardanomocks "github.com/kilnfi/cardano-validator-watcher/internal/cardano/mocks"
	"github.com/kilnfi/cardano-validator-watcher/internal/metrics"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/prometheus/client_golang/prometheus"
)

type clients struct {
	bf      *blockfrostmocks.MockClient
	cardano *cardanomocks.MockCardanoClient
}

type dbMockClient struct {
	db   *sqlx.DB
	mock sqlmock.Sqlmock
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
		bf:      blockfrostmocks.NewMockClient(t),
		cardano: cardanomocks.NewMockCardanoClient(t),
	}
}

func setupDB(t *testing.T) *dbMockClient {
	t.Helper()

	mockdb, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("failed to create mock: %v", err)
	}

	db := sqlx.NewDb(mockdb, "sqlite3")
	return &dbMockClient{
		db:   db,
		mock: mock,
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
