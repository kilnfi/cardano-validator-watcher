package blockfrostapi

import (
	"context"
	"fmt"
	"time"

	"github.com/blockfrost/blockfrost-go"
	"github.com/hashicorp/go-retryablehttp"
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
	// Passing a custom client disables blockfrost-go's default retrying client:
	// keep retries with backoff on 429/5xx and apply the timeout per attempt.
	retryClient := retryablehttp.NewClient()
	retryClient.Logger = nil
	retryClient.HTTPClient.Timeout = opts.Timeout

	return &Client{
		blockfrost: blockfrost.NewAPIClient(
			blockfrost.APIClientOptions{
				ProjectID:   opts.ProjectID,
				Server:      opts.Server,
				MaxRoutines: opts.MaxRoutines,
				Client:      retryClient.StandardClient(),
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

// epochEdgeBlock returns the first ("asc") or last ("desc") block of an epoch with a single
// sorted request. Fetching every page concurrently and taking the first/last element is both
// expensive (~200 pages per epoch) and wrong: the concurrent pages are not returned in order.
func (c *Client) epochEdgeBlock(ctx context.Context, epoch int, order string) (blockfrost.Block, error) {
	hashes, err := c.blockfrost.EpochBlockDistribution(ctx, epoch, blockfrost.APIQueryParams{Count: 1, Page: 1, Order: order})
	if err != nil {
		return blockfrost.Block{}, fmt.Errorf("unable to list the blocks of epoch %d: %w", epoch, err)
	}
	if len(hashes) == 0 {
		return blockfrost.Block{}, fmt.Errorf("no block found in epoch %d", epoch)
	}
	block, err := c.blockfrost.Block(ctx, hashes[0])
	if err != nil {
		return blockfrost.Block{}, fmt.Errorf("unable to get block %s: %w", hashes[0], err)
	}
	return block, nil
}

func (c *Client) GetLastBlockFromPreviousEpoch(ctx context.Context, prevEpoch int) (blockfrost.Block, error) {
	return c.epochEdgeBlock(ctx, prevEpoch, "desc")
}

func (c *Client) GetFirstBlockInEpoch(ctx context.Context, epoch int) (blockfrost.Block, error) {
	return c.epochEdgeBlock(ctx, epoch, "asc")
}

func (c *Client) GetFirstSlotInEpoch(ctx context.Context, epoch int) (int, error) {
	block, err := c.epochEdgeBlock(ctx, epoch, "asc")
	if err != nil {
		return 0, err
	}
	return block.Slot - block.EpochSlot, nil
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

//nolint:wrapcheck
func (c *Client) GetAccountInfo(ctx context.Context, address string) (blockfrost.Account, error) {
	return c.blockfrost.Account(ctx, address)
}
