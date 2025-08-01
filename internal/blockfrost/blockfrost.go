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
	GetAccountInfo(ctx context.Context, stakeAddress string) (Account, error)
}

// TODO: remove it when the blockfrost-go library will be updated and return the DrepID
type Account struct {
	StakeAddress       string  `json:"stake_address"`
	Active             bool    `json:"active"`
	ActiveEpoch        *int64  `json:"active_epoch"`
	ControlledAmount   string  `json:"controlled_amount"`
	RewardsSum         string  `json:"rewards_sum"`
	WithdrawalsSum     string  `json:"withdrawals_sum"`
	ReservesSum        string  `json:"reserves_sum"`
	TreasurySum        string  `json:"treasury_sum"`
	WithdrawableAmount string  `json:"withdrawable_amount"`
	PoolID             *string `json:"pool_id"`
	DrepID             *string `json:"drep_id"`
}
