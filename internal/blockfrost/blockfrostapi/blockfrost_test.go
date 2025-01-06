package blockfrostapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/blockfrost/blockfrost-go"
)

var (
	server *httptest.Server
)

func TestGetLatestEpoch(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	want := blockfrost.Epoch{
		ActiveStake:    &[]string{"784953934049314"}[0],
		BlockCount:     21298,
		EndTime:        1603835086,
		Epoch:          100,
		Fees:           "4203312194",
		FirstBlockTime: 1603403092,
		LastBlockTime:  1603835084,
		Output:         "7849943934049314",
		StartTime:      1603403091,
		TxCount:        17856,
	}
	mux.HandleFunc("/api/v0/epochs/latest", func(res http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("could not marshal response: %v", err)
		}
		res.WriteHeader(http.StatusOK)
		if _, err := res.Write(payload); err != nil {
			t.Fatalf("could not write response: %v", err)
		}
	})
	server = httptest.NewServer(mux)

	serverURL, _ := url.JoinPath(server.URL, "/api/v0")
	blockfrostClientOpts := ClientOptions{
		ProjectID:   "projectID",
		Server:      serverURL,
		MaxRoutines: 0,
		Timeout:     0,
	}
	client := NewClient(blockfrostClientOpts)
	epoch, err := client.GetLatestEpoch(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, epoch)
}

func TestGetLatestBlock(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()

	want := blockfrost.Block{
		Time:       1641338934,
		Height:     15243593,
		Hash:       "4ea1ba291e8eef538635a53e59fddba7810d1679631cc3aed7c8e6c4091a516a",
		Slot:       412162133,
		Epoch:      425,
		EpochSlot:  12,
		SlotLeader: "kiln",
	}
	mux.HandleFunc("/api/v0/blocks/latest", func(res http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("could not marshal response: %v", err)
		}
		res.WriteHeader(http.StatusOK)
		if _, err := res.Write(payload); err != nil {
			t.Fatalf("could not write response: %v", err)
		}
	})
	server = httptest.NewServer(mux)

	serverURL, _ := url.JoinPath(server.URL, "/api/v0")
	blockfrostClientOpts := ClientOptions{
		ProjectID:   "projectID",
		Server:      serverURL,
		MaxRoutines: 0,
		Timeout:     0,
	}
	client := NewClient(blockfrostClientOpts)
	block, err := client.GetLatestBlock(ctx)
	require.NoError(t, err)
	assert.Equal(t, want, block)
}

func TestGetPoolInfo(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()

	want := blockfrost.Pool{
		PoolID:         "pool-0",
		Hex:            "pool-0-hex",
		VrfKey:         "pool-0-vrf",
		BlocksMinted:   70,
		LiveStake:      "10000",
		LiveSaturation: 0.5,
		ActiveStake:    "10000",
		DeclaredPledge: "100",
		LivePledge:     "200",
	}
	mux.HandleFunc("/api/v0/pools/pool-0", func(res http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("could not marshal response: %v", err)
		}
		res.WriteHeader(http.StatusOK)
		if _, err := res.Write(payload); err != nil {
			t.Fatalf("could not write response: %v", err)
		}
	})
	server = httptest.NewServer(mux)

	serverURL, _ := url.JoinPath(server.URL, "/api/v0")
	blockfrostClientOpts := ClientOptions{
		ProjectID:   "projectID",
		Server:      serverURL,
		MaxRoutines: 0,
		Timeout:     0,
	}
	client := NewClient(blockfrostClientOpts)
	pool, err := client.GetPoolInfo(ctx, "pool-0")
	require.NoError(t, err)
	assert.Equal(t, want, pool)
}

func TestGetPoolMetadata(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()

	want := blockfrost.PoolMetadata{
		PoolID: "pool-0",
		Hex:    "pool-0-hex",
		Ticker: &[]string{"Pool-0"}[0],
		Name:   &[]string{"Pool-0"}[0],
	}
	mux.HandleFunc("/api/v0/pools/pool-0/metadata", func(res http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("could not marshal response: %v", err)
		}
		res.WriteHeader(http.StatusOK)
		if _, err := res.Write(payload); err != nil {
			t.Fatalf("could not write response: %v", err)
		}
	})
	server = httptest.NewServer(mux)

	serverURL, _ := url.JoinPath(server.URL, "/api/v0")
	blockfrostClientOpts := ClientOptions{
		ProjectID:   "projectID",
		Server:      serverURL,
		MaxRoutines: 0,
		Timeout:     0,
	}
	client := NewClient(blockfrostClientOpts)
	poolm, err := client.GetPoolMetadata(ctx, "pool-0")
	require.NoError(t, err)
	assert.Equal(t, want, poolm)
}

func TestGetPoolRelays(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()

	want := []blockfrost.PoolRelay{
		{
			Ipv4: &[]string{"10.0.0.1"}[0],
			DNS:  &[]string{"relay-0.example.com"}[0],
			Port: 3001,
		},
	}
	mux.HandleFunc("/api/v0/pools/pool-0/relays", func(res http.ResponseWriter, _ *http.Request) {
		payload, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("could not marshal response: %v", err)
		}
		res.WriteHeader(http.StatusOK)
		if _, err := res.Write(payload); err != nil {
			t.Fatalf("could not write response: %v", err)
		}
	})
	server = httptest.NewServer(mux)

	serverURL, _ := url.JoinPath(server.URL, "/api/v0")
	blockfrostClientOpts := ClientOptions{
		ProjectID:   "projectID",
		Server:      serverURL,
		MaxRoutines: 0,
		Timeout:     0,
	}
	client := NewClient(blockfrostClientOpts)
	poolRelays, err := client.GetPoolRelays(ctx, "pool-0")
	require.NoError(t, err)
	assert.Equal(t, want, poolRelays)
}
