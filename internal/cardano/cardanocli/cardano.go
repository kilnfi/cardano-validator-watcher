package cardanocli

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/blake2b"

	_ "github.com/mattn/go-sqlite3"

	"github.com/kilnfi/cardano-validator-watcher/internal/blockfrost"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
)

const (
	pingTimeout          = 10 * time.Second
	stakeSnapshotTimeout = 30 * time.Second
	leaderLogsTimeout    = 5 * time.Minute
)

type Client struct {
	logger     *slog.Logger
	blockfrost blockfrost.Client
	opts       ClientOptions
	executor   CommandExecutor
}

var _ cardano.CardanoClient = (*Client)(nil)

type ClientOptions struct {
	ConfigDir  string
	Network    string
	SocketPath string
	Timezone   string
}

func (c *Client) appendNetworkArgs(args []string) []string {
	switch c.opts.Network {
	case "mainnet":
		return append(args, "--mainnet")
	case "preprod":
		return append(args, "--testnet-magic", "1")
	case "sanchonet":
		return append(args, "--testnet-magic", "4")
	case "preview":
		return append(args, "--testnet-magic", "2")
	}
	return args
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
		"query", "tip",
		"--socket-path", c.opts.SocketPath,
	}

	args = c.appendNetworkArgs(args)

	c.logger.DebugContext(ctx, "querying cardano node tip",
		slog.String("cmd", fmt.Sprintf("cardano-cli %s", strings.Join(args, " "))),
	)

	output, err := c.executor.ExecCommand(ctx, pingTimeout, nil, "cardano-cli", args...)
	if err != nil {
		if len(output) > 0 {
			return fmt.Errorf("failed to query Cardano node tip: %w: %s", err, output)
		}
		return fmt.Errorf("failed to query Cardano node tip: %w", err)
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

	args = c.appendNetworkArgs(args)

	output, err := c.executor.ExecCommand(ctx, stakeSnapshotTimeout, nil, "cardano-cli", args...)
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

func (c *Client) LeaderLogsNextEpoch(ctx context.Context, pool pools.Pool) (cardano.ClientLeaderLogsResponse, error) {
	start := time.Now()
	ctx = context.WithValue(ctx, poolNameCtxKey, pool.Name)

	protocolState, err := c.getProtocolState(ctx)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to get protocol state: %w", err)
	}

	nextEpochNonce, err := deriveNextEpochNonce(protocolState.CandidateNonce, protocolState.LastEpochBlockNonce)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to derive next epoch nonce: %w", err)
	}

	c.logger.DebugContext(ctx, "derived next epoch nonce",
		slog.String("candidate_nonce", protocolState.CandidateNonce),
		slog.String("last_epoch_block_nonce", protocolState.LastEpochBlockNonce),
		slog.String("next_epoch_nonce", nextEpochNonce),
	)

	resp, err := c.LeaderLogs(ctx, "next", nextEpochNonce, pool)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, err
	}

	c.logger.InfoContext(ctx, "next epoch slot schedule computed",
		slog.String("pool", pool.Name),
		slog.String("duration", time.Since(start).Round(time.Second).String()),
		slog.Int("assigned_slots", len(resp.AssignedSlots)),
	)

	return resp, nil
}

func (c *Client) getProtocolState(ctx context.Context) (cardano.ClientProtocolStateResponse, error) {
	args := []string{
		"query", "protocol-state",
		"--socket-path", c.opts.SocketPath,
	}
	args = c.appendNetworkArgs(args)

	output, err := c.executor.ExecCommand(ctx, stakeSnapshotTimeout, nil, "cardano-cli", args...)
	if err != nil {
		return cardano.ClientProtocolStateResponse{}, fmt.Errorf("unable to query protocol state: %w", err)
	}

	var response cardano.ClientProtocolStateResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return cardano.ClientProtocolStateResponse{}, fmt.Errorf("unable to unmarshal protocol-state response: %w", err)
	}

	return response, nil
}

func deriveNextEpochNonce(candidateNonce, lastEpochBlockNonce string) (string, error) {
	candidate, err := hex.DecodeString(candidateNonce)
	if err != nil {
		return "", fmt.Errorf("unable to decode candidateNonce: %w", err)
	}

	lastBlock, err := hex.DecodeString(lastEpochBlockNonce)
	if err != nil {
		return "", fmt.Errorf("unable to decode lastEpochBlockNonce: %w", err)
	}

	hash := blake2b.Sum256(append(candidate, lastBlock...))
	return hex.EncodeToString(hash[:]), nil
}

func (c *Client) LeaderLogs(ctx context.Context, ledgerSet string, epochNonce string, pool pools.Pool) (cardano.ClientLeaderLogsResponse, error) {
	// Inject pool name for subprocess memory logs; may already be set by LeaderLogsNextEpoch
	if _, ok := ctx.Value(poolNameCtxKey).(string); !ok {
		ctx = context.WithValue(ctx, poolNameCtxKey, pool.Name)
	}

	byronGenesisfile := filepath.Join(c.opts.ConfigDir, "byron.json")
	shelleyGenesisfile := filepath.Join(c.opts.ConfigDir, "shelley.json")
	if _, err := os.Stat(byronGenesisfile); errors.Is(err, os.ErrNotExist) {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to find byron genesis file: %w", err)
	}

	if _, err := os.Stat(shelleyGenesisfile); errors.Is(err, os.ErrNotExist) {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to find shelley genesis file: %w", err)
	}

	if _, err := os.Stat(pool.Key); errors.Is(err, os.ErrNotExist) {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to find pool vrf skey file: %w", err)
	}

	tmpFile, err := os.CreateTemp("", "cncli-*.db")
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to create temp db for cncli: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	tmpDB, err := sql.Open("sqlite3", tmpPath)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to initialize temp db for cncli: %w", err)
	}
	if _, err := tmpDB.ExecContext(ctx, "PRAGMA user_version = 1"); err != nil {
		tmpDB.Close()
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to initialize temp db for cncli: %w", err)
	}
	tmpDB.Close()

	args := []string{
		"leaderlog",
		"--byron-genesis",
		byronGenesisfile,
		"--shelley-genesis",
		shelleyGenesisfile,
		"--ledger-set",
		ledgerSet,
		"--nonce",
		epochNonce,
		"--pool-id",
		pool.ID,
		"--pool-vrf-skey",
		pool.Key,
		"--tz",
		c.opts.Timezone,
		"--db",
		tmpPath,
	}

	poolInfo, err := c.blockfrost.GetPoolInfo(ctx, pool.ID)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to fetch pool info for %s: %w", pool.ID, err)
	}

	poolstakeSnapshot, err := c.StakeSnapshot(ctx, poolInfo.PoolID)
	if err != nil {
		return cardano.ClientLeaderLogsResponse{}, err
	}

	switch ledgerSet {
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
	output, err := c.executor.ExecCommand(ctx, leaderLogsTimeout, envs, "cncli", args...)
	if err != nil {
		c.logger.ErrorContext(ctx,
			fmt.Sprintf("unable to execute cncli leaderlog command: %v", err),
			slog.String("pool_name", pool.Name),
			slog.String("pool_id", pool.ID),
		)
		fmt.Fprint(os.Stdout, string(output))
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("cncli leaderlog: %w", err)
	}

	response := cardano.ClientLeaderLogsResponse{}
	if err := json.Unmarshal(output, &response); err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to unmarshal response for leaderlog command: %w", err)
	}

	if response.Status == "error" {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("cncli leaderlog: %s", response.ErrorMessage)
	}

	return response, nil
}
