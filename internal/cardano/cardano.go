package cardano

import (
	"context"

	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

type CardanoClient interface {
	LeaderLogs(ctx context.Context, ledgetSet string, epochNonce string, pool pools.Pool) error
	StakeSnapshot(ctx context.Context, PoolID string) (ClientQueryStakeSnapshotResponse, error)
	Ping(ctx context.Context) error
}
