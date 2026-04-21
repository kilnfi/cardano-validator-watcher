package cardanocli

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
	shelleyGenesisfile := filepath.Join(c.opts.ConfigDir, "shelley.json")
	if _, err := os.Stat(shelleyGenesisfile); errors.Is(err, os.ErrNotExist) {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to find shelley genesis file: %w", err)
	}

	if _, err := os.Stat(pool.Key); errors.Is(err, os.ErrNotExist) {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to find pool vrf skey file: %w", err)
	}

	args := []string{
		"conway", "query", "leadership-schedule",
		"--genesis", shelleyGenesisfile,
		"--stake-pool-id", pool.ID,
		"--vrf-signing-key-file", pool.Key,
		"--next",
		"--socket-path", c.opts.SocketPath,
	}

	args = c.appendNetworkArgs(args)

	output, err := c.executor.ExecCommand(ctx, leaderLogsTimeout, nil, "cardano-cli", args...)
	if err != nil {
		c.logger.ErrorContext(ctx,
			fmt.Sprintf("unable to execute cardano-cli leadership-schedule command: %v", err),
			slog.String("pool_name", pool.Name),
			slog.String("pool_id", pool.ID),
		)
		fmt.Fprint(os.Stdout, string(output))
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("cardano-cli leadership-schedule: %w", err)
	}

	type entry struct {
		SlotNumber int    `json:"slotNumber"`
		SlotTime   string `json:"slotTime"`
	}
	var entries []entry
	if err := json.Unmarshal(output, &entries); err != nil {
		return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to unmarshal leadership-schedule response: %w", err)
	}

	slots := make([]cardano.SlotSchedule, len(entries))
	for i, e := range entries {
		t, err := time.Parse(time.RFC3339, e.SlotTime)
		if err != nil {
			return cardano.ClientLeaderLogsResponse{}, fmt.Errorf("unable to parse slot time %q: %w", e.SlotTime, err)
		}
		slots[i] = cardano.SlotSchedule{
			No:   i + 1,
			Slot: e.SlotNumber,
			At:   t,
		}
	}

	return cardano.ClientLeaderLogsResponse{
		Status:        "ok",
		AssignedSlots: slots,
	}, nil
}

func (c *Client) LeaderLogs(ctx context.Context, ledgerSet string, epochNonce string, pool pools.Pool) (cardano.ClientLeaderLogsResponse, error) {
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
