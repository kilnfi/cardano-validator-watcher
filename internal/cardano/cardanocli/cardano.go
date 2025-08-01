package cardanocli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

type Client struct {
	logger     *slog.Logger
	blockfrost blockfrost.Client
	opts       ClientOptions
	executor   CommandExecutor
}

var _ cardano.CardanoClient = (*Client)(nil)

type ClientOptions struct {
	DBPath     string
	ConfigDir  string
	Network    string
	SocketPath string
	Timezone   string
}

func NewClient(opts ClientOptions, blockfrost blockfrost.Client, executor CommandExecutor) *Client {
	logger := slog.With(
		slog.String("component", "cardano-client"),
	)
	return &Client{
		logger:     logger,
		blockfrost: blockfrost,
		opts:       opts,
		executor:   executor,
	}
}

func (c *Client) Ping(ctx context.Context) error {
	args := []string{
		"ping",
		"-u",
		c.opts.SocketPath,
		"-t",
		"-c",
		"1",
	}

	switch c.opts.Network {
	case "mainnet":
		args = append(args, "-m", "764824073")
	case "preprod":
		args = append(args, "-m", "1")
	case "sanchonet":
		args = append(args, "--testnet-magic", "4")
	case "preview":
		args = append(args, "--testnet-magic", "2")
	}

	cmd := fmt.Sprintf("cardano-cli %s", strings.Join(args, " "))
	c.logger.DebugContext(ctx, "pinging cardano node", slog.String("cmd", cmd))

	_, err := c.executor.ExecCommand(ctx, nil, "cardano-cli", args...)
	if err != nil {
		return fmt.Errorf("unable to ping cardano RPC node: %w", err)
	}
	return nil
}

func (c *Client) StakeSnapshot(ctx context.Context, PoolID string) (cardano.ClientQueryStakeSnapshotResponse, error) {
	args := []string{
		"query",
		"stake-snapshot",
		"--stake-pool-id",
		PoolID,
		"--socket-path",
		c.opts.SocketPath,
	}

	switch c.opts.Network {
	case "mainnet":
		args = append(args, "--mainnet")
	case "preprod":
		args = append(args, "--testnet-magic", "1")
	case "sanchonet":
		args = append(args, "--testnet-magic", "4")
	case "preview":
		args = append(args, "--testnet-magic", "2")
	}

	output, err := c.executor.ExecCommand(ctx, nil, "cardano-cli", args...)
	if err != nil {
		fmt.Fprintln(os.Stdout, string(output))
		return cardano.ClientQueryStakeSnapshotResponse{}, fmt.Errorf("unable to query stake snapshot for pool %s: %w", PoolID, err)
	}

	response := cardano.ClientQueryStakeSnapshotResponse{}
	if err := json.Unmarshal(output, &response); err != nil {
		return cardano.ClientQueryStakeSnapshotResponse{}, fmt.Errorf("unable to unmarshal response for stake-snapshot command: %w", err)
	}

	return response, nil
}

func (c *Client) LeaderLogs(ctx context.Context, ledgetSet string, epochNonce string, pool pools.Pool) error {
	byronGenesisfile := filepath.Join(c.opts.ConfigDir, "byron.json")
	shelleyGenesisfile := filepath.Join(c.opts.ConfigDir, "shelley.json")
	if _, err := os.Stat(byronGenesisfile); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unable to find byron genesis file: %w", err)
	}

	if _, err := os.Stat(shelleyGenesisfile); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unable to find shelley genesis file: %w", err)
	}

	if _, err := os.Stat(pool.Key); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unable to find pool vrf skey file: %w", err)
	}

	args := []string{
		"leaderlog",
		"--byron-genesis",
		byronGenesisfile,
		"--shelley-genesis",
		shelleyGenesisfile,
		"--ledger-set",
		ledgetSet,
		"--nonce",
		epochNonce,
		"--pool-id",
		pool.ID,
		"--pool-vrf-skey",
		pool.Key,
		"--tz",
		c.opts.Timezone,
		"--db",
		c.opts.DBPath,
	}

	poolInfo, err := c.blockfrost.GetPoolInfo(ctx, pool.ID)
	if err != nil {
		return fmt.Errorf("unable to fetch pool info for %s: %w", pool.ID, err)
	}

	poolstakeSnapshot, err := c.StakeSnapshot(ctx, poolInfo.PoolID)
	if err != nil {
		return err
	}

	switch ledgetSet {
	case "prev":
		args = append(args, "--pool-stake", strconv.Itoa(poolstakeSnapshot.Pools[poolInfo.Hex].StakeGo))
		args = append(args, "--active-stake", strconv.Itoa(poolstakeSnapshot.Total.StakeGo))
	case "current":
		args = append(args, "--pool-stake", strconv.Itoa(poolstakeSnapshot.Pools[poolInfo.Hex].StakeSet))
		args = append(args, "--active-stake", strconv.Itoa(poolstakeSnapshot.Total.StakeSet))
	case "next":
		args = append(args, "--pool-stake", strconv.Itoa(poolstakeSnapshot.Pools[poolInfo.Hex].StakeMark))
		args = append(args, "--active-stake", strconv.Itoa(poolstakeSnapshot.Total.StakeMark))
	}

	envs := []string{
		"RUST_LOG=error",
	}
	output, err := c.executor.ExecCommand(ctx, envs, "cncli", args...)
	if err != nil {
		c.logger.ErrorContext(ctx,
			fmt.Sprintf("unable to execute cncli leaderlog command: %v", err),
			slog.String("pool_name", pool.Name),
			slog.String("pool_id", pool.ID),
		)
		fmt.Fprint(os.Stdout, string(output))
		return fmt.Errorf("cncli leaderlog: %w", err)
	}

	response := cardano.ClientLeaderLogsResponse{}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("unable to unmarshal response for leaderlog command: %w", err)
	}

	// catch errors from the cncli command when it returns an exit status of 0
	// despite encountering an issue.
	if response.Status == "error" {
		return fmt.Errorf("cncli leaderlog: %s", response.ErrorMessage)
	}

	return nil
}
