package blockfrost

import (
	"context"

	"github.com/blockfrost/blockfrost-go"
)

type Client interface {
	GetLatestEpoch(ctx context.Context) (blockfrost.Epoch, error)
	GetLatestBlock(ctx context.Context) (blockfrost.Block, error)
	GetPoolInfo(ctx context.Context, PoolID string) (blockfrost.Pool, error)
	GetPoolMetadata(ctx context.Context, PoolID string) (blockfrost.PoolMetadata, error)
	GetPoolRelays(ctx context.Context, PoolID string) ([]blockfrost.PoolRelay, error)
	GetBlockDistributionByPool(ctx context.Context, epoch int, PoolID string) ([]string, error)
	GetLastBlockFromPreviousEpoch(ctx context.Context, prevEpoch int) (blockfrost.Block, error)
	GetEpochParameters(ctx context.Context, epoch int) (blockfrost.EpochParameters, error)
	GetBlockBySlotAndEpoch(ctx context.Context, epoch int, slot int) (blockfrost.Block, error)
	GetBlockBySlot(ctx context.Context, slot int) (blockfrost.Block, error)
	Health(ctx context.Context) (blockfrost.Health, error)
	GetFirstSlotInEpoch(ctx context.Context, epoch int) (int, error)
	GetFirstBlockInEpoch(ctx context.Context, epoch int) (blockfrost.Block, error)
	GetGenesisInfo(ctx context.Context) (blockfrost.GenesisBlock, error)
	GetAllPools(ctx context.Context) ([]string, error)
	GetNetworkInfo(ctx context.Context) (blockfrost.NetworkInfo, error)
}
