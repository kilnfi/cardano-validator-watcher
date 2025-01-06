package pools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	defaultPools = Pools{
		{
			Instance:        "pool1",
			ID:              "pool1",
			Name:            "pool1",
			Key:             "pool1",
			Exclude:         false,
			AllowEmptySlots: false,
		},
		{
			Instance:        "pool2",
			ID:              "pool2",
			Name:            "pool2",
			Key:             "pool2",
			Exclude:         false,
			AllowEmptySlots: true,
		},
		{
			Instance:        "pool3",
			ID:              "pool3",
			Name:            "pool3",
			Key:             "pool3",
			Exclude:         true,
			AllowEmptySlots: false,
		},
	}
)

func TestGetActivePools(t *testing.T) {
	t.Parallel()

	pools := defaultPools

	activePools := pools.GetActivePools()
	require.Len(t, activePools, 2)
}

func TestGetExcludedPools(t *testing.T) {
	t.Parallel()

	pools := defaultPools

	excludedPools := pools.GetExcludedPools()
	require.Len(t, excludedPools, 1)
}

func TestGetPoolStats(t *testing.T) {
	t.Parallel()

	pools := defaultPools

	stats := pools.GetPoolStats()
	require.Equal(t, 2, stats.Active)
	require.Equal(t, 1, stats.Excluded)
	require.Equal(t, 3, stats.Total)
}
