package cardano

import (
	"context"

	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

type CardanoClient interface {
	LeaderLogs(ctx context.Context, ledgerSet string, epochNonce string, pool pools.Pool) (ClientLeaderLogsResponse, error)
	StakeSnapshot(ctx context.Context, PoolID string) (ClientQueryStakeSnapshotResponse, error)
	Ping(ctx context.Context) error
}
