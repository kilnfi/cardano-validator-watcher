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

func TestDeriveNextEpochNonce(t *testing.T) {
	// Values validated manually against cardano-cli leadership-schedule --next output
	candidateNonce := "fe29a9a0a3161eebcdab7d210bed35c20636cf759054d705a3620d4ed09b8183"
	lastEpochBlockNonce := "bac3ded0ddd204f324618ac32a6938b5a398318b2da37d7b9679cf96648c053d"
	expectedNonce := "47c2b7ae9e783b753afe7fd28986ac1b39c37220e13b5df09245cd4c1125f759"

	nonce, err := deriveNextEpochNonce(candidateNonce, lastEpochBlockNonce)
	require.NoError(t, err)
	require.Equal(t, expectedNonce, nonce)
}

func TestPing(t *testing.T) {
	t.Run("GoodPath", func(t *testing.T) {
		clientopts := ClientOptions{
			Network:    "preprod",
			SocketPath: "/tmp/cardano.socket",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		exec := &mocks.MockCommandExecutor{}
		exec.EXPECT().ExecCommand(
			ctx,
			pingTimeout,
			mock.Anything,
			"cardano-cli",
			"query",
			"tip",
			"--socket-path",
			clientopts.SocketPath,
			"--testnet-magic",
			"1",
		).Return(nil, nil)

		client := NewClient(clientopts, nil, exec)
		err := client.Ping(ctx)
		require.NoError(t, err)
	})

	t.Run("SadPath", func(t *testing.T) {
		clientopts := ClientOptions{
			Network:    "preprod",
			SocketPath: "/tmp/cardano.socket",
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		exec := &mocks.MockCommandExecutor{}
		exec.EXPECT().ExecCommand(
			ctx,
			pingTimeout,
			mock.Anything,
			"cardano-cli",
			"query",
			"tip",
			"--socket-path",
			clientopts.SocketPath,
			"--testnet-magic",
			"1",
		).Return(nil, errors.New("connection refused"))

		client := NewClient(clientopts, nil, exec)
		err := client.Ping(ctx)
		assert.Equal(t, "failed to query Cardano node tip: connection refused", err.Error())
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
		stakeSnapshotTimeout,
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

	bf.EXPECT().GetPoolInfo(mock.Anything, "pool-0").Return(blockfrost.Pool{
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
		mock.Anything,
		stakeSnapshotTimeout,
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
		mock.Anything,
		leaderLogsTimeout,
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
		mock.AnythingOfType("string"),
		"--pool-stake",
		strconv.Itoa(expected.Pools["pool-0-hex"].StakeSet),
		"--active-stake",
		strconv.Itoa(expected.Total.StakeSet),
	).Return(expectedOutputByte, nil)

	client := NewClient(clientopts, bf, exec)
	response, err := client.LeaderLogs(ctx, "current", "nonce", pool)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, response)
}

func TestLeaderLogsNextEpoch(t *testing.T) {
	pool := pools.Pool{
		Instance: "pool-0",
		ID:       "pool-0",
		Name:     "pool-0",
		Key:      "pool-0.vrf.skey",
	}
	clientopts := ClientOptions{
		Network:    "preprod",
		SocketPath: "/tmp/cardano.socket",
		Timezone:   "UTC",
	}
	bf := blockfrostmocks.NewMockClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	dir, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	clientopts.ConfigDir = dir
	_, _ = os.Create(filepath.Join(clientopts.ConfigDir, "byron.json"))
	_, _ = os.Create(filepath.Join(clientopts.ConfigDir, "shelley.json"))
	vrf, _ := os.Create("pool-0.vrf.skey")
	defer func() {
		os.RemoveAll(clientopts.ConfigDir)
		os.Remove(vrf.Name())
	}()

	const (
		candidateNonce      = "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd"
		lastEpochBlockNonce = "1122334411223344112233441122334411223344112233441122334411223344"
		derivedNonce        = "921e343a85e3030b0e986605f80cc7ff82e90f1051861d14215d3b4d22928c10"
	)

	protocolStateOutput, err := json.Marshal(cardano.ClientProtocolStateResponse{
		CandidateNonce:      candidateNonce,
		LastEpochBlockNonce: lastEpochBlockNonce,
	})
	require.NoError(t, err)

	stakeSnapshot := cardano.ClientQueryStakeSnapshotResponse{
		Pools: map[string]cardano.PoolStakeInfo{
			"pool-0-hex": {StakeMark: 100},
		},
		Total: cardano.TotalStakeInfo{StakeMark: 200},
	}
	stakeSnapshotOutput, err := json.Marshal(stakeSnapshot)
	require.NoError(t, err)

	expectedOutput := cardano.ClientLeaderLogsResponse{
		Status:     "ok",
		Epoch:      628,
		EpochNonce: derivedNonce,
		EpochSlots: 2,
		AssignedSlots: []cardano.SlotSchedule{
			{No: 1, Slot: 185071693, SlotInEpoch: 1},
			{No: 2, Slot: 185072489, SlotInEpoch: 2},
		},
	}
	expectedOutputByte, err := json.Marshal(expectedOutput)
	require.NoError(t, err)

	exec := &mocks.MockCommandExecutor{}
	exec.EXPECT().ExecCommand(
		mock.Anything,
		stakeSnapshotTimeout,
		mock.Anything,
		"cardano-cli",
		"query", "protocol-state",
		"--socket-path", clientopts.SocketPath,
		"--testnet-magic", "1",
	).Return(protocolStateOutput, nil)

	bf.EXPECT().GetPoolInfo(mock.Anything, "pool-0").Return(blockfrost.Pool{
		PoolID: "pool-0",
		Hex:    "pool-0-hex",
	}, nil)

	exec.EXPECT().ExecCommand(
		mock.Anything,
		stakeSnapshotTimeout,
		mock.Anything,
		"cardano-cli",
		"query", "stake-snapshot",
		"--stake-pool-id", "pool-0",
		"--socket-path", clientopts.SocketPath,
		"--testnet-magic", "1",
	).Return(stakeSnapshotOutput, nil)

	exec.EXPECT().ExecCommand(
		mock.Anything,
		leaderLogsTimeout,
		mock.Anything,
		"cncli",
		"leaderlog",
		"--byron-genesis", filepath.Join(clientopts.ConfigDir, "byron.json"),
		"--shelley-genesis", filepath.Join(clientopts.ConfigDir, "shelley.json"),
		"--ledger-set", "next",
		"--nonce", derivedNonce,
		"--pool-id", pool.ID,
		"--pool-vrf-skey", vrf.Name(),
		"--tz", clientopts.Timezone,
		"--db", mock.AnythingOfType("string"),
		"--pool-stake", strconv.Itoa(stakeSnapshot.Pools["pool-0-hex"].StakeMark),
		"--active-stake", strconv.Itoa(stakeSnapshot.Total.StakeMark),
	).Return(expectedOutputByte, nil)

	client := NewClient(clientopts, bf, exec)
	response, err := client.LeaderLogsNextEpoch(ctx, pool)
	require.NoError(t, err)
	require.Equal(t, expectedOutput, response)
}
