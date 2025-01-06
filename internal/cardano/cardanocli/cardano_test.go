package cardanocli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/blockfrost/blockfrost-go"
	blockfrostmocks "github.com/kilnfi/cardano-validator-watcher/internal/blockfrost/mocks"
	"github.com/kilnfi/cardano-validator-watcher/internal/cardano"
	mocks "github.com/kilnfi/cardano-validator-watcher/internal/cardano/cardanocli/mocks"
	"github.com/kilnfi/cardano-validator-watcher/internal/pools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestPing(t *testing.T) {
	t.Run("GoodPath_PingOK", func(t *testing.T) {
		clientopts := ClientOptions{
			Network:    "preprod",
			SocketPath: "/tmp/cardano.socket",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		exec := &mocks.MockCommandExecutor{}
		exec.EXPECT().ExecCommand(
			ctx,
			mock.Anything,
			"cardano-cli",
			"ping",
			"-u",
			clientopts.SocketPath,
			"-t",
			"-c",
			"1",
			"-m",
			"1",
		).Return(nil, nil)

		client := NewClient(clientopts, nil, exec)
		err := client.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("SadPath_PingKO", func(t *testing.T) {
		clientopts := ClientOptions{
			Network:    "preprod",
			SocketPath: "/tmp/cardano.socket",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		exec := &mocks.MockCommandExecutor{}
		exec.EXPECT().ExecCommand(
			ctx,
			mock.Anything,
			"cardano-cli",
			"ping",
			"-u",
			clientopts.SocketPath,
			"-t",
			"-c",
			"1",
			"-m",
			"1",
		).Return(nil, errors.New("connection refused"))

		client := NewClient(clientopts, nil, exec)
		err := client.Ping(ctx)
		assert.Equal(t, "unable to ping cardano RPC node: connection refused", err.Error())
	})
}

func TestStakeSnapshot(t *testing.T) {
	clientopts := ClientOptions{
		Network:    "preprod",
		SocketPath: "/tmp/cardano.socket",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	exec := &mocks.MockCommandExecutor{}
	expected := cardano.ClientQueryStakeSnapshotResponse{
		Pools: map[string]cardano.PoolStakeInfo{
			"pool-0": {
				StakeGo:   100,
				StakeMark: 100,
				StakeSet:  100,
			},
		},
		Total: cardano.TotalStakeInfo{
			StakeGo:   200,
			StakeMark: 200,
			StakeSet:  200,
		},
	}
	expectedByte, err := json.Marshal(expected)
	require.NoError(t, err)
	exec.EXPECT().ExecCommand(
		ctx,
		mock.Anything,
		"cardano-cli",
		"query",
		"stake-snapshot",
		"--stake-pool-id",
		"pool-0",
		"--socket-path",
		clientopts.SocketPath,
		"--testnet-magic",
		"1",
	).Return(expectedByte, nil)

	client := NewClient(clientopts, nil, exec)
	response, err := client.StakeSnapshot(ctx, "pool-0")
	require.NoError(t, err)
	assert.Equal(t, expected, response)
}

func TestLeaderLogs(t *testing.T) {
	pool := pools.Pool{
		Instance: "pool-0",
		ID:       "pool-0",
		Name:     "pool-0",
		Key:      "pool-0.vrf.skey",
	}
	clientopts := ClientOptions{
		Network:    "preprod",
		SocketPath: "/tmp/cardano.socket",
		DBPath:     "db.db",
		Timezone:   "UTC",
	}
	bf := blockfrostmocks.NewMockClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Temporary file
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatal(err)
	}
	clientopts.ConfigDir = dir
	byronGenesis, _ := os.Create(filepath.Join(clientopts.ConfigDir, "byron.json"))
	shelleyGenesis, _ := os.Create(filepath.Join(clientopts.ConfigDir, "shelley.json"))
	vrf, _ := os.Create("pool-0.vrf.skey")
	defer func() {
		os.RemoveAll(clientopts.ConfigDir)
		os.Remove(vrf.Name())
	}()

	bf.EXPECT().GetPoolInfo(ctx, "pool-0").Return(blockfrost.Pool{
		PoolID: "pool-0",
		Hex:    "pool-0-hex",
	}, nil)

	expected := cardano.ClientQueryStakeSnapshotResponse{
		Pools: map[string]cardano.PoolStakeInfo{
			"pool-0": {
				StakeGo:   100,
				StakeMark: 100,
				StakeSet:  100,
			},
		},
		Total: cardano.TotalStakeInfo{
			StakeGo:   200,
			StakeMark: 200,
			StakeSet:  200,
		},
	}
	expectedByte, err := json.Marshal(expected)
	require.NoError(t, err)

	exec := &mocks.MockCommandExecutor{}
	exec.EXPECT().ExecCommand(
		ctx,
		mock.Anything,
		"cardano-cli",
		"query",
		"stake-snapshot",
		"--stake-pool-id",
		"pool-0",
		"--socket-path",
		clientopts.SocketPath,
		"--testnet-magic",
		"1",
	).Return(expectedByte, nil)

	expectedOutput := cardano.ClientLeaderLogsResponse{
		Status:     "ok",
		Epoch:      100,
		EpochNonce: "nonce",
		EpochSlots: 10,
		PoolID:     "pool-0",
		AssignedSlots: []cardano.SlotSchedule{
			{
				No:          1,
				Slot:        2,
				SlotInEpoch: 2,
			},
		},
	}
	expectedOutputByte, err := json.Marshal(expectedOutput)
	require.NoError(t, err)
	exec.EXPECT().ExecCommand(
		ctx,
		mock.Anything,
		"cncli",
		"leaderlog",
		"--byron-genesis",
		byronGenesis.Name(),
		"--shelley-genesis",
		shelleyGenesis.Name(),
		"--ledger-set",
		"current",
		"--nonce",
		"nonce",
		"--pool-id",
		pool.ID,
		"--pool-vrf-skey",
		vrf.Name(),
		"--tz",
		clientopts.Timezone,
		"--db",
		clientopts.DBPath,
		"--pool-stake",
		strconv.Itoa(expected.Pools["pool-0-hex"].StakeSet),
		"--active-stake",
		strconv.Itoa(expected.Total.StakeSet),
	).Return(expectedOutputByte, nil)

	client := NewClient(clientopts, bf, exec)
	err = client.LeaderLogs(ctx, "current", "nonce", pool)
	require.NoError(t, err)
}
