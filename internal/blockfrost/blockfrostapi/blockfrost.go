package blockfrostapi

import (
	"context"
	"net/http"
	"time"

	"github.com/blockfrost/blockfrost-go"
	bf "github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
)

type Client struct {
	blockfrost blockfrost.APIClient
}

var _ bf.Client = (*Client)(nil)

type ClientOptions struct {
	ProjectID   string
	Server      string
	MaxRoutines int
	Timeout     time.Duration
}

func NewClient(opts ClientOptions) *Client {
	return &Client{
		blockfrost: blockfrost.NewAPIClient(
			blockfrost.APIClientOptions{
				ProjectID:   opts.ProjectID,
				Server:      opts.Server,
				MaxRoutines: opts.MaxRoutines,
				Client: &http.Client{
					Timeout: opts.Timeout,
				},
			},
		),
	}
}

//nolint:wrapcheck
func (c *Client) GetLatestEpoch(ctx context.Context) (blockfrost.Epoch, error) {
	return c.blockfrost.EpochLatest(ctx)
}

//nolint:wrapcheck
func (c *Client) GetLatestBlock(ctx context.Context) (blockfrost.Block, error) {
	return c.blockfrost.BlockLatest(ctx)
}

//nolint:wrapcheck
func (c *Client) GetPoolInfo(ctx context.Context, PoolID string) (blockfrost.Pool, error) {
	return c.blockfrost.Pool(ctx, PoolID)
}

//nolint:wrapcheck
func (c *Client) GetPoolMetadata(ctx context.Context, PoolID string) (blockfrost.PoolMetadata, error) {
	return c.blockfrost.PoolMetadata(ctx, PoolID)
}

//nolint:wrapcheck
func (c *Client) GetPoolRelays(ctx context.Context, PoolID string) ([]blockfrost.PoolRelay, error) {
	return c.blockfrost.PoolRelays(ctx, PoolID)
}

func (c *Client) GetBlockDistributionByPool(ctx context.Context, epoch int, PoolID string) ([]string, error) {
	resultChan := c.blockfrost.EpochBlockDistributionByPoolAll(ctx, epoch, PoolID)
	results := []string{}
	for result := range resultChan {
		if result.Err != nil {
			return nil, result.Err
		}

		results = append(results, result.Res...)
	}

	return results, nil
}

//nolint:wrapcheck
func (c *Client) GetEpochParameters(ctx context.Context, epoch int) (blockfrost.EpochParameters, error) {
	return c.blockfrost.EpochParameters(ctx, epoch)
}

//nolint:wrapcheck
func (c *Client) Health(ctx context.Context) (blockfrost.Health, error) {
	return c.blockfrost.Health(ctx)
}

//nolint:wrapcheck
func (c *Client) GetBlockBySlotAndEpoch(ctx context.Context, slot int, epoch int) (blockfrost.Block, error) {
	return c.blockfrost.BlocksBySlotAndEpoch(ctx, slot, epoch)
}

//nolint:wrapcheck
func (c *Client) GetBlockBySlot(ctx context.Context, slot int) (blockfrost.Block, error) {
	return c.blockfrost.BlockBySlot(ctx, slot)
}

//nolint:wrapcheck
func (c *Client) GetLastBlockFromPreviousEpoch(ctx context.Context, prevEpoch int) (blockfrost.Block, error) {
	response := c.blockfrost.EpochBlockDistributionAll(ctx, prevEpoch)
	results := []string{}

	for result := range response {
		if result.Err != nil {
			return blockfrost.Block{}, result.Err
		}

		results = append(results, result.Res...)
	}

	lastBlock := results[len(results)-1]
	return c.blockfrost.Block(ctx, lastBlock)
}

//nolint:wrapcheck
func (c *Client) GetFirstBlockInEpoch(ctx context.Context, epoch int) (blockfrost.Block, error) {
	response := c.blockfrost.EpochBlockDistributionAll(ctx, epoch)
	results := []string{}

	for result := range response {
		if result.Err != nil {
			return blockfrost.Block{}, result.Err
		}

		results = append(results, result.Res...)
	}

	return c.blockfrost.Block(ctx, results[0])
}

//nolint:wrapcheck
func (c *Client) GetFirstSlotInEpoch(ctx context.Context, epoch int) (int, error) {
	resultChan := c.blockfrost.EpochBlockDistributionAll(ctx, epoch)
	results := []string{}
	for result := range resultChan {
		if result.Err != nil {
			return 0, result.Err
		}

		results = append(results, result.Res...)
	}

	firstBlock := results[0]
	block, err := c.blockfrost.Block(ctx, firstBlock)
	if err != nil {
		return 0, err
	}

	return block.Slot, nil
}

//nolint:wrapcheck
func (c *Client) GetGenesisInfo(ctx context.Context) (blockfrost.GenesisBlock, error) {
	return c.blockfrost.Genesis(ctx)
}

func (c *Client) GetAllPools(ctx context.Context) ([]string, error) {
	resultChan := c.blockfrost.PoolsAll(ctx)
	results := []string{}
	for result := range resultChan {
		if result.Err != nil {
			return nil, result.Err
		}

		results = append(results, result.Res...)
	}

	return results, nil
}

//nolint:wrapcheck
func (c *Client) GetNetworkInfo(ctx context.Context) (blockfrost.NetworkInfo, error) {
	return c.blockfrost.Network(ctx)
}
